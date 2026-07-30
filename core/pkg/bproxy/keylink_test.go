package bproxy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestInspectKeylinkReturnsOnlyDisplayMetadata(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	info, err := InspectKeylink("bproxy://" + token + "@board-a,board-b#desktop")
	if err != nil {
		t.Fatal(err)
	}
	if info.Label != "desktop" {
		t.Fatalf("label = %q", info.Label)
	}
	if strings.Join(info.Boards, ",") != "board-a,board-b" {
		t.Fatalf("boards = %v", info.Boards)
	}
}

func TestInspectKeylinkRejectsMalformedLink(t *testing.T) {
	if _, err := InspectKeylink("https://example.com"); err == nil {
		t.Fatal("expected malformed link error")
	}
}
