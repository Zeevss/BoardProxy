package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"bproxy-control-plane/internal/domain"
	"bproxy-control-plane/internal/ports"
)

type Enrollment struct {
	tokens    ports.EnrollmentTokens
	authority ports.CertificateAuthority
}

type BootstrapSecret struct {
	NodeID          string `json:"node_id"`
	HubURL          string `json:"hub_url"`
	EnrollmentToken string `json:"enrollment_token"`
	CACertificate   string `json:"ca_certificate_pem"`
}

func NewEnrollment(tokens ports.EnrollmentTokens, authority ports.CertificateAuthority) *Enrollment {
	return &Enrollment{tokens: tokens, authority: authority}
}

func (s *Enrollment) IssueBootstrap(ctx context.Context, nodeID, hubURL string, ttl time.Duration) (string, error) {
	if !domain.ValidID(nodeID) || hubURL == "" || ttl <= 0 {
		return "", errors.New("node id, hub URL and positive TTL are required")
	}
	token, err := s.tokens.CreateEnrollmentToken(ctx, nodeID, ttl)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(BootstrapSecret{
		NodeID: nodeID, HubURL: hubURL, EnrollmentToken: token, CACertificate: string(s.authority.CAPEM()),
	})
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func (s *Enrollment) Enroll(ctx context.Context, nodeID, token string, csr []byte) (domain.Certificate, error) {
	if err := s.tokens.ConsumeEnrollmentToken(ctx, nodeID, token); err != nil {
		return domain.Certificate{}, err
	}
	return s.authority.SignNodeCSR(nodeID, csr)
}

func (s *Enrollment) Renew(nodeID string, csr []byte) (domain.Certificate, error) {
	return s.authority.SignNodeCSR(nodeID, csr)
}
