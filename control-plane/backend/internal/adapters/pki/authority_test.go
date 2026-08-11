package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
)

func TestSignsNodeClientCertificate(t *testing.T) {
	authority, err := Open(t.TempDir(), []string{"hub"})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csr, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "ignored"}}, key)
	certificate, err := authority.SignNodeCSR("node-1", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificate.PEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "node-1" {
		t.Fatalf("CN = %q", leaf.Subject.CommonName)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: authority.CertPool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal(err)
	}
}
