package grpcapi

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"

	nodev1 "bproxy-control-plane/api/node/v1"
	"bproxy-control-plane/internal/application"
	"bproxy-control-plane/internal/ports"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Server struct {
	nodev1.UnimplementedNodeControlServiceServer
	enrollment *application.Enrollment
	desired    ports.DesiredRevisions
	statuses   ports.NodeStatusStore
	traffic    ports.TrafficSink
	events     ports.DesiredNotifier
	log        *slog.Logger
}

func New(enrollment *application.Enrollment, desired ports.DesiredRevisions, statuses ports.NodeStatusStore, traffic ports.TrafficSink, events ports.DesiredNotifier, log *slog.Logger) *Server {
	return &Server{enrollment: enrollment, desired: desired, statuses: statuses, traffic: traffic, events: events, log: log}
}

func Serve(ctx context.Context, address string, tokens ports.EnrollmentTokens, desired ports.DesiredRevisions, statuses ports.NodeStatusStore, traffic ports.TrafficSink, events ports.DesiredNotifier, authority ports.CertificateAuthority, log *slog.Logger) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{authority.ServerCertificate()}, ClientCAs: authority.CertPool(),
		ClientAuth: tls.VerifyClientCertIfGiven, MinVersion: tls.VersionTLS13,
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	nodev1.RegisterNodeControlServiceServer(server, New(
		application.NewEnrollment(tokens, authority), desired, statuses, traffic, events, log,
	))
	go stopOnContext(ctx, server)
	log.Info("node control listening", "address", address, "tls", "mTLS")
	if err := server.Serve(listener); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func stopOnContext(ctx context.Context, server *grpc.Server) {
	<-ctx.Done()
	server.GracefulStop()
}
