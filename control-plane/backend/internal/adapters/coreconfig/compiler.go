// Package coreconfig compiles normalized control-plane entities into the
// versioned TOML boundary consumed by BoardProxy core.
package coreconfig

import (
	"bytes"
	"fmt"

	"bproxy-control-plane/internal/domain"

	"github.com/BurntSushi/toml"
)

const coreConfigVersion = 1

type Compiler struct{}

func (Compiler) Compile(catalog domain.Catalog) ([]byte, error) {
	boards, assignedUsers, err := catalog.AssignedResources()
	if err != nil {
		return nil, fmt.Errorf("core config compiler: %w", err)
	}
	settings := catalog.Node.Core
	compiled := config{
		Version: coreConfigVersion,
		Server: server{
			PrivateKey: settings.Server.PrivateKey, IdleTimeout: settings.Server.IdleTimeout.String(),
			AllowPrivateEgress: settings.Server.AllowPrivateEgress,
		},
		Transport: transport{
			Window: settings.Transport.Window, MaxFramePayload: settings.Transport.MaxFramePayload,
			StreamWindow: settings.Transport.StreamWindow, MaxStreamWindow: settings.Transport.MaxStreamWindow,
			AckTimeout: settings.Transport.AckTimeout.String(), CoalesceTarget: settings.Transport.CoalesceTarget,
			StreamIdleTimeout: settings.Transport.StreamIdleTimeout.String(),
		},
		Management: management{
			GRPCListen: settings.Management.GRPCListen,
			HTTPListen: settings.Management.HTTPListen,
		},
		Observability: observability{
			Enabled: settings.Observability.Enabled, LogLevel: settings.Observability.LogLevel,
		},
	}
	compiled.Boards = make([]board, 0, len(boards))
	availableBoards := make(map[string]bool, len(boards))
	for _, item := range boards {
		if catalog.Node.State == domain.ResourceRevoked || item.State == domain.ResourceRevoked {
			continue
		}
		availableBoards[item.ID] = true
		compiled.Boards = append(compiled.Boards, board{
			Tag: item.ID, Name: item.Name, Hash: item.Hash, HubSlide: item.HubSlide,
			APIBase: item.APIBase, GuestName: item.GuestName,
			Enabled: catalog.Node.State.Enabled() && item.State.Enabled(), MaxLanes: item.MaxLanes,
		})
	}
	compiled.Users = make([]user, 0, len(assignedUsers))
	for _, item := range assignedUsers {
		if catalog.Node.State == domain.ResourceRevoked || item.User.State == domain.ResourceRevoked {
			continue
		}
		boards := make([]string, 0, len(item.Boards))
		for _, boardID := range item.Boards {
			if availableBoards[boardID] {
				boards = append(boards, boardID)
			}
		}
		if len(boards) == 0 {
			continue
		}
		compiled.Users = append(compiled.Users, user{
			Tag: item.User.ID, Name: item.User.Name,
			PrivateKey: item.User.PrivateKey, PublicKey: item.User.PublicKey,
			Enabled: catalog.Node.State.Enabled() && item.User.State.Enabled(), Boards: boards,
			MaxSessions: item.User.MaxSessions, MaxLanes: item.User.MaxLanes,
		})
	}
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(compiled); err != nil {
		return nil, fmt.Errorf("core config compiler: encode TOML: %w", err)
	}
	return output.Bytes(), nil
}

type config struct {
	Version       int           `toml:"version"`
	Server        server        `toml:"server"`
	Transport     transport     `toml:"transport"`
	Management    management    `toml:"management"`
	Observability observability `toml:"observability"`
	Boards        []board       `toml:"boards"`
	Users         []user        `toml:"users"`
}

type server struct {
	PrivateKey         string `toml:"private_key"`
	IdleTimeout        string `toml:"idle_timeout"`
	AllowPrivateEgress bool   `toml:"allow_private_egress"`
}

type transport struct {
	Window            int    `toml:"window"`
	MaxFramePayload   int    `toml:"max_frame_payload"`
	StreamWindow      int    `toml:"stream_window"`
	MaxStreamWindow   int    `toml:"max_stream_window"`
	AckTimeout        string `toml:"ack_timeout"`
	CoalesceTarget    int    `toml:"coalesce_target"`
	StreamIdleTimeout string `toml:"stream_idle_timeout"`
}

type management struct {
	GRPCListen string `toml:"grpc_listen"`
	HTTPListen string `toml:"http_listen,omitempty"`
}

type observability struct {
	Enabled  bool   `toml:"enabled"`
	LogLevel string `toml:"log_level"`
}

type board struct {
	Tag       string `toml:"tag"`
	Name      string `toml:"name"`
	Hash      string `toml:"hash"`
	HubSlide  string `toml:"hub_slide,omitempty"`
	APIBase   string `toml:"api_base,omitempty"`
	GuestName string `toml:"guest_name,omitempty"`
	Enabled   bool   `toml:"enabled"`
	MaxLanes  int    `toml:"max_lanes"`
}

type user struct {
	Tag         string   `toml:"tag"`
	Name        string   `toml:"name"`
	PrivateKey  string   `toml:"private_key,omitempty"`
	PublicKey   string   `toml:"public_key,omitempty"`
	Enabled     bool     `toml:"enabled"`
	Boards      []string `toml:"boards"`
	MaxSessions int      `toml:"max_sessions"`
	MaxLanes    int      `toml:"max_lanes"`
}
