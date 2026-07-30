package codec

import (
	"fmt"
	"strings"
)

// z85Marker identifies BPX2 (Z85) объекты — v2 кодек, ~25% оверхеда вместо
// 33% у base64 (5 символов на 4 байта вместо 4 на 3). Проверено живьём на
// реальной доске: весь алфавит ниже и произвольные payload'ы до 2МБ доходят
// без искажений (JSON-экранирование, нормализация Unicode не портят данные).
const z85Marker = "BPX2:"

// z85Alphabet — 85 печатных ASCII-символов спецификации Z85 (ZeroMQ RFC 32),
// намеренно без кавычек и обратного слэша — не требует JSON-экранирования.
const z85Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-:+=^!/*?&<>()[]{}@%$#"

var z85Decode [256]int8 // -1 = символ вне алфавита

func init() {
	for i := range z85Decode {
		z85Decode[i] = -1
	}
	for i, c := range []byte(z85Alphabet) {
		z85Decode[c] = int8(i)
	}
}

// Z85Codec — v2: marker + цифра паддинга + Z85(frame). Z85 кодирует 4 байта
// в 5 символов; если длина frame не кратна 4, хвост дополняется нулями, а
// число добавленных байт (0-3) хранится отдельной цифрой сразу после маркера.
type Z85Codec struct{}

var _ Codec = Z85Codec{}

func (Z85Codec) Encode(frame []byte) (string, error) {
	pad := (4 - len(frame)%4) % 4
	padded := frame
	if pad > 0 {
		padded = make([]byte, len(frame)+pad)
		copy(padded, frame)
	}

	out := make([]byte, 0, len(z85Marker)+1+len(padded)/4*5)
	out = append(out, z85Marker...)
	out = append(out, '0'+byte(pad))
	var chunk [5]byte
	for i := 0; i < len(padded); i += 4 {
		v := uint32(padded[i])<<24 | uint32(padded[i+1])<<16 | uint32(padded[i+2])<<8 | uint32(padded[i+3])
		for j := 4; j >= 0; j-- {
			chunk[j] = z85Alphabet[v%85]
			v /= 85
		}
		out = append(out, chunk[:]...)
	}
	return string(out), nil
}

func (Z85Codec) Decode(value string) ([]byte, error) {
	rest, ok := strings.CutPrefix(value, z85Marker)
	if !ok {
		return nil, ErrNotProtocol
	}
	if len(rest) == 0 {
		return nil, fmt.Errorf("codec: z85 payload missing padding digit")
	}
	padDigit := rest[0]
	if padDigit < '0' || padDigit > '3' {
		return nil, fmt.Errorf("codec: z85 invalid padding digit %q", padDigit)
	}
	pad := int(padDigit - '0')
	body := rest[1:]
	if len(body)%5 != 0 {
		return nil, fmt.Errorf("codec: corrupt z85 payload: length %d not a multiple of 5", len(body))
	}

	out := make([]byte, 0, len(body)/5*4)
	for i := 0; i < len(body); i += 5 {
		var v uint32
		for j := 0; j < 5; j++ {
			d := z85Decode[body[i+j]]
			if d < 0 {
				return nil, fmt.Errorf("codec: corrupt z85 payload: invalid character %q", body[i+j])
			}
			v = v*85 + uint32(d)
		}
		out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
	if pad > len(out) {
		return nil, fmt.Errorf("codec: corrupt z85 payload: padding %d exceeds decoded length %d", pad, len(out))
	}
	return out[:len(out)-pad], nil
}
