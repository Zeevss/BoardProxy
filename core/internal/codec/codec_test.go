package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		[]byte("hello"),
		{0x00, 0xff, 0x10, 0x7f, 0x80},
		bytes.Repeat([]byte{0xAB}, 4096),
		bytes.Repeat([]byte{0xAB}, 4097), // не кратно 4 — задевает паддинг Z85
	}
	for name, c := range map[string]Codec{"base64": Base64Codec{}, "z85": Z85Codec{}} {
		for _, frame := range cases {
			value, err := c.Encode(frame)
			if err != nil {
				t.Fatalf("[%s] Encode(%d bytes): %v", name, len(frame), err)
			}
			got, err := c.Decode(value)
			if err != nil {
				t.Fatalf("[%s] Decode: %v", name, err)
			}
			if !bytes.Equal(got, frame) {
				t.Fatalf("[%s] round trip mismatch: got %x want %x", name, got, frame)
			}
		}
	}
}

func TestDecodeNonProtocol(t *testing.T) {
	for name, c := range map[string]Codec{"base64": Base64Codec{}, "z85": Z85Codec{}} {
		for _, v := range []string{"", "just a note", "meow meow", "BPX", "bpx1:abc"} {
			if _, err := c.Decode(v); !errors.Is(err, ErrNotProtocol) {
				t.Fatalf("[%s] Decode(%q) err = %v, want ErrNotProtocol", name, v, err)
			}
		}
	}
}

func TestDecodeCorrupt(t *testing.T) {
	cases := []struct {
		codec Codec
		value string
	}{
		{Base64Codec{}, marker + "!!!not base64!!!"},
		{Z85Codec{}, z85Marker + "9" /* невалидная цифра паддинга */ + "aaaaa"},
		{Z85Codec{}, z85Marker + "0" + "aaa" /* не кратно 5 */},
		{Z85Codec{}, z85Marker + "0" + "\"\"\"\"\"" /* вне алфавита Z85 */},
	}
	for _, tc := range cases {
		_, err := tc.codec.Decode(tc.value)
		if err == nil || errors.Is(err, ErrNotProtocol) {
			t.Fatalf("Decode(%q) err = %v, want a corruption error", tc.value, err)
		}
	}
}

// TestCodecsDoNotCrossDecode проверяет, что маркеры не пересекаются: base64
// не пытается декодировать значения Z85 как свои (и наоборот) — оба кодека
// могут молча сосуществовать на одной странице во время миграции.
func TestCodecsDoNotCrossDecode(t *testing.T) {
	frame := []byte("cross-decode probe")
	z85Value, err := Z85Codec{}.Encode(frame)
	if err != nil {
		t.Fatalf("z85 encode: %v", err)
	}
	if _, err := (Base64Codec{}).Decode(z85Value); !errors.Is(err, ErrNotProtocol) {
		t.Fatalf("Base64Codec.Decode(z85 value) err = %v, want ErrNotProtocol", err)
	}

	b64Value, err := Base64Codec{}.Encode(frame)
	if err != nil {
		t.Fatalf("base64 encode: %v", err)
	}
	if _, err := (Z85Codec{}).Decode(b64Value); !errors.Is(err, ErrNotProtocol) {
		t.Fatalf("Z85Codec.Decode(base64 value) err = %v, want ErrNotProtocol", err)
	}
}

func TestIsProtocolValue(t *testing.T) {
	value, err := Base64Codec{}.Encode([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsProtocolValue(value) {
		t.Fatalf("encoded value lacks marker: %.16q", value)
	}
	if IsProtocolValue("not ours") {
		t.Fatal("IsProtocolValue should reject a value without the marker")
	}
}
