// Package clientcore composes the client-only BoardProxy stack. It deliberately
// does not import the server application package, SQLite, or management API so
// mobile bindings can include only the code needed by a client.
package clientcore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/config"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
)

type yandexDialer struct {
	options yandex.Options
}

var joinBoard = yandex.Join

func (d yandexDialer) Join(ctx context.Context) (board.Session, error) {
	return yandex.Join(ctx, d.options)
}

// Dial joins the board, completes rendezvous, and returns an encrypted mux
// session. Closing the mux cascades through link to the board session.
func Dial(ctx context.Context, cfg config.Config, log *slog.Logger) (*mux.Session, error) {
	clientStatic, serverPublic, boards, err := credentials(cfg)
	if err != nil {
		return nil, err
	}
	var failures []error
	for index, boardHash := range boards {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempt := cfg
		attempt.Board.Hash = boardHash
		sess, err := dialBoard(ctx, attempt, log, clientStatic, serverPublic)
		if err == nil {
			if index > 0 {
				log.Info("client board failover succeeded", "board", boardHash, "attempt", index+1)
			}
			return sess, nil
		}
		failures = append(failures, fmt.Errorf("board %s: %w", boardHash, err))
		log.Warn("client board unavailable; trying next", "board", boardHash,
			"attempt", index+1, "remaining", len(boards)-index-1, "err", err)
	}
	return nil, fmt.Errorf("all keylink boards failed: %w", errors.Join(failures...))
}

func dialBoard(ctx context.Context, cfg config.Config, log *slog.Logger, clientStatic crypto.Keypair, serverPublic []byte) (*mux.Session, error) {
	boardOptions := yandex.Options{
		APIBase:   cfg.Board.APIBase,
		Hash:      cfg.Board.Hash,
		GuestName: cfg.Board.GuestName,
		Protector: cfg.Client.Protector,
		Log:       log.With("component", "board", "role", "client-lane"),
	}
	sess, err := joinBoard(ctx, boardOptions)
	if err != nil {
		return nil, fmt.Errorf("join board: %w", err)
	}

	hubSlide, err := resolveHubSlide(cfg, sess)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("resolve hub page: %w", err)
	}

	connection, err := hub.DialBundle(ctx, hub.ClientConfig{
		Session:           sess,
		Dialer:            yandexDialer{options: boardOptions},
		HubSlide:          hubSlide,
		Codec:             codec.Z85Codec{},
		Link:              link.Options{RecvWindow: cfg.Transport.Window, Log: log},
		ClientStatic:      clientStatic,
		ServerPublic:      serverPublic,
		MaxPayload:        cfg.Transport.MaxFramePayload,
		StreamWindow:      cfg.Transport.StreamWindow,
		MaxStreamWindow:   cfg.Transport.MaxStreamWindow,
		CoalesceTarget:    cfg.Transport.CoalesceTarget,
		StreamIdleTimeout: cfg.Transport.StreamIdleTimeout,
		TargetLanes:       1,
		MaxLanes:          cfg.Client.MaxLanes,
	})
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hub rendezvous: %w", err)
	}
	m := connection.Session

	log.Info("client connected", "board", cfg.Board.Hash, "hub", hubSlide,
		"bundle", connection.Bundle.ID.String(), "lane", connection.Bundle.LaneID, "epoch", connection.Bundle.Epoch,
		"lanes", len(connection.Lanes),
		"window", link.ResolveRecvWindow(cfg.Transport.Window), "max_frame_payload", cfg.Transport.MaxFramePayload,
		"stream_window", cfg.Transport.StreamWindow, "max_stream_window", cfg.Transport.MaxStreamWindow,
		"max_lanes", cfg.Client.MaxLanes, "coalesce_target", "adaptive", "coalesce_ceiling", cfg.Transport.CoalesceTarget,
		"stream_idle_timeout", cfg.Transport.StreamIdleTimeout)
	return m, nil
}

func credentials(cfg config.Config) (kp crypto.Keypair, serverPub []byte, boards []string, err error) {
	if cfg.Client.Keylink == "" {
		return crypto.Keypair{}, nil, nil, errors.New("client keylink not set (-keylink / BPROXY_KEYLINK)")
	}
	creds, err := keylink.Parse(cfg.Client.Keylink)
	if err != nil {
		return crypto.Keypair{}, nil, nil, fmt.Errorf("parse keylink: %w", err)
	}
	kp, err = creds.ClientKeypair()
	if err != nil {
		return crypto.Keypair{}, nil, nil, fmt.Errorf("client keypair: %w", err)
	}
	if cfg.Board.Hash != "" {
		boards = []string{cfg.Board.Hash}
	} else {
		boards = append([]string(nil), creds.Boards...)
	}
	if len(boards) == 0 {
		return crypto.Keypair{}, nil, nil, errors.New("no board hash (-board flag or keylink)")
	}
	return kp, creds.ServerPublic, boards, nil
}

func resolveHubSlide(cfg config.Config, sess *yandex.Session) (string, error) {
	if cfg.Server.HubPage != "" {
		return cfg.Server.HubPage, nil
	}
	slides := sess.Slides()
	if len(slides) == 0 {
		return "", errors.New("board has no slides")
	}
	sorted := append([]string(nil), slides...)
	sort.Strings(sorted)
	return sorted[0], nil
}
