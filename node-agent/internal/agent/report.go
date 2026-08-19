package agent

import (
	"context"

	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// sendReports drains the outbox. Each pending row is a complete report with its
// own batch id, so a lost response costs one duplicate rather than a lost
// interval — the hub deduplicates by batch id.
//
// There is no separate acknowledgement round trip: a successful call is the
// acknowledgement.
func (s *Service) sendReports(ctx context.Context, client nodev1.NodeControlServiceClient) {
	pending, err := s.store.Pending()
	if err != nil {
		s.log.Warn("read outbox", "err", err)
		return
	}
	if len(pending) == 0 {
		// Health still has to reach the hub: "online" means "reported recently".
		s.deliver(ctx, client, &nodev1.ReportRequest{BatchId: randomID()}, false)
		return
	}
	for _, item := range pending {
		s.deliver(ctx, client, item.Event, true)
	}
}

func (s *Service) deliver(
	ctx context.Context,
	client nodev1.NodeControlServiceClient,
	report *nodev1.ReportRequest,
	drop bool,
) {
	s.seq++
	report.NodeId = s.identity.NodeID
	report.BootId = s.bootID
	report.Seq = s.seq
	// Health is filled at send time, not at collection time: it describes the
	// node now, not when the batch was gathered.
	report.Health = s.health(ctx)

	response, err := client.Report(ctx, report)
	if err != nil {
		s.log.Warn("send report", "batch", report.GetBatchId(), "err", err)
		return
	}
	if drop {
		if err := s.store.Ack(report.GetBatchId()); err != nil {
			s.log.Warn("clear delivered report", "batch", report.GetBatchId(), "err", err)
		}
	}
	for _, command := range response.GetCommands() {
		s.handleCommand(command)
	}
}

func (s *Service) health(ctx context.Context) *nodev1.Health {
	running, ready, failure := s.core.Status(ctx)
	applyError := s.applyError
	if applyError == "" && !running {
		applyError = failure
	}
	return &nodev1.Health{
		AppliedRevision: s.state.Revision,
		AppliedSha256:   s.state.SHA256,
		ApplyError:      applyError,
		CoreVersion:     s.coreVersion(ready),
		AgentVersion:    s.version,
		UptimeSeconds:   s.uptimeSeconds(),
		ObservedAt:      timestamppb.Now(),
	}
}

// The hub delivers a command exactly once, so an unknown kind must not be
// silently swallowed: it would never be offered again.
func (s *Service) handleCommand(command *nodev1.AgentCommand) {
	switch command.GetKind() {
	case "restart":
		s.log.Info("restart requested by hub", "nonce", command.GetNonce())
		s.requestRestart()
	default:
		s.log.Warn("unknown hub command ignored", "kind", command.GetKind(), "nonce", command.GetNonce())
	}
}
