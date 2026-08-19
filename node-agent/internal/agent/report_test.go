package agent

import (
	"context"
	"errors"
	"testing"

	"bproxy-node-agent/internal/localstore"
	nodev1 "bproxy-node-contracts/node/v1"

	"google.golang.org/grpc"
)

func TestReportsAreDroppedOnlyAfterTheHubAcceptsThem(t *testing.T) {
	service, store, _ := newTestService()
	store.pending = []localstore.Pending{
		{BatchID: "batch-1", Event: &nodev1.ReportRequest{BatchId: "batch-1"}},
	}
	client := &fakeClient{err: errors.New("hub unreachable")}

	service.sendReports(context.Background(), client)

	if len(store.acked) != 0 {
		t.Fatal("a failed delivery must keep the report for the next attempt")
	}
	if len(store.pending) != 1 {
		t.Fatalf("pending=%d, want 1", len(store.pending))
	}

	client.err = nil
	service.sendReports(context.Background(), client)

	if len(store.acked) != 1 {
		t.Fatalf("acked=%v, want batch-1 dropped after success", store.acked)
	}
	// The batch id survives the retry, so the hub can recognise the duplicate.
	if client.sent[0].GetBatchId() != client.sent[1].GetBatchId() {
		t.Fatal("retrying with a new batch id would defeat hub-side deduplication")
	}
}

// Health has to reach the hub even with nothing to report: "online" means
// "reported recently".
func TestHealthIsSentWhenTheOutboxIsEmpty(t *testing.T) {
	service, _, _ := newTestService()
	client := &fakeClient{}

	service.sendReports(context.Background(), client)

	if len(client.sent) != 1 {
		t.Fatalf("sent=%d, want 1", len(client.sent))
	}
	if client.sent[0].GetHealth() == nil {
		t.Fatal("a report without health tells the hub nothing about liveness")
	}
}

func TestEveryReportCarriesIdentityAndAMonotonicSequence(t *testing.T) {
	service, store, _ := newTestService()
	store.pending = []localstore.Pending{
		{BatchID: "batch-1", Event: &nodev1.ReportRequest{BatchId: "batch-1"}},
		{BatchID: "batch-2", Event: &nodev1.ReportRequest{BatchId: "batch-2"}},
	}
	client := &fakeClient{}

	service.sendReports(context.Background(), client)

	if len(client.sent) != 2 {
		t.Fatalf("sent=%d, want 2", len(client.sent))
	}
	for index, report := range client.sent {
		if report.GetBootId() != "boot-1" {
			t.Fatalf("report %d lost its boot id", index)
		}
		if report.GetSeq() != uint64(index+1) {
			t.Fatalf("seq=%d, want %d", report.GetSeq(), index+1)
		}
	}
}

// Health is filled at send time: it must describe the node now, not when the
// batch was collected.
func TestHealthReportsTheAppliedRevision(t *testing.T) {
	service, _, _ := newTestService()
	service.state = appliedState{Revision: 4, SHA256: "abc"}
	service.applyError = "toml parse failed"
	client := &fakeClient{}

	service.sendReports(context.Background(), client)

	health := client.sent[0].GetHealth()
	if health.GetAppliedRevision() != 4 || health.GetAppliedSha256() != "abc" {
		t.Fatalf("health=%+v, want revision 4", health)
	}
	if health.GetApplyError() != "toml parse failed" {
		t.Fatalf("apply error was not surfaced: %q", health.GetApplyError())
	}
}

func TestRestartCommandIsActedUpon(t *testing.T) {
	service, _, _ := newTestService()
	restarted := false
	service.restart = func() { restarted = true }
	client := &fakeClient{commands: []*nodev1.AgentCommand{{Nonce: 1, Kind: "restart"}}}

	service.sendReports(context.Background(), client)

	if !restarted {
		t.Fatal("the hub delivers a command once; ignoring it loses the restart")
	}
}

func TestUnknownCommandDoesNotRestart(t *testing.T) {
	service, _, _ := newTestService()
	restarted := false
	service.restart = func() { restarted = true }
	client := &fakeClient{commands: []*nodev1.AgentCommand{{Nonce: 1, Kind: "self-destruct"}}}

	service.sendReports(context.Background(), client)

	if restarted {
		t.Fatal("an unknown command must not be treated as a restart")
	}
}

type fakeClient struct {
	nodev1.NodeControlServiceClient
	sent     []*nodev1.ReportRequest
	commands []*nodev1.AgentCommand
	err      error
}

func (c *fakeClient) Report(
	_ context.Context,
	request *nodev1.ReportRequest,
	_ ...grpc.CallOption,
) (*nodev1.ReportResponse, error) {
	c.sent = append(c.sent, request)
	if c.err != nil {
		return nil, c.err
	}
	return &nodev1.ReportResponse{Commands: c.commands}, nil
}
