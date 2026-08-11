package ports

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"time"

	"bproxy-control-plane/internal/domain"
)

type EnrollmentTokens interface {
	CreateEnrollmentToken(context.Context, string, time.Duration) (string, error)
	ConsumeEnrollmentToken(context.Context, string, string) error
}

type DesiredRevisions interface {
	AppendDesired(context.Context, string, uint64, uint64, []byte, string) (domain.ConfigRevision, error)
	Desired(context.Context, string) (domain.ConfigRevision, error)
	DesiredHistory(context.Context, string) ([]domain.ConfigRevision, error)
}

type TrafficSink interface {
	StoreTraffic(context.Context, string, domain.TrafficKind, string, []byte) error
}

type CatalogStore interface {
	Catalog(context.Context, string) (domain.Catalog, error)
	ListCatalogs(context.Context) ([]domain.Catalog, error)
	SaveCatalog(context.Context, domain.Catalog, uint64) error
}

type NodeStatusStore interface {
	NodeStatus(context.Context, string) (domain.NodeStatus, error)
	SaveNodeStatus(context.Context, domain.NodeStatus, uint64) error
}

type AuditLog interface {
	AppendAudit(context.Context, domain.AuditEvent) error
}

type ConfigCompiler interface {
	Compile(domain.Catalog) ([]byte, error)
}

type DesiredNotifier interface {
	Subscribe(string) (<-chan struct{}, func())
	Notify(string)
}

type CertificateAuthority interface {
	CAPEM() []byte
	ServerCertificate() tls.Certificate
	CertPool() *x509.CertPool
	SignNodeCSR(string, []byte) (domain.Certificate, error)
}
