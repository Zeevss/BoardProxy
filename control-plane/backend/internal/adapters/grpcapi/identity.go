package grpcapi

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func validateCSR(raw []byte) error {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return errors.New("invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return errors.New("invalid CSR signature")
	}
	return nil
}

func authenticatedNodeID(ctx context.Context) (string, error) {
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing peer identity")
	}
	info, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return "", status.Error(codes.Unauthenticated, "valid node client certificate is required")
	}
	nodeID := info.State.VerifiedChains[0][0].Subject.CommonName
	if nodeID == "" {
		return "", status.Error(codes.Unauthenticated, "node certificate has no identity")
	}
	return nodeID, nil
}
