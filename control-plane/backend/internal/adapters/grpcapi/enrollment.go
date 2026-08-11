package grpcapi

import (
	"context"
	"errors"
	"time"

	nodev1 "bproxy-control-plane/api/node/v1"
	"bproxy-control-plane/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) Enroll(ctx context.Context, request *nodev1.EnrollRequest) (*nodev1.EnrollResponse, error) {
	if request.GetNodeId() == "" || request.GetToken() == "" || len(request.GetCsrPem()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "node_id, token and csr_pem are required")
	}
	if err := validateCSR(request.GetCsrPem()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	certificate, err := s.enrollment.Enroll(ctx, request.GetNodeId(), request.GetToken(), request.GetCsrPem())
	if err != nil {
		return nil, enrollmentError(err)
	}
	s.log.Info("node enrolled", "node", request.GetNodeId(), "agent_version", request.GetAgentVersion(), "expires", certificate.ExpiresAt)
	return certificateResponse(certificate.PEM, certificate.CAPEM, certificate.ExpiresAt), nil
}

func (s *Server) Renew(ctx context.Context, request *nodev1.RenewRequest) (*nodev1.EnrollResponse, error) {
	nodeID, err := authenticatedNodeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCSR(request.GetCsrPem()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	certificate, err := s.enrollment.Renew(nodeID, request.GetCsrPem())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	s.log.Info("node certificate renewed", "node", nodeID, "agent_version", request.GetAgentVersion(), "expires", certificate.ExpiresAt)
	return certificateResponse(certificate.PEM, certificate.CAPEM, certificate.ExpiresAt), nil
}

func enrollmentError(err error) error {
	switch {
	case errors.Is(err, domain.ErrTokenExpired):
		return status.Error(codes.Unauthenticated, "enrollment token expired")
	case errors.Is(err, domain.ErrTokenInvalid):
		return status.Error(codes.Unauthenticated, "invalid enrollment token")
	default:
		return status.Error(codes.InvalidArgument, err.Error())
	}
}

func certificateResponse(certificate, ca []byte, expiresAt time.Time) *nodev1.EnrollResponse {
	return &nodev1.EnrollResponse{CertificatePem: certificate, CaCertificatePem: ca, ExpiresAt: timestamppb.New(expiresAt)}
}
