package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"bproxy-control-plane/internal/domain"
)

const nodeCertificateLifetime = 30 * 24 * time.Hour

type Authority struct {
	caCertificate *x509.Certificate
	caKey         *ecdsa.PrivateKey
	caPEM         []byte
	server        tls.Certificate
}

func Open(root string, serverNames []string) (*Authority, error) {
	directory := filepath.Join(root, "pki")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	caCertificate, caKey, caPEM, err := loadOrCreateCA(directory)
	if err != nil {
		return nil, err
	}
	server, err := loadOrCreateServerCertificate(directory, caCertificate, caKey, serverNames)
	if err != nil {
		return nil, err
	}
	return &Authority{caCertificate: caCertificate, caKey: caKey, caPEM: caPEM, server: server}, nil
}

func (a *Authority) CAPEM() []byte { return append([]byte(nil), a.caPEM...) }

func (a *Authority) ServerCertificate() tls.Certificate { return a.server }

func (a *Authority) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.caCertificate)
	return pool
}

func (a *Authority) SignNodeCSR(nodeID string, csrPEM []byte) (domain.Certificate, error) {
	csr, err := parseCSR(csrPEM)
	if err != nil {
		return domain.Certificate{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(nodeCertificateLifetime)
	identityURI, _ := url.Parse("spiffe://boardproxy/node/" + nodeID)
	template := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: nodeID},
		NotBefore: now.Add(-time.Minute), NotAfter: expiresAt,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs: []*url.URL{identityURI}, BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, a.caCertificate, csr.PublicKey, a.caKey)
	if err != nil {
		return domain.Certificate{}, err
	}
	return domain.Certificate{
		PEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
		CAPEM: a.CAPEM(), ExpiresAt: expiresAt,
	}, nil
}

func parseCSR(raw []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("pki: invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, errors.New("pki: invalid CSR signature")
	}
	return csr, nil
}

func loadOrCreateCA(directory string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	certificatePath := filepath.Join(directory, "ca.crt")
	keyPath := filepath.Join(directory, "ca.key")
	if certificatePEM, err := os.ReadFile(certificatePath); err == nil {
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, nil, nil, err
		}
		certificate, key, err := parsePair(certificatePEM, keyPEM)
		return certificate, key, certificatePEM, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: "BoardProxy Node CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(5 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := writeSecret(certificatePath, certificatePEM); err != nil {
		return nil, nil, nil, err
	}
	if err := writeSecret(keyPath, keyPEM); err != nil {
		return nil, nil, nil, err
	}
	certificate, err := x509.ParseCertificate(raw)
	return certificate, key, certificatePEM, err
}

func loadOrCreateServerCertificate(directory string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, names []string) (tls.Certificate, error) {
	certificatePath := filepath.Join(directory, "server.crt")
	keyPath := filepath.Join(directory, "server.key")
	if certificatePEM, err := os.ReadFile(certificatePath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			return tls.X509KeyPair(certificatePEM, keyPEM)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: "BoardProxy Hub"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(365 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else if name != "" {
			template.DNSNames = append(template.DNSNames, name)
		}
	}
	if len(template.DNSNames) == 0 && len(template.IPAddresses) == 0 {
		template.DNSNames = []string{"localhost"}
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	keyPEM, err := marshalKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := writeSecret(certificatePath, certificatePEM); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeSecret(keyPath, keyPEM); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certificatePEM, keyPEM)
}

func parsePair(certificatePEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificateBlock, _ := pem.Decode(certificatePEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certificateBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("pki: invalid PEM pair")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("pki: CA key is not ECDSA")
	}
	return certificate, key, nil
}

func marshalKey(key *ecdsa.PrivateKey) ([]byte, error) {
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), nil
}

func writeSecret(path string, raw []byte) error {
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("pki: write %s: %w", path, err)
	}
	return nil
}

func randomSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}
