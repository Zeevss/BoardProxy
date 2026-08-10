package mgmt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bproxy-core/internal/telemetry"
)

type fakeStats struct{ value telemetry.Stats }

func (f fakeStats) Stats() telemetry.Stats { return f.value }

func TestReadOnlyHandler(t *testing.T) {
	h := Handler(fakeStats{value: telemetry.Stats{Revision: 7}}, nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/stats", nil))
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"revision":7`) {
		t.Fatalf("unexpected stats response: %d %s", resp.Code, resp.Body.String())
	}
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("POST", "/users", nil))
	if resp.Code != 404 {
		t.Fatalf("mutation endpoint unexpectedly exists: %d", resp.Code)
	}
}

func TestReadinessRequiresAnActiveEnabledBoard(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stats    telemetry.Stats
		wantCode int
	}{
		{name: "ready", stats: telemetry.Stats{BoardsEnabled: 2, BoardsRunning: 1}, wantCode: http.StatusOK},
		{name: "no active board", stats: telemetry.Stats{BoardsEnabled: 1}, wantCode: http.StatusServiceUnavailable},
		{name: "no enabled board", stats: telemetry.Stats{}, wantCode: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			Handler(fakeStats{value: tc.stats}, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/readyz", nil))
			if recorder.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.wantCode, recorder.Body.String())
			}
		})
	}
}
