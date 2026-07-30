package bond_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"bproxy-core/internal/board/memory"
	"bproxy-core/internal/bond"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
)

func TestBondConnCarriesMuxAcrossTwoRealLinks(t *testing.T) {
	board := memory.NewBoard()
	clientBond := bond.New(bond.Options{})
	serverBond := bond.New(bond.Options{})

	for laneID, page := range map[bond.LaneID]string{1: "page-1", 2: "page-2"} {
		clientSession := board.NewSession("client-" + page)
		serverSession := board.NewSession("server-" + page)
		if _, err := clientSession.Subscribe(context.Background(), page); err != nil {
			t.Fatal(err)
		}
		if _, err := serverSession.Subscribe(context.Background(), page); err != nil {
			t.Fatal(err)
		}
		if err := clientBond.AddLane(laneID, link.New(clientSession, codec.Base64Codec{}, link.Options{})); err != nil {
			t.Fatal(err)
		}
		if err := serverBond.AddLane(laneID, link.New(serverSession, codec.Base64Codec{}, link.Options{})); err != nil {
			t.Fatal(err)
		}
	}

	clientMux := mux.New(clientBond, mux.Options{Client: true, MaxPayload: 16})
	serverMux := mux.New(serverBond, mux.Options{MaxPayload: 16})
	t.Cleanup(func() {
		_ = clientMux.Close()
		_ = serverMux.Close()
	})

	go func() {
		for {
			stream, err := serverMux.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go func() {
				data, _ := io.ReadAll(stream)
				_, _ = stream.Write(data)
				_ = stream.CloseWrite()
			}()
		}
	}()

	stream, err := clientMux.OpenStream("two-lane.example:443")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("bonded payload ", 64)
	if _, err := io.WriteString(stream, want); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	result := make(chan struct {
		data string
		err  error
	}, 1)
	go func() {
		data, err := io.ReadAll(stream)
		result <- struct {
			data string
			err  error
		}{string(data), err}
	}()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.data != want {
			t.Fatalf("echo length=%d, want %d", len(got.data), len(want))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("two-lane mux round trip timed out")
	}

	if clientBond.LaneCount() != 2 || serverBond.LaneCount() != 2 {
		t.Fatalf("lane counts: client=%d server=%d", clientBond.LaneCount(), serverBond.LaneCount())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) &&
		(clientBond.ConfirmedBytes() == 0 || serverBond.ConfirmedBytes() == 0) {
		time.Sleep(10 * time.Millisecond)
	}
	if clientBond.ConfirmedBytes() == 0 || serverBond.ConfirmedBytes() == 0 {
		t.Fatalf("receipts were not propagated: client=%d server=%d",
			clientBond.ConfirmedBytes(), serverBond.ConfirmedBytes())
	}
}
