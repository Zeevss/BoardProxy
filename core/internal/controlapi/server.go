package controlapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	"bproxy-core/internal/control"
	"bproxy-core/internal/serverconfig"
	"bproxy-core/internal/telemetry"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Runtime interface {
	Revision() uint64
	Source() string
	ServerPublicKey() []byte
	Users() []control.UserView
	Boards() []control.BoardView
	ReplaceUser(expected uint64, user serverconfig.User) error
	SetUserEnabled(expected uint64, tag string, enabled bool) error
	RemoveUser(expected uint64, tag string) error
	ReplaceBoard(expected uint64, board serverconfig.Board) error
	SetBoardEnabled(expected uint64, tag string, enabled bool) error
	RemoveBoard(expected uint64, tag string) error
	ApplySnapshot(expected uint64, users []serverconfig.User, boards []serverconfig.Board) error
	Keylink(tag string) (string, error)
	Reload(expected uint64) error
	Stats() telemetry.Stats
}

type Server struct {
	UnimplementedControlServiceServer
	runtime Runtime
}

func NewServer(runtime Runtime) *Server { return &Server{runtime: runtime} }

func Serve(ctx context.Context, address string, runtime Runtime, log *slog.Logger) error {
	ln, cleanup, err := listen(address)
	if err != nil {
		return err
	}
	defer cleanup()
	srv := grpc.NewServer()
	RegisterControlServiceServer(srv, NewServer(runtime))
	reflection.Register(srv)
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()
	log.Info("gRPC control API listening", "address", address)
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func listen(address string) (net.Listener, func(), error) {
	if strings.HasPrefix(address, "unix://") {
		path := strings.TrimPrefix(address, "unix://")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, nil, err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			ln.Close()
			return nil, nil, err
		}
		return ln, func() { _ = ln.Close(); _ = os.Remove(path) }, nil
	}
	address = strings.TrimPrefix(address, "tcp://")
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	return ln, func() { _ = ln.Close() }, nil
}

func (s *Server) GetRuntime(context.Context, *emptypb.Empty) (*RuntimeInfo, error) {
	return &RuntimeInfo{
		Revision: s.runtime.Revision(), ConfigSource: s.runtime.Source(),
		ServerPublicKey: "base64:" + base64.StdEncoding.EncodeToString(s.runtime.ServerPublicKey()),
	}, nil
}

func (s *Server) Reload(_ context.Context, req *RevisionRequest) (*MutationResult, error) {
	if err := s.runtime.Reload(req.GetExpectedRevision()); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) ApplySnapshot(_ context.Context, req *ApplySnapshotRequest) (*MutationResult, error) {
	users := make([]serverconfig.User, 0, len(req.GetUsers()))
	for i, spec := range req.GetUsers() {
		if spec == nil {
			return nil, status.Errorf(codes.InvalidArgument, "users[%d] is required", i)
		}
		users = append(users, userConfig(spec))
	}
	boards := make([]serverconfig.Board, 0, len(req.GetBoards()))
	for i, spec := range req.GetBoards() {
		if spec == nil {
			return nil, status.Errorf(codes.InvalidArgument, "boards[%d] is required", i)
		}
		boards = append(boards, boardConfig(spec))
	}
	if err := s.runtime.ApplySnapshot(req.GetExpectedRevision(), users, boards); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) ListUsers(context.Context, *emptypb.Empty) (*ListUsersResponse, error) {
	views := s.runtime.Users()
	out := &ListUsersResponse{Revision: s.runtime.Revision(), Users: make([]*UserInfo, 0, len(views))}
	for _, user := range views {
		item := &UserInfo{
			Tag: user.ID, Name: user.Name, Enabled: user.Enabled,
			PublicKey: "base64:" + base64.StdEncoding.EncodeToString(user.PublicKey),
			Boards:    user.Boards, MaxSessions: int32(user.MaxSessions), MaxLanes: int32(user.MaxLanes),
			ActiveSessions: int32(user.ActiveSessions), RxBytesSinceStart: user.RXBytes, TxBytesSinceStart: user.TXBytes,
		}
		if !user.LastSeen.IsZero() {
			item.LastSeenSinceStart = timestamppb.New(user.LastSeen)
		}
		out.Users = append(out.Users, item)
	}
	return out, nil
}

func (s *Server) ReplaceUser(_ context.Context, req *ReplaceUserRequest) (*MutationResult, error) {
	if req.GetUser() == nil {
		return nil, status.Error(codes.InvalidArgument, "user is required")
	}
	if err := s.runtime.ReplaceUser(req.GetExpectedRevision(), userConfig(req.GetUser())); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) SetUserEnabled(_ context.Context, req *SetEnabledRequest) (*MutationResult, error) {
	if err := s.runtime.SetUserEnabled(req.GetExpectedRevision(), req.GetTag(), req.GetEnabled()); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) RemoveUser(_ context.Context, req *ResourceRequest) (*MutationResult, error) {
	if err := s.runtime.RemoveUser(req.GetExpectedRevision(), req.GetTag()); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) GetKeylink(_ context.Context, req *ResourceRequest) (*KeylinkResponse, error) {
	link, err := s.runtime.Keylink(req.GetTag())
	if err != nil {
		return nil, rpcError(err)
	}
	return &KeylinkResponse{Keylink: link}, nil
}

func (s *Server) ListBoards(context.Context, *emptypb.Empty) (*ListBoardsResponse, error) {
	views := s.runtime.Boards()
	out := &ListBoardsResponse{Revision: s.runtime.Revision(), Boards: make([]*BoardInfo, 0, len(views))}
	for _, board := range views {
		out.Boards = append(out.Boards, &BoardInfo{Config: boardSpec(board.Config), State: board.State, Error: board.Error})
	}
	return out, nil
}

func (s *Server) ReplaceBoard(_ context.Context, req *ReplaceBoardRequest) (*MutationResult, error) {
	if req.GetBoard() == nil {
		return nil, status.Error(codes.InvalidArgument, "board is required")
	}
	if err := s.runtime.ReplaceBoard(req.GetExpectedRevision(), boardConfig(req.GetBoard())); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) SetBoardEnabled(_ context.Context, req *SetEnabledRequest) (*MutationResult, error) {
	if err := s.runtime.SetBoardEnabled(req.GetExpectedRevision(), req.GetTag(), req.GetEnabled()); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) RemoveBoard(_ context.Context, req *ResourceRequest) (*MutationResult, error) {
	if err := s.runtime.RemoveBoard(req.GetExpectedRevision(), req.GetTag()); err != nil {
		return nil, rpcError(err)
	}
	return &MutationResult{Revision: s.runtime.Revision()}, nil
}

func (s *Server) GetStats(context.Context, *emptypb.Empty) (*RuntimeStats, error) {
	return statsMessage(s.runtime.Stats()), nil
}

func boardSpec(board serverconfig.Board) *BoardSpec {
	enabled := board.IsEnabled()
	return &BoardSpec{
		Tag: board.Tag, Name: board.Name, Hash: board.Hash, HubSlide: board.HubSlide,
		ApiBase: board.APIBase, GuestName: board.GuestName, Enabled: &enabled, MaxLanes: int32(board.MaxLanes),
	}
}

func userConfig(u *UserSpec) serverconfig.User {
	enabled := true
	if u.Enabled != nil {
		enabled = u.GetEnabled()
	}
	return serverconfig.User{
		Tag: u.GetTag(), Name: u.GetName(), PrivateKey: u.GetPrivateKey(), PublicKey: u.GetPublicKey(),
		Enabled: &enabled, Boards: append([]string(nil), u.GetBoards()...),
		MaxSessions: int(u.GetMaxSessions()), MaxLanes: int(u.GetMaxLanes()),
	}
}

func boardConfig(b *BoardSpec) serverconfig.Board {
	enabled := true
	if b.Enabled != nil {
		enabled = b.GetEnabled()
	}
	return serverconfig.Board{
		Tag: b.GetTag(), Name: b.GetName(), Hash: b.GetHash(), HubSlide: b.GetHubSlide(),
		APIBase: b.GetApiBase(), GuestName: b.GetGuestName(), Enabled: &enabled, MaxLanes: int(b.GetMaxLanes()),
	}
}

func statsMessage(s telemetry.Stats) *RuntimeStats {
	out := &RuntimeStats{
		StartedAt: timestamppb.New(s.StartedAt), Revision: s.Revision,
		UsersConfigured: int32(s.UsersConfigured), UsersEnabled: int32(s.UsersEnabled), UsersOnline: int32(s.UsersOnline),
		BoardsConfigured: int32(s.BoardsConfigured), BoardsEnabled: int32(s.BoardsEnabled), BoardsRunning: int32(s.BoardsRunning),
		ActiveConnections: int32(s.ActiveConnections), ActiveLanes: int32(s.ActiveLanes), ActiveStreams: int32(s.ActiveStreams),
		RxBytesSinceStart: s.RXBytesSinceStart, TxBytesSinceStart: s.TXBytesSinceStart,
		Network: &NetworkStats{
			Available: s.Network.Available, Scope: s.Network.Scope, Interfaces: s.Network.Interfaces,
			RxBytesSinceStart: s.Network.RXBytesSinceStart, TxBytesSinceStart: s.Network.TXBytesSinceStart,
			RxBytesPerSecond: s.Network.RXBytesPerSecond, TxBytesPerSecond: s.Network.TXBytesPerSecond,
		},
		Transport: &TransportStats{
			DisconnectsTotal: s.Transport.DisconnectsTotal, ReconnectsTotal: s.Transport.ReconnectsTotal,
			ReconnectAttemptsFailed: s.Transport.ReconnectAttemptsFailed, CircuitOpenTotal: s.Transport.CircuitOpenTotal,
			SnapshotObjectsTotal: s.Transport.SnapshotObjectsTotal, SnapshotBytesTotal: s.Transport.SnapshotBytesTotal,
			ReconnectsLastMinute: int32(s.Transport.ReconnectsLastMinute), SnapshotBytesLastMinute: s.Transport.SnapshotBytesLastMinute,
			LastDisconnectReason: s.Transport.LastDisconnectReason, LastDowntimeMs: s.Transport.LastDowntimeMillis,
		},
	}
	if !s.Network.SampledAt.IsZero() {
		out.Network.SampledAt = timestamppb.New(s.Network.SampledAt)
	}
	if s.Transport.LastDisconnectAt != nil {
		out.Transport.LastDisconnectAt = timestamppb.New(*s.Transport.LastDisconnectAt)
	}
	if s.Transport.LastReconnectAt != nil {
		out.Transport.LastReconnectAt = timestamppb.New(*s.Transport.LastReconnectAt)
	}
	for _, user := range s.Users {
		u := &UserRuntimeStats{
			Tag: user.Tag, Name: user.Name, Enabled: user.Enabled, Online: user.Online,
			Connections: int32(user.Connections), Lanes: int32(user.Lanes), Streams: int32(user.Streams),
			RxBytesSinceStart: user.RXBytes, TxBytesSinceStart: user.TXBytes,
			MaxSessions: int32(user.MaxSessions), MaxLanes: int32(user.MaxLanes),
		}
		if user.LastSeen != nil {
			u.LastSeenSinceStart = timestamppb.New(*user.LastSeen)
		}
		out.Users = append(out.Users, u)
	}
	for _, board := range s.Boards {
		out.Boards = append(out.Boards, &BoardRuntimeStats{
			Tag: board.Tag, Name: board.Name, Hash: board.Hash, Enabled: board.Enabled,
			State: board.State, Error: board.Error, Clients: int32(board.Clients), FreePages: int32(board.FreePages),
			RxBytes: board.RXBytes, TxBytes: board.TXBytes,
			PageCleanupRuns: board.PageCleanupRuns, PageCleanupDeleted: board.PageCleanupDeleted,
			PageCleanupFailures: board.PageCleanupFailures, PageCleanupQuarantined: board.PageCleanupQuarantined,
		})
	}
	return out
}

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "revision conflict") {
		return status.Error(codes.Aborted, err.Error())
	}
	if strings.Contains(err.Error(), "not found") {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Error(codes.InvalidArgument, fmt.Sprint(err))
}
