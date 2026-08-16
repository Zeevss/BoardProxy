package identity

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSecretAcceptsControlPlaneBase64URL(t *testing.T) {
	want := BootstrapSecret{
		NodeID:          "node-1",
		HubURL:          "hub:8443",
		EnrollmentToken: "one-time-token",
		CACertificate:   "࠾",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if !strings.ContainsAny(encoded, "-_") {
		t.Fatalf("fixture must exercise the URL-safe alphabet: %q", encoded)
	}

	got, err := parseSecret(encoded)
	if err != nil {
		t.Fatalf("parseSecret() error = %v", err)
	}
	if got != want {
		t.Fatalf("parseSecret() = %#v, want %#v", got, want)
	}
}

func TestParseSecretKeepsStandardBase64Compatibility(t *testing.T) {
	want := BootstrapSecret{
		NodeID:          "node-1",
		HubURL:          "hub:8443",
		EnrollmentToken: "one-time-token",
		CACertificate:   "࠾",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawStdEncoding.EncodeToString(raw)

	got, err := parseSecret(encoded)
	if err != nil {
		t.Fatalf("parseSecret() error = %v", err)
	}
	if got != want {
		t.Fatalf("parseSecret() = %#v, want %#v", got, want)
	}
}
