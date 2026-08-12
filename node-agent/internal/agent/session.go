package agent

import (
	"context"
	"time"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const outboxReconcileInterval = 30 * time.Second
const outboxAcknowledgementTimeout = 30 * time.Second

type receivedCommand struct {
	command *nodev1.HubCommand
	err     error
}

func (s *Service) connect(ctx context.Context) error {
	connection, err := grpc.NewClient(s.identity.HubURL, grpc.WithTransportCredentials(credentials.NewTLS(s.identity.TLS.Clone())))
	if err != nil {
		return err
	}
	defer connection.Close()
	stream, err := nodev1.NewNodeControlServiceClient(connection).Connect(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(s.hello()); err != nil {
		return err
	}
	s.log.Info("hub stream connected", "hub", s.identity.HubURL, "node", s.identity.NodeID)
	sent := make(map[string]time.Time)
	if err := s.flushOutbox(stream, sent); err != nil {
		return err
	}
	commands := receiveCommands(stream)
	heartbeats := time.NewTicker(s.config.Heartbeat)
	defer heartbeats.Stop()
	outbox := time.NewTicker(outboxReconcileInterval)
	defer outbox.Stop()
	renewal := time.NewTimer(time.Until(s.identity.RenewAt))
	defer renewal.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-renewal.C:
			return errCertificateRenewal
		case received := <-commands:
			if received.err != nil {
				return received.err
			}
			if err := s.handleCommand(ctx, stream, received.command, sent); err != nil {
				return err
			}
		case <-heartbeats.C:
			if err := stream.Send(s.heartbeat(ctx)); err != nil {
				return err
			}
		case <-outbox.C:
			if err := s.flushOutbox(stream, sent); err != nil {
				return err
			}
		case <-s.store.Changes():
			if err := s.flushOutbox(stream, sent); err != nil {
				return err
			}
		}
	}
}

func (s *Service) hello() *nodev1.NodeEvent {
	return &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_Hello{Hello: &nodev1.NodeHello{
		NodeId: s.identity.NodeID, BootId: s.bootID, AgentVersion: s.version,
		AppliedRevision: s.state.Revision, ConfigSha256: s.state.SHA256,
	}}}
}

func (s *Service) heartbeat(ctx context.Context) *nodev1.NodeEvent {
	running, ready, failure := s.core.Status(ctx)
	return &nodev1.NodeEvent{Payload: &nodev1.NodeEvent_Heartbeat{Heartbeat: &nodev1.NodeHeartbeat{
		SampledAt: timestamppb.Now(), CoreRunning: running, CoreReady: ready,
		AppliedRevision: s.state.Revision, Error: failure,
	}}}
}

func receiveCommands(stream grpc.BidiStreamingClient[nodev1.NodeEvent, nodev1.HubCommand]) <-chan receivedCommand {
	commands := make(chan receivedCommand, 1)
	go func() {
		for {
			command, err := stream.Recv()
			select {
			case commands <- receivedCommand{command: command, err: err}:
			case <-stream.Context().Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return commands
}

func (s *Service) handleCommand(ctx context.Context, stream grpc.BidiStreamingClient[nodev1.NodeEvent, nodev1.HubCommand], command *nodev1.HubCommand, sent map[string]time.Time) error {
	if desired := command.GetDesiredState(); desired != nil {
		if err := s.applyDesired(ctx, stream, desired); err != nil {
			s.log.Warn("desired state rejected", "revision", desired.GetRevision(), "err", err)
		}
	}
	if acknowledgement := command.GetTrafficAck(); acknowledgement != nil {
		if err := s.store.Ack(acknowledgement.GetBatchId()); err != nil {
			return err
		}
		delete(sent, acknowledgement.GetBatchId())
	}
	if acknowledgement := command.GetRuntimeEventAck(); acknowledgement != nil {
		if err := s.store.Ack(acknowledgement.GetBatchId()); err != nil {
			return err
		}
		delete(sent, acknowledgement.GetBatchId())
	}
	return nil
}

func (s *Service) flushOutbox(stream grpc.BidiStreamingClient[nodev1.NodeEvent, nodev1.HubCommand], sent map[string]time.Time) error {
	pending, err := s.store.Pending()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, item := range pending {
		if sentAt, exists := sent[item.BatchID]; exists && now.Sub(sentAt) < outboxAcknowledgementTimeout {
			continue
		}
		if err := stream.Send(item.Event); err != nil {
			return err
		}
		sent[item.BatchID] = now
	}
	return nil
}
