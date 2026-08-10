package bproxy

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/keylink"
)

func TestClientCredentialsPreserveKeylinkBoardOrder(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	raw, err := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b", "board-c"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, _, boards, err := clientCredentials(Config{Keylink: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 3 || boards[0] != "board-a" || boards[1] != "board-b" || boards[2] != "board-c" {
		t.Fatalf("boards = %v", boards)
	}
}

func TestDialClientTriesEveryKeylinkBoardInOrder(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	raw, _ := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b"}, "")
	var attempts []string
	previous := joinClientBoard
	joinClientBoard = func(_ context.Context, options yandex.Options) (*yandex.Session, error) {
		attempts = append(attempts, options.Hash)
		return nil, errors.New("unavailable")
	}
	defer func() { joinClientBoard = previous }()

	_, err := dialClient(context.Background(), Config{Keylink: raw}, slog.Default())
	if err == nil {
		t.Fatal("dialClient must fail when every board is unavailable")
	}
	if len(attempts) != 2 || attempts[0] != "board-a" || attempts[1] != "board-b" {
		t.Fatalf("attempts = %v", attempts)
	}
}

func TestClientCredentialsExplicitBoardOverridesFailoverList(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	raw, _ := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b"}, "")
	_, _, boards, err := clientCredentials(Config{Keylink: raw, Board: "forced"})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0] != "forced" {
		t.Fatalf("boards = %v", boards)
	}
}
