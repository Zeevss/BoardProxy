package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	nodev1 "bproxy-control-plane/api/node/v1"
	"bproxy-control-plane/internal/application"
	"bproxy-control-plane/internal/domain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Notifications make in-process changes immediate. Polling is deliberately
// retained as reconciliation for another hub process/CLI writing the shared
// filesystem adapter or for a lost notification.
const desiredReconcileInterval = 2 * time.Second

type receivedEvent struct {
	event *nodev1.NodeEvent
	err   error
}

func (s *Server) Connect(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand]) error {
	certificateNodeID, err := authenticatedNodeID(stream.Context())
	if err != nil {
		return err
	}
	hello, err := receiveHello(stream, certificateNodeID)
	if err != nil {
		return err
	}
	session := application.NewNodeSession(s.desired, s.statuses, s.traffic, certificateNodeID, domain.AppliedState{
		Revision: hello.GetAppliedRevision(), SHA256: hello.GetConfigSha256(),
	})
	if err := session.Connected(stream.Context(), domain.NodeHello{
		BootID: hello.GetBootId(), AgentVersion: hello.GetAgentVersion(), CoreVersion: hello.GetCoreVersion(),
		AppliedRevision: hello.GetAppliedRevision(), ConfigSHA256: hello.GetConfigSha256(),
	}, time.Now()); err != nil {
		return err
	}
	s.log.Info("node stream connected", "node", certificateNodeID, "boot", hello.GetBootId(),
		"agent_version", hello.GetAgentVersion(), "applied_revision", hello.GetAppliedRevision())
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := session.Disconnected(cleanupContext, time.Now()); err != nil {
			s.log.Warn("persist node disconnect", "node", certificateNodeID, "err", err)
		}
		s.log.Info("node stream disconnected", "node", certificateNodeID)
	}()

	desiredChanged, unsubscribe := s.events.Subscribe(certificateNodeID)
	defer unsubscribe()
	if err := sendDesired(stream, session); err != nil {
		return err
	}
	events := receiveEvents(stream)
	ticker := time.NewTicker(desiredReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			if err := sendDesired(stream, session); err != nil {
				return err
			}
		case <-desiredChanged:
			if err := sendDesired(stream, session); err != nil {
				return err
			}
		case received := <-events:
			if errors.Is(received.err, io.EOF) {
				return nil
			}
			if received.err != nil {
				return received.err
			}
			if err := s.handleEvent(stream, session, received.event); err != nil {
				return err
			}
		}
	}
}

func receiveHello(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand], certificateNodeID string) (*nodev1.NodeHello, error) {
	first, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	hello := first.GetHello()
	if hello == nil {
		return nil, status.Error(codes.FailedPrecondition, "hello must be the first node event")
	}
	if hello.GetNodeId() != certificateNodeID {
		return nil, status.Error(codes.PermissionDenied, "hello node_id does not match client certificate")
	}
	return hello, nil
}

func receiveEvents(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand]) <-chan receivedEvent {
	events := make(chan receivedEvent, 1)
	go func() {
		for {
			event, err := stream.Recv()
			select {
			case events <- receivedEvent{event: event, err: err}:
			case <-stream.Context().Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return events
}

func sendDesired(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand], session *application.NodeSession) error {
	desired, send, err := session.PendingDesired(stream.Context(), time.Now())
	if err != nil || !send {
		return err
	}
	return stream.Send(&nodev1.HubCommand{Payload: &nodev1.HubCommand_DesiredState{DesiredState: &nodev1.DesiredState{
		Revision: desired.Revision, ConfigToml: desired.ConfigTOML, ConfigSha256: desired.ConfigSHA256,
	}}})
}

func (s *Server) handleEvent(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand], session *application.NodeSession, event *nodev1.NodeEvent) error {
	if result := event.GetApplyResult(); result != nil {
		appliedAt := time.Now().UTC()
		if result.GetAppliedAt() != nil {
			appliedAt = result.GetAppliedAt().AsTime()
		}
		if err := session.RecordApply(stream.Context(), domain.ApplyResult{
			DesiredRevision: result.GetDesiredRevision(), RuntimeRevision: result.GetRuntimeRevision(),
			ConfigSHA256: result.GetConfigSha256(), Error: result.GetError(), AppliedAt: appliedAt,
		}, time.Now()); err != nil {
			return err
		}
		level := slog.LevelInfo
		if result.GetError() != "" {
			level = slog.LevelWarn
		}
		s.log.Log(stream.Context(), level, "node apply result", "node", session.NodeID(), "desired_revision", result.GetDesiredRevision(),
			"runtime_revision", result.GetRuntimeRevision(), "err", result.GetError())
		return nil
	}
	if heartbeat := event.GetHeartbeat(); heartbeat != nil {
		sampledAt := time.Now().UTC()
		if heartbeat.GetSampledAt() != nil {
			sampledAt = heartbeat.GetSampledAt().AsTime()
		}
		return session.RecordHeartbeat(stream.Context(), domain.NodeHeartbeat{
			SampledAt: sampledAt, CoreRunning: heartbeat.GetCoreRunning(), CoreReady: heartbeat.GetCoreReady(),
			AppliedRevision: heartbeat.GetAppliedRevision(), Error: heartbeat.GetError(),
		})
	}
	if batch := event.GetInterfaceTraffic(); batch != nil {
		return persistAndAcknowledge(stream, session, domain.InterfaceTraffic, batch.GetBatchId(), batch)
	}
	if batch := event.GetUserTraffic(); batch != nil {
		return persistAndAcknowledge(stream, session, domain.UserTraffic, batch.GetBatchId(), batch)
	}
	return nil
}

func persistAndAcknowledge(stream grpc.BidiStreamingServer[nodev1.NodeEvent, nodev1.HubCommand], session *application.NodeSession, kind domain.TrafficKind, batchID string, message proto.Message) error {
	if batchID == "" {
		return status.Error(codes.InvalidArgument, "traffic batch_id is required")
	}
	raw, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	if err := session.StoreTraffic(stream.Context(), kind, batchID, raw); err != nil {
		return fmt.Errorf("persist %s traffic: %w", kind, err)
	}
	return stream.Send(&nodev1.HubCommand{Payload: &nodev1.HubCommand_TrafficAck{TrafficAck: &nodev1.TrafficAck{BatchId: batchID}}})
}
