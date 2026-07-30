// Package keylink собирает и разбирает строку подключения BoardProxy вида
//
//	bproxy://<base64url(clientPriv32 ‖ serverPub32)>[@hash1,hash2,…][#label]
//
// Она несёт всё, что нужно клиенту для подключения: его приватный ключ
// (аутентифицирует клиента), публичный ключ сервера (клиент проверяет сервер и
// начинает Noise IK) и опционально список хешей досок. Публичный ключ клиента
// не хранится — выводится из приватного.
//
// Это граница учётных данных: её разбирают app/config и CLI. Пакет чистый,
// зависит только от crypto для проверки ключей.
package keylink

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"bproxy-core/internal/crypto"
)

// Scheme — схема URI keylink.
const Scheme = "bproxy"

const tokenSize = crypto.KeySize * 2

var (
	ErrScheme  = errors.New("keylink: неверная схема")
	ErrToken   = errors.New("keylink: неверный токен ключей")
	ErrKeySize = errors.New("keylink: неверная длина ключа")
)

// Credentials — разобранное содержимое keylink.
type Credentials struct {
	ClientPrivate []byte
	ServerPublic  []byte
	Boards        []string
	Label         string
}

// Build собирает keylink. clientPriv и serverPub — по 32 байта; boards и label
// опциональны.
func Build(clientPriv, serverPub []byte, boards []string, label string) (string, error) {
	if len(clientPriv) != crypto.KeySize || len(serverPub) != crypto.KeySize {
		return "", ErrKeySize
	}
	raw := make([]byte, 0, tokenSize)
	raw = append(raw, clientPriv...)
	raw = append(raw, serverPub...)

	var b strings.Builder
	b.WriteString(Scheme)
	b.WriteString("://")
	b.WriteString(base64.RawURLEncoding.EncodeToString(raw))
	if len(boards) > 0 {
		b.WriteByte('@')
		b.WriteString(strings.Join(boards, ","))
	}
	if label != "" {
		b.WriteByte('#')
		b.WriteString(label)
	}
	return b.String(), nil
}

// Parse разбирает keylink. Порядок отделения хвостов важен: сначала #label (он
// последний и может содержать любые символы, включая @), затем @boards.
func Parse(s string) (Credentials, error) {
	rest, ok := strings.CutPrefix(s, Scheme+"://")
	if !ok {
		return Credentials{}, ErrScheme
	}

	var c Credentials
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		c.Label = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '@'); i >= 0 {
		for _, h := range strings.Split(rest[i+1:], ",") {
			if h = strings.TrimSpace(h); h != "" {
				c.Boards = append(c.Boards, h)
			}
		}
		rest = rest[:i]
	}

	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return Credentials{}, fmt.Errorf("%w: %v", ErrToken, err)
	}
	if len(raw) != tokenSize {
		return Credentials{}, ErrToken
	}
	c.ClientPrivate = raw[:crypto.KeySize]
	c.ServerPublic = raw[crypto.KeySize:]
	return c, nil
}

// ClientKeypair восстанавливает пару ключей клиента из приватного ключа.
func (c Credentials) ClientKeypair() (crypto.Keypair, error) {
	return crypto.KeypairFromPrivate(c.ClientPrivate)
}
