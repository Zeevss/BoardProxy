package keylink

import (
	"bytes"
	"strings"
	"testing"

	"bproxy-core/internal/crypto"
)

func testKeys(t *testing.T) (clientPriv, serverPub []byte) {
	t.Helper()
	client, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate(client): %v", err)
	}
	server, err := crypto.Generate()
	if err != nil {
		t.Fatalf("Generate(server): %v", err)
	}
	return client.Private(), server.Public()
}

func TestBuildParseRoundTrip(t *testing.T) {
	clientPriv, serverPub := testKeys(t)
	boards := []string{"1272cae57eef80dda58036f3ac627c2b", "abcd1234"}

	link, err := Build(clientPriv, serverPub, boards, "мой ноутбук")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(link, Scheme+"://") {
		t.Fatalf("Build: нет префикса схемы: %q", link)
	}

	c, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(c.ClientPrivate, clientPriv) {
		t.Fatal("ClientPrivate не совпал")
	}
	if !bytes.Equal(c.ServerPublic, serverPub) {
		t.Fatal("ServerPublic не совпал")
	}
	if len(c.Boards) != 2 || c.Boards[0] != boards[0] || c.Boards[1] != boards[1] {
		t.Fatalf("Boards = %v, хочу %v", c.Boards, boards)
	}
	if c.Label != "мой ноутбук" {
		t.Fatalf("Label = %q, хочу «мой ноутбук»", c.Label)
	}

	// Пара клиента восстанавливается и её публичная часть согласована.
	kp, err := c.ClientKeypair()
	if err != nil {
		t.Fatalf("ClientKeypair: %v", err)
	}
	restored, _ := crypto.KeypairFromPrivate(clientPriv)
	if !bytes.Equal(kp.Public(), restored.Public()) {
		t.Fatal("ClientKeypair дал другой публичный ключ")
	}
}

func TestBuildParseMinimal(t *testing.T) {
	clientPriv, serverPub := testKeys(t)

	// Без досок и без метки.
	link, err := Build(clientPriv, serverPub, nil, "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.ContainsAny(link, "@#") {
		t.Fatalf("минимальный keylink не должен содержать @ или #: %q", link)
	}
	c, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Boards) != 0 || c.Label != "" {
		t.Fatalf("ожидались пустые Boards/Label, получил %+v", c)
	}
}

func TestParseLabelWithAtSign(t *testing.T) {
	clientPriv, serverPub := testKeys(t)
	// В метке есть '@' — она отделяется по '#' раньше, чем ищется '@' досок,
	// поэтому '@' в метке не должен ломать разбор.
	link, err := Build(clientPriv, serverPub, []string{"board1"}, "user@host")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Label != "user@host" {
		t.Fatalf("Label = %q, хочу user@host", c.Label)
	}
	if len(c.Boards) != 1 || c.Boards[0] != "board1" {
		t.Fatalf("Boards = %v, хочу [board1]", c.Boards)
	}
}

func TestBuildRejectsWrongKeySize(t *testing.T) {
	_, serverPub := testKeys(t)
	if _, err := Build([]byte("короткий"), serverPub, nil, ""); err != ErrKeySize {
		t.Fatalf("Build(короткий priv) = %v, хочу ErrKeySize", err)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	clientPriv, serverPub := testKeys(t)
	valid, _ := Build(clientPriv, serverPub, nil, "")

	cases := map[string]string{
		"чужая схема":    "icmptun://" + strings.TrimPrefix(valid, Scheme+"://"),
		"не base64":      Scheme + "://не-валидный-base64!!!",
		"короткий токен": Scheme + "://" + "AAAA",
		"пустой":         "",
		"только схема":   Scheme + "://",
	}
	for name, in := range cases {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%s) = nil, хочу ошибку", name)
		}
	}
}
