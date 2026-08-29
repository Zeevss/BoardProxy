package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateHubAddress(t *testing.T) {
	valid := []string{"hub:8443", "hub.example.net:8443", "127.0.0.1:8443", "[::1]:8443"}
	for _, address := range valid {
		if err := validateHubAddress(address); err != nil {
			t.Errorf("validateHubAddress(%q) = %v, want nil", address, err)
		}
	}

	// Ровно те значения, которые панель клала в секрет до починки.
	invalid := map[string]string{
		"http://127.0.0.1:8080":   "scheme",
		"https://hub.example.net": "scheme",
		"hub.example.net":         "host:port",
		"hub:":                    "port",
		"hub:not-a-port":          "port",
		"hub:99999":               "port",
	}
	for address, want := range invalid {
		err := validateHubAddress(address)
		if err == nil {
			t.Errorf("validateHubAddress(%q) = nil, want an error", address)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validateHubAddress(%q) = %v, want a message about %q", address, err, want)
		}
	}
}

func TestSupersededByAcceptsMatchingIdentity(t *testing.T) {
	authority := newAuthority(t)
	leaf := authority.issue(t, "node-1", time.Hour)

	if reason := supersededBy(leaf, authority.secret("node-1")); reason != "" {
		t.Fatalf("supersededBy() = %q, want an empty reason", reason)
	}
}

func TestSupersededByDetectsRenamedNode(t *testing.T) {
	authority := newAuthority(t)
	leaf := authority.issue(t, "node-2", time.Hour)

	reason := supersededBy(leaf, authority.secret("buterbrod-malevich"))
	if !strings.Contains(reason, "node-2") || !strings.Contains(reason, "buterbrod-malevich") {
		t.Fatalf("supersededBy() = %q, want both node ids in the reason", reason)
	}
}

func TestSupersededByDetectsAnotherHub(t *testing.T) {
	previous := newAuthority(t)
	replacement := newAuthority(t)
	leaf := previous.issue(t, "node-1", time.Hour)

	// Имя ноды то же, но CA другой: секрет ведёт на другой хаб.
	if reason := supersededBy(leaf, replacement.secret("node-1")); reason == "" {
		t.Fatal("supersededBy() = \"\", want a reason about the certificate authority")
	}
}

// Истёкший сертификат продлевает отдельная ветка Ensure. Если бы просрочка
// считалась сменой хаба, нода жгла бы одноразовый токен на каждом продлении.
func TestSupersededByIgnoresExpiry(t *testing.T) {
	authority := newAuthority(t)
	leaf := authority.issue(t, "node-1", -time.Hour)

	if reason := supersededBy(leaf, authority.secret("node-1")); reason != "" {
		t.Fatalf("supersededBy() = %q, want an empty reason for an expired certificate", reason)
	}
}

func TestResetRemovesStoredIdentity(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "identity", "identity.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := Reset(dataDir)
	if err != nil || !removed {
		t.Fatalf("Reset() = %v, %v; want true, nil", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stored identity still present: %v", err)
	}

	// Повторный сброс — не ошибка: команду запускают вслепую.
	removed, err = Reset(dataDir)
	if err != nil || removed {
		t.Fatalf("Reset() on a clean directory = %v, %v; want false, nil", removed, err)
	}
}

// --- вспомогательное: минимальный удостоверяющий центр -----------------------

type testAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         string
}

func newAuthority(t *testing.T) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	return &testAuthority{certificate: certificate, key: key, pem: string(encoded)}
}

// issue выпускает клиентский сертификат ноды; отрицательный validity даёт
// уже истёкший.
func (a *testAuthority) issue(t *testing.T, nodeID string, validity time.Duration) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	notAfter := time.Now().Add(validity)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: nodeID},
		NotBefore:    notAfter.Add(-2 * time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, a.certificate, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func (a *testAuthority) secret(nodeID string) BootstrapSecret {
	return BootstrapSecret{
		NodeID:          nodeID,
		HubURL:          "hub:8443",
		EnrollmentToken: "one-time-token",
		CACertificate:   a.pem,
	}
}
