package recovery

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/flynn/noise"
)

var cipherSuite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

var ErrIncomplete = errors.New("recovery Noise IK handshake is incomplete")

type Initiation struct{ state *noise.HandshakeState }

func Initiate(clientPrivate, serverPublic, payload []byte) (*Initiation, []byte, error) {
	client, err := keypair(clientPrivate)
	if err != nil {
		return nil, nil, err
	}
	if len(serverPublic) != 32 {
		return nil, nil, errors.New("recovery server public key must contain 32 bytes")
	}
	state, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: cipherSuite, Pattern: noise.HandshakeIK, Initiator: true,
		StaticKeypair: client, PeerStatic: serverPublic,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize recovery handshake: %w", err)
	}
	message, _, _, err := state.WriteMessage(nil, payload)
	if err != nil {
		return nil, nil, fmt.Errorf("write recovery hello: %w", err)
	}
	return &Initiation{state: state}, message, nil
}

func (i *Initiation) Complete(message []byte) ([]byte, error) {
	payload, send, receive, err := i.state.ReadMessage(nil, message)
	if err != nil {
		return nil, fmt.Errorf("read recovery response: %w", err)
	}
	if send == nil || receive == nil {
		return nil, ErrIncomplete
	}
	return payload, nil
}

type Response struct {
	state      *noise.HandshakeState
	peerStatic []byte
	payload    []byte
}

func Respond(serverPrivate, message []byte) (*Response, error) {
	server, err := keypair(serverPrivate)
	if err != nil {
		return nil, err
	}
	state, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: cipherSuite, Pattern: noise.HandshakeIK,
		StaticKeypair: server,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize recovery responder: %w", err)
	}
	payload, _, _, err := state.ReadMessage(nil, message)
	if err != nil {
		return nil, fmt.Errorf("read recovery hello: %w", err)
	}
	peer := state.PeerStatic()
	if len(peer) != 32 {
		return nil, errors.New("recovery client did not present a static key")
	}
	return &Response{state: state, peerStatic: append([]byte(nil), peer...), payload: payload}, nil
}

func (r *Response) PeerStatic() []byte { return append([]byte(nil), r.peerStatic...) }
func (r *Response) Payload() []byte    { return append([]byte(nil), r.payload...) }

func (r *Response) Accept(payload []byte) ([]byte, error) {
	message, receive, send, err := r.state.WriteMessage(nil, payload)
	if err != nil {
		return nil, fmt.Errorf("write recovery response: %w", err)
	}
	if send == nil || receive == nil {
		return nil, ErrIncomplete
	}
	return message, nil
}

func PublicKey(private []byte) ([]byte, error) {
	pair, err := keypair(private)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), pair.Public...), nil
}

func keypair(private []byte) (noise.DHKey, error) {
	if len(private) != 32 {
		return noise.DHKey{}, errors.New("recovery private key must contain 32 bytes")
	}
	pair, err := noise.DH25519.GenerateKeypair(&fixedReader{data: append([]byte(nil), private...)})
	if err != nil {
		return noise.DHKey{}, fmt.Errorf("derive recovery public key: %w", err)
	}
	return pair, nil
}

type fixedReader struct{ data []byte }

func (r *fixedReader) Read(target []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, errors.New("recovery private key reader exhausted")
	}
	n := copy(target, r.data)
	r.data = r.data[n:]
	return n, nil
}

// DecodeKey разбирает 32-байтовый ключ в base64url или стандартном base64.
// Нулевой ключ отвергается: он означает потерянный или незаполненный секрет.
func DecodeKey(encoded string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(encoded), "base64:")
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(trimmed)
	}
	if err != nil || len(raw) != 32 {
		return nil, errors.New("recovery key must contain 32 base64 bytes")
	}
	for _, value := range raw {
		if value != 0 {
			return raw, nil
		}
	}
	return nil, errors.New("recovery key must not be all zero")
}
