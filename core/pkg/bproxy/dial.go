package bproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/hub"
	"bproxy-core/internal/keylink"
	"bproxy-core/internal/link"
	"bproxy-core/internal/mux"
)

const (
	defaultAPIBase         = "https://boards.yandex.ru/api"
	defaultGuestName       = "bproxy"
	defaultClientMaxLanes  = 8
	defaultMaxFramePayload = 4 << 20
	defaultStreamWindow    = 1 << 20
	defaultMaxStreamWindow = 32 << 20
)

type clientBoardDialer struct{ options yandex.Options }

var joinClientBoard = yandex.Join

// permanentDialError marks local configuration defects. A supervised client
// may retry network failures forever, but retrying an invalid keylink or URL
// only hides operator mistakes and creates noisy hot loops.
type permanentDialError struct{ error }

func (d clientBoardDialer) Join(ctx context.Context) (board.Session, error) {
	return yandex.Join(ctx, d.options)
}

// dialClient composes the client-only transport. Keeping it beside Client
// avoids a second composition root and prevents server packages from leaking
// into the desktop/mobile dependency graph.
func dialClient(ctx context.Context, cfg Config, log *slog.Logger) (*mux.Session, error) {
	if err := validateClientDialConfig(cfg); err != nil {
		return nil, permanentDialError{err}
	}
	clientStatic, serverPublic, boards, err := clientCredentials(cfg)
	if err != nil {
		return nil, permanentDialError{err}
	}
	var failures []error
	for index, boardHash := range boards {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		sess, err := dialClientBoard(ctx, cfg, boardHash, log, clientStatic, serverPublic)
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

func validateClientDialConfig(cfg Config) error {
	if cfg.MaxLanes < 0 || cfg.MaxLanes > 32 {
		return errors.New("max lanes must be between 1 and 32, or 0 for default")
	}
	if cfg.APIBase == "" {
		return nil
	}
	endpoint, err := url.ParseRequestURI(cfg.APIBase)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("API base must be an absolute http(s) URL")
	}
	return nil
}

func dialClientBoard(ctx context.Context, cfg Config, boardHash string, log *slog.Logger, clientStatic crypto.Keypair, serverPublic []byte) (*mux.Session, error) {
	apiBase := cfg.APIBase
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	maxLanes := cfg.MaxLanes
	if maxLanes <= 0 {
		maxLanes = defaultClientMaxLanes
	}
	boardOptions := yandex.Options{
		APIBase: apiBase, Hash: boardHash, GuestName: defaultGuestName,
		Protector: cfg.Protector, Log: log.With("component", "board", "role", "client-lane"),
	}
	sess, err := joinClientBoard(ctx, boardOptions)
	if err != nil {
		return nil, fmt.Errorf("join board: %w", err)
	}

	hubSlide, err := resolveClientHubSlide(cfg.HubPage, sess)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("resolve hub page: %w", err)
	}
	connection, err := hub.DialBundle(ctx, hub.ClientConfig{
		Session: sess, Dialer: clientBoardDialer{options: boardOptions}, HubSlide: hubSlide,
		Codec: codec.Z85Codec{}, Link: link.Options{Log: log},
		ClientStatic: clientStatic, ServerPublic: serverPublic,
		MaxPayload: defaultMaxFramePayload, StreamWindow: defaultStreamWindow, MaxStreamWindow: defaultMaxStreamWindow,
		TargetLanes: 1, MaxLanes: maxLanes,
	})
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hub rendezvous: %w", err)
	}
	log.Info("client connected", "board", boardHash, "hub", hubSlide,
		"bundle", connection.Bundle.ID.String(), "lane", connection.Bundle.LaneID,
		"epoch", connection.Bundle.Epoch, "lanes", len(connection.Lanes), "max_lanes", maxLanes)
	return connection.Session, nil
}

func clientCredentials(cfg Config) (kp crypto.Keypair, serverPub []byte, boards []string, err error) {
	if cfg.Keylink == "" {
		return crypto.Keypair{}, nil, nil, errors.New("client keylink not set (-keylink / BPROXY_KEYLINK)")
	}
	creds, err := keylink.Parse(cfg.Keylink)
	if err != nil {
		return crypto.Keypair{}, nil, nil, fmt.Errorf("parse keylink: %w", err)
	}
	kp, err = creds.ClientKeypair()
	if err != nil {
		return crypto.Keypair{}, nil, nil, fmt.Errorf("client keypair: %w", err)
	}
	if cfg.Board != "" {
		boards = []string{cfg.Board}
	} else {
		boards = append([]string(nil), creds.Boards...)
	}
	if len(boards) == 0 {
		return crypto.Keypair{}, nil, nil, errors.New("no board hash (-board flag or keylink)")
	}
	return kp, creds.ServerPublic, boards, nil
}

func resolveClientHubSlide(explicit string, sess *yandex.Session) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	slides := sess.Slides()
	if len(slides) == 0 {
		return "", errors.New("board has no slides")
	}
	sorted := append([]string(nil), slides...)
	sort.Strings(sorted)
	return sorted[0], nil
}
