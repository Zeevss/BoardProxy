package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	nodev1 "bproxy-control-plane/api/node/v1"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) applyDesired(ctx context.Context, stream grpc.BidiStreamingClient[nodev1.NodeEvent, nodev1.HubCommand], desired *nodev1.DesiredState) error {
	result := &nodev1.ApplyResult{DesiredRevision: desired.GetRevision(), ConfigSha256: desired.GetConfigSha256()}
	result.RuntimeRevision, result.Error = s.activateDesired(ctx, desired)
	result.AppliedAt = timestamppb.Now()
	if err := stream.Send(&nodev1.NodeEvent{Payload: &nodev1.NodeEvent_ApplyResult{ApplyResult: result}}); err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

func (s *Service) activateDesired(ctx context.Context, desired *nodev1.DesiredState) (uint64, string) {
	if desired.GetRevision() < s.state.Revision {
		return 0, "stale desired revision"
	}
	if desired.GetRevision() == s.state.Revision {
		if desired.GetConfigSha256() != s.state.SHA256 {
			return 0, "same desired revision has a different config hash"
		}
		return 0, ""
	}
	digest := sha256.Sum256(desired.GetConfigToml())
	if hex.EncodeToString(digest[:]) != desired.GetConfigSha256() {
		return 0, "config sha256 mismatch"
	}
	runtimeRevision, err := s.core.Apply(ctx, desired.GetConfigToml())
	if err != nil {
		return 0, err.Error()
	}
	next := appliedState{Revision: desired.GetRevision(), SHA256: desired.GetConfigSha256()}
	raw, err := json.Marshal(next)
	if err != nil {
		return 0, err.Error()
	}
	if err := s.store.PutCheckpoint(agentStateKey, raw); err != nil {
		return 0, err.Error()
	}
	s.state = next
	return runtimeRevision, ""
}
