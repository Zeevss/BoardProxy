package protocol

import (
	"bytes"
	"testing"
)

func TestSubscriptionURLRoundTrip(t *testing.T) {
	capsule := Capsule{
		Version: 1, YandexURL: "https://disk.yandex.ru/i/example", RecoveryKeyID: "key-1",
		ClientPrivateKey:     EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: EncodeKey(bytes.Repeat([]byte{2}, 32)),
	}
	raw, err := BuildURL("https://sub.example.com", "bps_token", capsule)
	if err != nil {
		t.Fatal(err)
	}
	requestURL, token, decoded, err := ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if token != "bps_token" || requestURL.Fragment != "" || decoded != capsule {
		t.Fatalf("round trip mismatch: token=%q URL=%q capsule=%+v", token, requestURL, decoded)
	}
}

func TestCapsuleRejectsNonYandexRecoveryURL(t *testing.T) {
	capsule := Capsule{
		Version: 1, YandexURL: "https://attacker.example/sheet", RecoveryKeyID: "r1",
		ClientPrivateKey:     EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: EncodeKey(bytes.Repeat([]byte{2}, 32)),
	}
	if _, err := capsule.Encode(); err == nil {
		t.Fatal("expected an untrusted recovery URL to be rejected")
	}
}

func TestFrameRoundTrip(t *testing.T) {
	want := Frame{Type: "hello", RequestID: "request", KeyID: "key", Part: 1, Parts: 1, Payload: []byte("noise")}
	encoded, err := EncodeFrame(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.RequestID != want.RequestID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("decoded frame = %+v", got)
	}
}
