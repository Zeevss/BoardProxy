// Package crypto — статические ключи идентичности BoardProxy (X25519) и
// запечатывающий кодек: конфиденциальность реализована как декоратор поверх
// codec.Codec, а не как отдельный слой стека. Пакет знает только о примитивах
// и о codec.Codec; ни link/mux, ни хранилище ему не видны — собирают эти
// примитивы вместе hub (рукопожатие) и keylink.
package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize — длина ключа X25519 в байтах (и приватного, и публичного).
const KeySize = 32

// ErrKeySize возвращается, когда сырые байты ключа имеют неверную длину или не
// являются валидной точкой кривой.
var ErrKeySize = errors.New("crypto: неверная длина ключа")

// Keypair — статическая пара ключей X25519 (долгосрочная идентичность
// участника). Приватный ключ — секрет владельца; сервер хранит только
// публичные ключи пользователей (см. store.User.PublicKey).
type Keypair struct {
	priv *ecdh.PrivateKey
}

// Generate создаёт новую случайную пару.
func Generate() (Keypair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Keypair{}, fmt.Errorf("crypto: generate: %w", err)
	}
	return Keypair{priv: priv}, nil
}

// KeypairFromPrivate восстанавливает пару из 32 байт приватного ключа
// (например, разобранных из keylink).
func KeypairFromPrivate(priv []byte) (Keypair, error) {
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return Keypair{}, ErrKeySize
	}
	return Keypair{priv: k}, nil
}

// Private возвращает 32 байта приватного ключа.
func (k Keypair) Private() []byte { return k.priv.Bytes() }

// Public возвращает 32 байта публичного ключа.
func (k Keypair) Public() []byte { return k.priv.PublicKey().Bytes() }

// ValidatePublic проверяет, что b — валидный 32-байтный публичный ключ X25519.
// Нужна там, где публичный ключ приходит извне (пре-провизионирование
// пользователей, разбор keylink), до передачи его в рукопожатие.
func ValidatePublic(b []byte) error {
	if _, err := ecdh.X25519().NewPublicKey(b); err != nil {
		return ErrKeySize
	}
	return nil
}
