package recovery

import (
	"bytes"
	"testing"
)

func TestNoiseIKRoundTrip(t *testing.T) {
	clientPrivate := bytes.Repeat([]byte{1}, 32)
	serverPrivate := bytes.Repeat([]byte{2}, 32)
	serverPublic, err := PublicKey(serverPrivate)
	if err != nil {
		t.Fatal(err)
	}
	clientPublic, err := PublicKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	initiation, message1, err := Initiate(clientPrivate, serverPublic, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := Respond(serverPrivate, message1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.PeerStatic(), clientPublic) || string(response.Payload()) != "hello" {
		t.Fatal("responder did not authenticate client payload")
	}
	message2, err := response.Accept([]byte("configuration"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := initiation.Complete(message2)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "configuration" {
		t.Fatalf("payload = %q", payload)
	}
}
