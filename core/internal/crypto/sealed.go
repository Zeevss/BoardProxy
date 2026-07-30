package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"bproxy-core/internal/codec"

	"golang.org/x/crypto/chacha20poly1305"
)

// Sealed оборачивает внутренний codec.Codec, добавляя конфиденциальность и
// аутентичность: каждый объект шифруется отдельно XChaCha20-Poly1305 со
// случайным 24-байтным nonce. Ключи направленные (seal ≠ open) — на одной
// стороне это c2s/s2c, на другой наоборот, — так что пересечения nonce между
// направлениями нет даже теоретически.
//
// По-объектная независимость — не оптимизация, а соответствие модели доски:
// объекты независимы, link уже трактует каждый сам по себе, поэтому Reconcile
// «просто работает» — любой оставшийся на странице объект расшифровывается
// своим nonce, без общего состояния шифра. 24-байтный nonce берём случайным:
// счётчик пришлось бы восстанавливать через обрыв/рестарт (риск повторного
// nonce), а при 192 битах случайности коллизия пренебрежимо маловероятна.
type Sealed struct {
	inner codec.Codec
	seal  cipher.AEAD
	open  cipher.AEAD
}

// NewSealed строит запечатывающий кодек поверх inner. sealKey шифрует
// исходящие объекты, openKey расшифровывает входящие.
func NewSealed(inner codec.Codec, sealKey, openKey [KeySize]byte) (Sealed, error) {
	seal, err := chacha20poly1305.NewX(sealKey[:])
	if err != nil {
		return Sealed{}, fmt.Errorf("crypto: seal aead: %w", err)
	}
	open, err := chacha20poly1305.NewX(openKey[:])
	if err != nil {
		return Sealed{}, fmt.Errorf("crypto: open aead: %w", err)
	}
	return Sealed{inner: inner, seal: seal, open: open}, nil
}

// Encode шифрует кадр и передаёт запечатанные байты внутреннему кодеку.
func (s Sealed) Encode(frame []byte) (string, error) {
	nonce := make([]byte, s.seal.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}
	// nonce ‖ ciphertext(+tag) — самодостаточный запечатанный объект.
	sealed := s.seal.Seal(nonce, nonce, frame, nil)
	return s.inner.Encode(sealed)
}

// Decode декодирует объект внутренним кодеком и расшифровывает его. Объект с
// нашим маркером, но не открывающийся нашим ключом (чужая BoardProxy-сессия на
// той же доске, повреждение), трактуется как «не наш» — codec.ErrNotProtocol,
// — чтобы посторонний объект не рвал link, а тихо игнорировался, как и любой
// немаркированный.
func (s Sealed) Decode(value string) ([]byte, error) {
	sealed, err := s.inner.Decode(value)
	if err != nil {
		return nil, err // в т.ч. codec.ErrNotProtocol — объект без нашего маркера
	}
	ns := s.open.NonceSize()
	if len(sealed) < ns {
		return nil, codec.ErrNotProtocol
	}
	frame, err := s.open.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return nil, codec.ErrNotProtocol
	}
	return frame, nil
}
