package crypto

import (
	"bytes"
	"testing"

	"bproxy-core/internal/codec"
)

func TestKeypairRoundTrip(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(kp.Private()) != KeySize || len(kp.Public()) != KeySize {
		t.Fatalf("длины ключей: priv=%d pub=%d, хочу %d", len(kp.Private()), len(kp.Public()), KeySize)
	}

	restored, err := KeypairFromPrivate(kp.Private())
	if err != nil {
		t.Fatalf("KeypairFromPrivate: %v", err)
	}
	if !bytes.Equal(restored.Public(), kp.Public()) {
		t.Fatal("восстановленный публичный ключ не совпал с исходным")
	}

	if err := ValidatePublic(kp.Public()); err != nil {
		t.Fatalf("ValidatePublic(валидный) = %v", err)
	}
	if err := ValidatePublic([]byte("слишком коротко")); err == nil {
		t.Fatal("ValidatePublic(мусор) = nil, хочу ошибку")
	}
	if _, err := KeypairFromPrivate([]byte("короткий")); err == nil {
		t.Fatal("KeypairFromPrivate(мусор) = nil, хочу ошибку")
	}
}

func key(b byte) [KeySize]byte {
	var k [KeySize]byte
	for i := range k {
		k[i] = b
	}
	return k
}

func TestSealedRoundTrip(t *testing.T) {
	a, b := key(1), key(2)
	// Отправитель шифрует a / расшифровывает b; получатель — наоборот.
	sender, err := NewSealed(codec.Z85Codec{}, a, b)
	if err != nil {
		t.Fatalf("NewSealed(sender): %v", err)
	}
	receiver, err := NewSealed(codec.Z85Codec{}, b, a)
	if err != nil {
		t.Fatalf("NewSealed(receiver): %v", err)
	}

	frame := []byte("привет, это кадр link поверх доски")
	obj, err := sender.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := receiver.Decode(obj)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("round-trip: got %q, хочу %q", got, frame)
	}
}

func TestSealedNonceIsRandom(t *testing.T) {
	k := key(7)
	s, err := NewSealed(codec.Z85Codec{}, k, k)
	if err != nil {
		t.Fatalf("NewSealed: %v", err)
	}
	frame := []byte("одинаковый кадр")
	a, err := s.Encode(frame)
	if err != nil {
		t.Fatalf("Encode(a): %v", err)
	}
	b, err := s.Encode(frame)
	if err != nil {
		t.Fatalf("Encode(b): %v", err)
	}
	if a == b {
		t.Fatal("два запечатывания одного кадра совпали — nonce не случаен")
	}
}

func TestSealedWrongKeyTreatedAsForeign(t *testing.T) {
	sender, _ := NewSealed(codec.Z85Codec{}, key(1), key(2))
	// Получатель с ключами не от этого отправителя.
	receiver, _ := NewSealed(codec.Z85Codec{}, key(9), key(9))

	obj, err := sender.Encode([]byte("секрет"))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Чужой/неоткрывающийся объект должен трактоваться как «не наш», а не
	// рвать link непрозрачной ошибкой.
	if _, err := receiver.Decode(obj); err != codec.ErrNotProtocol {
		t.Fatalf("Decode(чужой ключ) = %v, хочу codec.ErrNotProtocol", err)
	}
}

func TestSealedTamperedRejected(t *testing.T) {
	k := key(3)
	s, _ := NewSealed(codec.Z85Codec{}, k, k)
	obj, err := s.Encode([]byte("данные под защитой Poly1305"))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Портим последний символ Z85-тела — расшифровка обязана провалиться.
	tampered := []byte(obj)
	last := tampered[len(tampered)-1]
	if last == '0' {
		tampered[len(tampered)-1] = '1'
	} else {
		tampered[len(tampered)-1] = '0'
	}
	if _, err := s.Decode(string(tampered)); err != codec.ErrNotProtocol {
		t.Fatalf("Decode(подделка) = %v, хочу codec.ErrNotProtocol", err)
	}
}

func TestSealedPassesThroughNonProtocol(t *testing.T) {
	k := key(4)
	s, _ := NewSealed(codec.Z85Codec{}, k, k)
	// Объект без нашего маркера: внутренний кодек вернёт ErrNotProtocol ещё
	// до попытки расшифровки.
	if _, err := s.Decode("человеческая заметка на доске"); err != codec.ErrNotProtocol {
		t.Fatalf("Decode(немаркированный) = %v, хочу codec.ErrNotProtocol", err)
	}
}
