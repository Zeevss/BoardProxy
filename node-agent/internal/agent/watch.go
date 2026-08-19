package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// The hub only announces revision numbers. Deciding when to fetch, and retrying
// a failed apply, is the node's job — which is why the hub keeps no per-session
// state and needs no lease.
const (
	reconcileInterval = 30 * time.Second
	reportInterval    = 15 * time.Second
)

func (s *Service) connect(ctx context.Context) error {
	connection, err := grpc.NewClient(
		s.identity.HubURL,
		grpc.WithTransportCredentials(credentials.NewTLS(s.identity.TLS.Clone())),
	)
	if err != nil {
		return err
	}
	defer connection.Close()

	client := nodev1.NewNodeControlServiceClient(connection)
	stream, err := client.Watch(ctx, &nodev1.WatchRequest{NodeId: s.identity.NodeID, AgentVersion: s.version})
	if err != nil {
		return err
	}
	s.log.Info("watching hub", "hub", s.identity.HubURL, "node", s.identity.NodeID)

	// The initial sync covers the case where the configuration changed while the
	// agent was down: no notice will arrive for a change that already happened.
	s.syncConfig(ctx, client)

	notices := receiveNotices(stream)
	reports := time.NewTicker(reportInterval)
	defer reports.Stop()
	reconcile := time.NewTicker(reconcileInterval)
	defer reconcile.Stop()
	renewal := time.NewTimer(time.Until(s.identity.RenewAt))
	defer renewal.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-renewal.C:
			return errCertificateRenewal
		case received := <-notices:
			if received.err != nil {
				return received.err
			}
			if received.notice.GetRevision() != s.state.Revision {
				s.syncConfig(ctx, client)
			}
		case <-reconcile.C:
			// Guards against a lost notice: without it a dropped signal would
			// leave the node on a stale configuration until someone else edits.
			s.syncConfig(ctx, client)
		case <-reports.C:
			s.sendReports(ctx, client)
		case <-s.store.Changes():
			s.sendReports(ctx, client)
		}
	}
}

type receivedNotice struct {
	notice *nodev1.ConfigNotice
	err    error
}

func receiveNotices(stream grpc.ServerStreamingClient[nodev1.ConfigNotice]) <-chan receivedNotice {
	notices := make(chan receivedNotice, 1)
	go func() {
		for {
			notice, err := stream.Recv()
			select {
			case notices <- receivedNotice{notice: notice, err: err}:
			case <-stream.Context().Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return notices
}

// syncConfig fetches and applies the current configuration. Failures are logged
// and left for the next reconcile: retrying is the node's responsibility.
func (s *Service) syncConfig(ctx context.Context, client nodev1.NodeControlServiceClient) {
	document, err := client.FetchConfig(ctx, &nodev1.FetchConfigRequest{NodeId: s.identity.NodeID})
	if err != nil {
		s.log.Warn("fetch config", "err", err)
		return
	}
	if err := s.applyConfig(ctx, document); err != nil {
		s.applyError = err.Error()
		s.log.Warn("apply config", "revision", document.GetRevision(), "err", err)
		return
	}
	s.applyError = ""
}

func (s *Service) applyConfig(ctx context.Context, document *nodev1.ConfigDocument) error {
	if document.GetRevision() < s.state.Revision {
		return errors.New("hub offered a stale revision")
	}
	if document.GetRevision() == s.state.Revision {
		if document.GetConfigSha256() != s.state.SHA256 {
			return errors.New("same revision carries a different config hash")
		}
		return nil
	}
	digest := sha256.Sum256(document.GetConfigToml())
	if hex.EncodeToString(digest[:]) != document.GetConfigSha256() {
		return errors.New("config sha256 mismatch")
	}
	if _, err := s.core.Apply(ctx, document.GetConfigToml()); err != nil {
		return err
	}
	next := appliedState{Revision: document.GetRevision(), SHA256: document.GetConfigSha256()}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := s.store.PutCheckpoint(agentStateKey, raw); err != nil {
		return err
	}
	s.state = next
	s.log.Info("configuration applied", "revision", next.Revision)
	return nil
}
