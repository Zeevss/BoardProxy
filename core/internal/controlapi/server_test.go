package controlapi

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	controlv1 "bproxy-core/api/control/v1"
	"bproxy-core/internal/control"
	"bproxy-core/internal/serverconfig"
	"bproxy-core/internal/telemetry"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeRuntime struct {
	revision uint64
	users    []control.UserView
	replaced serverconfig.User
	err      error
}

func (f *fakeRuntime) Revision() uint64                              { return f.revision }
func (f *fakeRuntime) Source() string                                { return "config.toml" }
func (f *fakeRuntime) ServerPublicKey() []byte                       { return make([]byte, 32) }
func (f *fakeRuntime) Users() []control.UserView                     { return f.users }
func (f *fakeRuntime) Boards() []control.BoardView                   { return nil }
func (f *fakeRuntime) Stats() telemetry.Stats                        { return telemetry.Stats{Revision: f.revision} }
func (f *fakeRuntime) Keylink(string) (string, error)                { return "bproxy://test", f.err }
func (f *fakeRuntime) Reload(uint64) error                           { return f.err }
func (f *fakeRuntime) RemoveUser(uint64, string) error               { return f.err }
func (f *fakeRuntime) ReplaceBoard(uint64, serverconfig.Board) error { return f.err }
func (f *fakeRuntime) SetBoardEnabled(uint64, string, bool) error    { return f.err }
func (f *fakeRuntime) RemoveBoard(uint64, string) error              { return f.err }
func (f *fakeRuntime) SetUserEnabled(uint64, string, bool) error     { return f.err }
func (f *fakeRuntime) ApplySnapshot(uint64, []serverconfig.User, []serverconfig.Board) error {
	return f.err
}
func (f *fakeRuntime) ReplaceUser(_ uint64, user serverconfig.User) error {
	if f.err != nil {
		return f.err
	}
	f.replaced = user
	f.revision++
	return nil
}

func TestPublicGRPCClientCanMutateRuntime(t *testing.T) {
	runtime := &fakeRuntime{revision: 4}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	controlv1.RegisterControlServiceServer(server, NewServer(runtime))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := controlv1.NewControlServiceClient(conn)

	result, err := client.ReplaceUser(ctx, &controlv1.ReplaceUserRequest{
		ExpectedRevision: 4,
		User: &controlv1.UserSpec{Tag: "alice", Name: "Alice", PrivateKey: "base64:key",
			Boards: []string{"main"}, MaxSessions: 2, MaxLanes: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 5 || runtime.replaced.Tag != "alice" || !runtime.replaced.IsEnabled() {
		t.Fatalf("mutation was not mapped: result=%+v user=%+v", result, runtime.replaced)
	}
	stats, err := client.GetStats(ctx, &emptypb.Empty{})
	if err != nil || stats.Revision != 5 {
		t.Fatalf("stats round trip: stats=%+v err=%v", stats, err)
	}
}

func TestExplicitDisabledValueIsPreserved(t *testing.T) {
	disabled := false
	user := userConfig(&UserSpec{Tag: "alice", Enabled: &disabled})
	board := boardConfig(&BoardSpec{Tag: "main", Enabled: &disabled})
	if user.IsEnabled() || board.IsEnabled() {
		t.Fatalf("explicit false was lost: user=%+v board=%+v", user, board)
	}
}

func TestRevisionConflictMapsToAborted(t *testing.T) {
	runtime := &fakeRuntime{revision: 2, err: errors.New("app: revision conflict")}
	_, err := NewServer(runtime).Reload(context.Background(), &RevisionRequest{ExpectedRevision: 1})
	if status.Code(err) != codes.Aborted {
		t.Fatalf("code = %v, want %v (err=%v)", status.Code(err), codes.Aborted, err)
	}
}
