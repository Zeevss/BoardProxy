package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const renewBefore = 7 * 24 * time.Hour

type BootstrapSecret struct {
	NodeID          string `json:"node_id"`
	HubURL          string `json:"hub_url"`
	EnrollmentToken string `json:"enrollment_token"`
	CACertificate   string `json:"ca_certificate_pem"`
}

type storedIdentity struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CACertificate  string `json:"ca_certificate_pem"`
}

type Identity struct {
	NodeID  string
	HubURL  string
	TLS     *tls.Config
	RenewAt time.Time
}

func Ensure(ctx context.Context, dataDir, encodedSecret, agentVersion string) (*Identity, error) {
	secret, err := parseSecret(encodedSecret)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, "identity")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	bundlePath := filepath.Join(dir, "identity.json")
	bundle, err := loadBundle(bundlePath)
	if errors.Is(err, os.ErrNotExist) {
		bundle, err = enroll(ctx, secret, agentVersion)
		if err == nil {
			err = writeBundle(bundlePath, bundle)
		}
	}
	if err != nil {
		return nil, err
	}
	tlsConfig, leaf, err := tlsFromBundle(bundle)
	if err != nil {
		return nil, err
	}
	if leaf.Subject.CommonName != secret.NodeID {
		return nil, fmt.Errorf("identity: stored certificate belongs to %q, not %q", leaf.Subject.CommonName, secret.NodeID)
	}
	if time.Until(leaf.NotAfter) <= renewBefore {
		if !time.Now().Before(leaf.NotAfter) {
			bundle, err = enroll(ctx, secret, agentVersion)
			if err != nil {
				return nil, fmt.Errorf("identity: node certificate expired; re-enrollment failed: %w", err)
			}
		} else {
			bundle, err = renew(ctx, secret, agentVersion, tlsConfig)
			if err != nil {
				return nil, err
			}
		}
		if err := writeBundle(bundlePath, bundle); err != nil {
			return nil, err
		}
		tlsConfig, leaf, err = tlsFromBundle(bundle)
		if err != nil {
			return nil, err
		}
	}
	return &Identity{NodeID: secret.NodeID, HubURL: secret.HubURL, TLS: tlsConfig, RenewAt: leaf.NotAfter.Add(-renewBefore)}, nil
}

func parseSecret(encoded string) (BootstrapSecret, error) {
	if encoded == "" {
		return BootstrapSecret{}, errors.New("identity: BPROXY_NODE_SECRET is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		// Accept secrets emitted by early control-plane builds and values that
		// operators converted to the standard alphabet as a compatibility path.
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return BootstrapSecret{}, fmt.Errorf("identity: decode bootstrap secret: %w", err)
		}
	}
	var secret BootstrapSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return BootstrapSecret{}, fmt.Errorf("identity: parse bootstrap secret: %w", err)
	}
	if secret.NodeID == "" || secret.HubURL == "" || secret.CACertificate == "" {
		return BootstrapSecret{}, errors.New("identity: incomplete bootstrap secret")
	}
	return secret, nil
}

func loadBundle(path string) (storedIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return storedIdentity{}, err
	}
	var bundle storedIdentity
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return storedIdentity{}, fmt.Errorf("identity: parse stored identity: %w", err)
	}
	return bundle, nil
}

func enroll(ctx context.Context, secret BootstrapSecret, agentVersion string) (storedIdentity, error) {
	if secret.EnrollmentToken == "" {
		return storedIdentity{}, errors.New("identity: no stored identity and bootstrap token is empty")
	}
	csrPEM, keyPEM, err := makeCSR(secret.NodeID)
	if err != nil {
		return storedIdentity{}, err
	}
	pool, err := certPool([]byte(secret.CACertificate))
	if err != nil {
		return storedIdentity{}, err
	}
	conn, err := grpc.NewClient(secret.HubURL, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		RootCAs: pool, MinVersion: tls.VersionTLS13,
	})))
	if err != nil {
		return storedIdentity{}, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, err := nodev1.NewNodeControlServiceClient(conn).Enroll(callCtx, &nodev1.EnrollRequest{
		NodeId: secret.NodeID, Token: secret.EnrollmentToken, CsrPem: csrPEM, AgentVersion: agentVersion,
	})
	if err != nil {
		return storedIdentity{}, fmt.Errorf("identity: enroll: %w", err)
	}
	return responseBundle(response, keyPEM)
}

func renew(ctx context.Context, secret BootstrapSecret, agentVersion string, currentTLS *tls.Config) (storedIdentity, error) {
	csrPEM, keyPEM, err := makeCSR(secret.NodeID)
	if err != nil {
		return storedIdentity{}, err
	}
	conn, err := grpc.NewClient(secret.HubURL, grpc.WithTransportCredentials(credentials.NewTLS(currentTLS.Clone())))
	if err != nil {
		return storedIdentity{}, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, err := nodev1.NewNodeControlServiceClient(conn).Renew(callCtx, &nodev1.RenewRequest{CsrPem: csrPEM, AgentVersion: agentVersion})
	if err != nil {
		return storedIdentity{}, fmt.Errorf("identity: renew certificate: %w", err)
	}
	return responseBundle(response, keyPEM)
}

func makeCSR(nodeID string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	csrRaw, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID}}, key)
	if err != nil {
		return nil, nil, err
	}
	keyRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrRaw}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw}), nil
}

func responseBundle(response *nodev1.EnrollResponse, keyPEM []byte) (storedIdentity, error) {
	bundle := storedIdentity{
		CertificatePEM: string(response.GetCertificatePem()), PrivateKeyPEM: string(keyPEM),
		CACertificate: string(response.GetCaCertificatePem()),
	}
	if _, _, err := tlsFromBundle(bundle); err != nil {
		return storedIdentity{}, fmt.Errorf("identity: hub returned an invalid identity: %w", err)
	}
	return bundle, nil
}

func tlsFromBundle(bundle storedIdentity) (*tls.Config, *x509.Certificate, error) {
	certificate, err := tls.X509KeyPair([]byte(bundle.CertificatePEM), []byte(bundle.PrivateKeyPEM))
	if err != nil {
		return nil, nil, err
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	pool, err := certPool([]byte(bundle.CACertificate))
	if err != nil {
		return nil, nil, err
	}
	verifyAt := time.Now()
	if verifyAt.After(leaf.NotAfter) {
		// Verify the CA signature and usages even for an expired stored identity;
		// Ensure will require a new bootstrap token before returning it.
		verifyAt = leaf.NotAfter.Add(-time.Second)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: verifyAt, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, nil, fmt.Errorf("identity: verify node certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{certificate}, RootCAs: pool, MinVersion: tls.VersionTLS13}, leaf, nil
}

func certPool(caPEM []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("identity: CA certificate is invalid")
	}
	return pool, nil
}

func writeBundle(path string, bundle storedIdentity) error {
	raw, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
