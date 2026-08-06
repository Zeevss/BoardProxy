package clientcore

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/config"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/keylink"
)

func TestCredentialsPreserveKeylinkBoardOrder(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	link, err := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b", "board-c"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Client.Keylink = link
	_, _, boards, err := credentials(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 3 || boards[0] != "board-a" || boards[1] != "board-b" || boards[2] != "board-c" {
		t.Fatalf("boards = %v", boards)
	}
}

func TestDialTriesEveryKeylinkBoardInOrder(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	link, _ := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b"}, "")
	cfg := config.Default()
	cfg.Client.Keylink = link
	var attempts []string
	previous := joinBoard
	joinBoard = func(_ context.Context, options yandex.Options) (*yandex.Session, error) {
		attempts = append(attempts, options.Hash)
		return nil, errors.New("unavailable")
	}
	defer func() { joinBoard = previous }()

	_, err := Dial(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Fatal("Dial must fail when every board is unavailable")
	}
	if len(attempts) != 2 || attempts[0] != "board-a" || attempts[1] != "board-b" {
		t.Fatalf("attempts = %v", attempts)
	}
}

func TestCredentialsExplicitBoardOverridesFailoverList(t *testing.T) {
	client, _ := crypto.Generate()
	server, _ := crypto.Generate()
	link, _ := keylink.Build(client.Private(), server.Public(), []string{"board-a", "board-b"}, "")
	cfg := config.Default()
	cfg.Client.Keylink = link
	cfg.Board.Hash = "forced"
	_, _, boards, err := credentials(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0] != "forced" {
		t.Fatalf("boards = %v", boards)
	}
}
