// Package handshake реализует взаимно аутентифицированное согласование ключа
// поверх Noise IK (github.com/flynn/noise). Инициатор (клиент) заранее знает
// статический ключ ответчика — из keylink; ответчик (сервер) узнаёт личность
// инициатора уже в ходе рукопожатия, из зашифрованного первого сообщения, и
// проверяет её снаружи (hub → store), не втягивая хранилище в крипто-конечный
// автомат. Так IK прячет личность клиента от наблюдателя на доске (в отличие от
// KK, где ответчику пришлось бы знать её заранее и клиент светил бы отпечаток
// открытым текстом), а слой crypto остаётся чистым.
//
// Пакет чистый: на вход — статические ключи, на выход — направленные ключи
// трафика (для crypto.NewSealed) и полезная нагрузка второго сообщения (id
// выданной страницы, едущий зашифрованным). Ни I/O, ни доступа к store.
package handshake

import (
	"errors"
	"fmt"

	"bproxy-core/internal/crypto"

	"github.com/flynn/noise"
)

// Keys — направленные ключи трафика после рукопожатия. Send шифрует исходящее,
// Recv расшифровывает входящее; у инициатора и ответчика они зеркальны.
type Keys struct {
	Send [crypto.KeySize]byte
	Recv [crypto.KeySize]byte
}

// cipherSuite — Noise_IK_25519_ChaChaPoly_SHA256. ChaChaPoly здесь шифрует
// только сами сообщения рукопожатия; трафик потом шифрует crypto.Sealed
// (XChaCha20-Poly1305) на свежих ключах из split — это разные употребления.
var cipherSuite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

var (
	errIncomplete   = errors.New("handshake: рукопожатие не завершилось (нет ключей)")
	errNoPeerStatic = errors.New("handshake: инициатор не предъявил статический ключ")
)

func dhKey(k crypto.Keypair) noise.DHKey {
	return noise.DHKey{Private: k.Private(), Public: k.Public()}
}

// Initiation — состояние инициатора между отправкой первого сообщения и
// приёмом ответа.
type Initiation struct {
	hs  *noise.HandshakeState
	msg []byte
}

// Initiate начинает рукопожатие к ответчику с публичным ключом remoteStatic.
func Initiate(local crypto.Keypair, remoteStatic []byte) (*Initiation, error) {
	return InitiateWithPayload(local, remoteStatic, nil)
}

// InitiateWithPayload starts Noise IK and carries authenticated client metadata
// in message 1. V3 rendezvous uses it for NEW_BUNDLE/JOIN_LANE data.
func InitiateWithPayload(local crypto.Keypair, remoteStatic, payload []byte) (*Initiation, error) {
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   cipherSuite,
		Pattern:       noise.HandshakeIK,
		Initiator:     true,
		StaticKeypair: dhKey(local),
		PeerStatic:    remoteStatic,
	})
	if err != nil {
		return nil, fmt.Errorf("handshake: init: %w", err)
	}
	msg, _, _, err := hs.WriteMessage(nil, payload)
	if err != nil {
		return nil, fmt.Errorf("handshake: write msg1: %w", err)
	}
	return &Initiation{hs: hs, msg: msg}, nil
}

// Message возвращает первое сообщение рукопожатия для отправки ответчику
// (внутри HELLO).
func (i *Initiation) Message() []byte { return i.msg }

// Complete обрабатывает ответ, завершает рукопожатие и возвращает ключи
// трафика и расшифрованную нагрузку (id выданной страницы).
func (i *Initiation) Complete(resp []byte) (Keys, []byte, error) {
	payload, cs0, cs1, err := i.hs.ReadMessage(nil, resp)
	if err != nil {
		return Keys{}, nil, fmt.Errorf("handshake: read response: %w", err)
	}
	if cs0 == nil || cs1 == nil {
		return Keys{}, nil, errIncomplete
	}
	// Split возвращает (init→resp, resp→init). Инициатор шифрует первым,
	// расшифровывает вторым.
	return Keys{Send: cs0.UnsafeKey(), Recv: cs1.UnsafeKey()}, payload, nil
}

// Response — состояние ответчика между приёмом первого сообщения и отправкой
// ответа.
type Response struct {
	hs      *noise.HandshakeState
	peer    []byte
	payload []byte
}

// Respond обрабатывает первое сообщение инициатора. Личность инициатора (его
// статический публичный ключ) к этому моменту уже криптографически
// подтверждена и доступна через PeerStatic — вызывающий (hub) сверяет её со
// store, прежде чем принять рукопожатие через Accept.
func Respond(local crypto.Keypair, msg1 []byte) (*Response, error) {
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   cipherSuite,
		Pattern:       noise.HandshakeIK,
		Initiator:     false,
		StaticKeypair: dhKey(local),
	})
	if err != nil {
		return nil, fmt.Errorf("handshake: respond: %w", err)
	}
	payload, _, _, err := hs.ReadMessage(nil, msg1)
	if err != nil {
		return nil, fmt.Errorf("handshake: read msg1: %w", err)
	}
	peer := hs.PeerStatic()
	if len(peer) != crypto.KeySize {
		return nil, errNoPeerStatic
	}
	return &Response{
		hs:      hs,
		peer:    peer,
		payload: append([]byte(nil), payload...),
	}, nil
}

// PeerStatic возвращает подтверждённый публичный ключ инициатора.
func (r *Response) PeerStatic() []byte { return r.peer }

// Payload returns the authenticated metadata carried in the initiator's first
// Noise message. The returned slice is detached from the handshake state.
func (r *Response) Payload() []byte { return append([]byte(nil), r.payload...) }

// Accept завершает рукопожатие: payload (id выданной страницы) едет в
// зашифрованном ответе; наружу возвращаются ключи трафика и само сообщение
// ответа для отправки инициатору.
func (r *Response) Accept(payload []byte) (Keys, []byte, error) {
	msg2, cs0, cs1, err := r.hs.WriteMessage(nil, payload)
	if err != nil {
		return Keys{}, nil, fmt.Errorf("handshake: write response: %w", err)
	}
	if cs0 == nil || cs1 == nil {
		return Keys{}, nil, errIncomplete
	}
	// Split возвращает (init→resp, resp→init). Ответчик шифрует вторым,
	// расшифровывает первым — зеркально инициатору.
	return Keys{Send: cs1.UnsafeKey(), Recv: cs0.UnsafeKey()}, msg2, nil
}
