package yandex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRESTCallRejectsHTTPErrorWithoutOutput(t *testing.T) {
	c := &restClient{
		base: "https://boards.example/api",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("maintenance")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	err := c.call(context.Background(), "request-guest-token", map[string]string{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("call error = %v, want HTTP 503", err)
	}
}
