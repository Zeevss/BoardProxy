package mobile

import (
	"encoding/json"
	"sync"
	"testing"
)

type testListener struct {
	mu       sync.Mutex
	statuses []string
	messages []string
}

func (l *testListener) OnStatus(status, message string) {
	l.mu.Lock()
	l.statuses = append(l.statuses, status)
	l.messages = append(l.messages, message)
	l.mu.Unlock()
}
func (*testListener) OnLog(string, string) {}
func (*testListener) OnMetrics(string)     {}

func TestNewClientRejectsUnknownConfigField(t *testing.T) {
	if _, err := CreateClient(`{"keylink":"x","typo":true}`, nil, nil); err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
}

func TestUDPIsSupportedByMobileConfig(t *testing.T) {
	_, err := CreateClient(`{"keylink":"placeholder","enable_udp":true}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !SupportsUDP() {
		t.Fatal("SupportsUDP returned false with datagram transport enabled")
	}
}

func TestMetricsJSONHasStableLowercaseSchema(t *testing.T) {
	c, err := CreateClient(`{"keylink":"placeholder"}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(c.MetricsJSON()), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "rtt_ms", "streams", "datagrams", "total_tx", "total_rx", "details"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("MetricsJSON missing key %q: %v", key, got)
		}
	}
}
