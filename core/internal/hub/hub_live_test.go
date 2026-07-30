package hub

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/boardtest"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/mux"
)

type yandexDialer struct{ opts yandex.Options }

func (d yandexDialer) Join(ctx context.Context) (board.Session, error) {
	return yandex.Join(ctx, d.opts)
}

// traceLog буферизует диагностические строки в памяти без синхронного I/O в
// момент вызова (add — просто мьютекс + append), чтобы сама трассировка не
// сдвигала тайминг настолько, что перестаёт ловиться исследуемая гонка.
// Печатается разом в конце теста через dump.
type traceLog struct {
	mu    sync.Mutex
	start time.Time
	lines []string
}

func newTraceLog() *traceLog { return &traceLog{start: time.Now()} }

func (t *traceLog) add(format string, args ...any) {
	line := fmt.Sprintf("+%v "+format, append([]any{time.Since(t.start).Round(time.Millisecond)}, args...)...)
	t.mu.Lock()
	t.lines = append(t.lines, line)
	t.mu.Unlock()
}

func (t *traceLog) dump(w io.Writer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, l := range t.lines {
		fmt.Fprintln(w, l)
	}
}

// serveEchoTraced — временная трассирующая копия serveEcho для диагностики
// TestLiveHubRendezvous: пишет в tl, что реально видит серверная сторона.
func serveEchoTraced(srv *Server, tl *traceLog) {
	go func() {
		for {
			m, err := srv.Accept(context.Background())
			if err != nil {
				return
			}
			go func(m *mux.Session) {
				for {
					st, err := m.AcceptStream(context.Background())
					if err != nil {
						return
					}
					tl.add("server: accepted stream id=%d target=%s", st.ID(), st.Target())
					go func(st *mux.Stream) {
						data, err := io.ReadAll(st)
						tl.add("server: stream id=%d read %q err=%v", st.ID(), string(data), err)
						n, werr := st.Write(data)
						tl.add("server: stream id=%d wrote n=%d err=%v", st.ID(), n, werr)
						cerr := st.CloseWrite()
						tl.add("server: stream id=%d closewrite err=%v", st.ID(), cerr)
					}(st)
				}
			}(m)
		}
	}()
}

// TestLiveHubRendezvous runs the full rendezvous over a real board: a hub
// observer on the first slide hands out real pages, and two clients each get a
// distinct page and echo over it. Skipped unless BPROXY_LIVE=1.
func TestLiveHubRendezvous(t *testing.T) {
	hash := boardtest.LiveHash(t)
	opts := yandex.Options{APIBase: boardtest.APIBase, Hash: hash, GuestName: "bproxy-hub"}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tl := newTraceLog()
	defer tl.dump(os.Stderr)

	hubSess, err := yandex.Join(ctx, opts)
	if err != nil {
		t.Fatalf("hub join: %v", err)
	}
	hubSlide := hubSess.CurrentSlide()
	slides := hubSess.Slides()
	if len(slides) < 3 {
		t.Skipf("board has %d slides; need >= 3 for this test", len(slides))
	}
	pool := slides[1:] // reserve the current slide as the hub

	serverKP, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	users := newFakeUsers()

	srv, err := NewServer(ctx, ServerConfig{
		HubSession:   hubSess,
		HubSlide:     hubSlide,
		Pool:         pool,
		Dialer:       yandexDialer{opts},
		ServerStatic: serverKP,
		Users:        users,
		Codec:        codec.Base64Codec{},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer srv.Close()
	serveEchoTraced(srv, tl)

	const clients = 2
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cs, err := yandex.Join(ctx, opts)
			if err != nil {
				errs <- fmt.Errorf("client %d join: %w", i, err)
				return
			}
			tl.add("client %d: joined participant=%s", i, cs.Participant())
			clientKP, err := crypto.Generate()
			if err != nil {
				errs <- fmt.Errorf("client %d keygen: %w", i, err)
				return
			}
			users.provision(clientKP.Public())
			m, err := Dial(ctx, ClientConfig{
				Session:      cs,
				HubSlide:     hubSlide,
				Codec:        codec.Base64Codec{},
				ClientStatic: clientKP,
				ServerPublic: serverKP.Public(),
			})
			if err != nil {
				errs <- fmt.Errorf("client %d dial: %w", i, err)
				return
			}
			defer m.Close()
			st, err := m.OpenStream(fmt.Sprintf("target-%d:443", i))
			if err != nil {
				errs <- err
				return
			}
			tl.add("client %d: opened stream id=%d", i, st.ID())
			msg := fmt.Sprintf("hub client %d says hi", i)
			n, werr := io.WriteString(st, msg)
			tl.add("client %d: wrote n=%d err=%v", i, n, werr)
			cerr := st.CloseWrite()
			tl.add("client %d: closewrite err=%v", i, cerr)
			type rr struct {
				data []byte
				err  error
			}
			ch := make(chan rr, 1)
			go func() {
				data, err := io.ReadAll(st)
				ch <- rr{data, err}
			}()
			var got string
			select {
			case r := <-ch:
				got = string(r.data)
				tl.add("client %d: read %q err=%v", i, got, r.err)
			case <-time.After(60 * time.Second):
				tl.add("client %d: read timed out", i)
				errs <- fmt.Errorf("client %d: read timed out", i)
				return
			}
			if got != msg {
				errs <- fmt.Errorf("client %d: got %q want %q", i, got, msg)
			} else {
				t.Logf("client %d round-tripped over its own page", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
