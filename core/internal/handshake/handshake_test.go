package handshake

import (
	"bytes"
	"testing"

	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
)

// runIK прогоняет полное рукопожатие клиент↔сервер и возвращает обе стороны
// ключей плюс доставленную нагрузку (id страницы).
func runIK(t *testing.T, client, server crypto.Keypair, page []byte) (clientKeys, serverKeys Keys, peerStatic, delivered []byte) {
	t.Helper()

	init, err := Initiate(client, server.Public())
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	resp, err := Respond(server, init.Message())
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	serverKeys, msg2, err := resp.Accept(page)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	clientKeys, delivered, err = init.Complete(msg2)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return clientKeys, serverKeys, resp.PeerStatic(), delivered
}

func TestHandshakeDerivesMirroredKeys(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()

	ck, sk, peer, page := runIK(t, client, server, []byte("page-hash-42"))

	// Ключи зеркальны: что клиент шлёт, то сервер принимает, и наоборот.
	if ck.Send != sk.Recv {
		t.Fatal("client.Send != server.Recv")
	}
	if ck.Recv != sk.Send {
		t.Fatal("client.Recv != server.Send")
	}
	// Направления различны — иначе один ключ на оба потока.
	if ck.Send == ck.Recv {
		t.Fatal("оба направления клиента используют один ключ")
	}
	if !bytes.Equal(peer, client.Public()) {
		t.Fatal("сервер не увидел публичный ключ клиента как PeerStatic")
	}
	if string(page) != "page-hash-42" {
		t.Fatalf("нагрузка msg2 = %q, хочу page-hash-42", page)
	}
}

func TestHandshakeCarriesAuthenticatedInitiatorPayload(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	want := []byte("new-bundle metadata")

	init, err := InitiateWithPayload(client, server.Public(), want)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := Respond(server, init.Message())
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("response payload = %q, want %q", got, want)
	}

	// Payload returns a copy: callers cannot mutate authenticated state shared
	// with a later rendezvous handler.
	got := resp.Payload()
	got[0] ^= 1
	if bytes.Equal(resp.Payload(), got) {
		t.Fatal("Response.Payload returned shared mutable storage")
	}
}

// Главная проверка: ключи рукопожатия реально стыкуются с crypto.Sealed —
// объект, запечатанный одной стороной, открывается другой.
func TestHandshakeKeysInteropWithSealed(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	ck, sk, _, _ := runIK(t, client, server, []byte("p"))

	clientCodec, err := crypto.NewSealed(codec.Z85Codec{}, ck.Send, ck.Recv)
	if err != nil {
		t.Fatalf("NewSealed(client): %v", err)
	}
	serverCodec, err := crypto.NewSealed(codec.Z85Codec{}, sk.Send, sk.Recv)
	if err != nil {
		t.Fatalf("NewSealed(server): %v", err)
	}

	// Клиент → сервер.
	up := []byte("данные от клиента")
	obj, err := clientCodec.Encode(up)
	if err != nil {
		t.Fatalf("client.Encode: %v", err)
	}
	got, err := serverCodec.Decode(obj)
	if err != nil {
		t.Fatalf("server.Decode: %v", err)
	}
	if !bytes.Equal(got, up) {
		t.Fatalf("up-link: got %q, хочу %q", got, up)
	}

	// Сервер → клиент.
	down := []byte("ответ от сервера")
	obj, err = serverCodec.Encode(down)
	if err != nil {
		t.Fatalf("server.Encode: %v", err)
	}
	got, err = clientCodec.Decode(obj)
	if err != nil {
		t.Fatalf("client.Decode: %v", err)
	}
	if !bytes.Equal(got, down) {
		t.Fatalf("down-link: got %q, хочу %q", got, down)
	}
}

func TestHandshakeRejectsWrongServerKey(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	impostor, _ := crypto.Generate()

	// Клиент думает, что говорит с server, а msg1 попадает к impostor —
	// IK-ответчик не сможет обработать (ss/es не сойдутся с его ключом).
	init, err := Initiate(client, server.Public())
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if _, err := Respond(impostor, init.Message()); err == nil {
		t.Fatal("Respond чужим ключом прошёл, хочу ошибку")
	}
}

func TestHandshakeRejectsTamperedMsg1(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()

	init, err := Initiate(client, server.Public())
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	msg1 := append([]byte(nil), init.Message()...)
	msg1[len(msg1)-1] ^= 0xff
	if _, err := Respond(server, msg1); err == nil {
		t.Fatal("Respond на испорченный msg1 прошёл, хочу ошибку")
	}
}

func TestHandshakeClientRejectsTamperedResponse(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()

	init, _ := Initiate(client, server.Public())
	resp, _ := Respond(server, init.Message())
	_, msg2, err := resp.Accept([]byte("page"))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	msg2[len(msg2)-1] ^= 0xff
	if _, _, err := init.Complete(msg2); err == nil {
		t.Fatal("Complete на испорченный ответ прошёл, хочу ошибку")
	}
}
