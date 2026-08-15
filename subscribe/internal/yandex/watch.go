package yandex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

var ErrBundleGap = errors.New("Yandex Sheets live bundle gap")

type Event struct {
	BundleID int
	Timeline int
	Comment  Comment
}

// Watch connects to the server-to-client Xiva stream and emits newly inserted
// threaded comments. It returns only after the websocket closes or ctx ends.
// Callers should reopen the whole Yandex session after any non-context error so
// snapshot and bundle history reconcile a possible gap.
func (c *Client) Watch(ctx context.Context, ready chan<- struct{}, events chan<- Event) error {
	endpoint, err := c.websocketURL()
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("Origin", "https://volga.yandex.ru")
	header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/151 Safari/537.36")
	websocketHTTPClient := *c.httpClient
	websocketHTTPClient.Timeout = 0
	connection, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: &websocketHTTPClient,
		HTTPHeader: header,
	})
	if err != nil {
		return fmt.Errorf("connect Yandex push websocket: %w", err)
	}
	defer connection.CloseNow()
	connection.SetReadLimit(4 << 20)
	readySent := false
	for {
		messageType, payload, err := connection.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read Yandex push websocket: %w", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		bundle, operation, ok, err := decodeLiveBundle(payload)
		if err != nil {
			return err
		}
		if !readySent && operation == "subscribed" {
			readySent = true
			if ready != nil {
				select {
				case ready <- struct{}{}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if !ok {
			continue
		}
		inserted, err := c.applyRemote(bundle)
		if err != nil {
			return err
		}
		for _, comment := range inserted {
			select {
			case events <- Event{BundleID: bundle.ID, Timeline: bundle.Timeline, Comment: comment}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (c *Client) websocketURL() (string, error) {
	xiva := c.config.Xiva
	if xiva.URL == "" || xiva.Service == "" || xiva.User == "" || xiva.Sign == "" || xiva.Timestamp == "" {
		return "", errors.New("Yandex session has incomplete Xiva configuration")
	}
	endpoint, err := url.Parse(xiva.URL)
	if err != nil {
		return "", fmt.Errorf("parse Yandex push URL: %w", err)
	}
	endpoint.Scheme = "wss"
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/subscribe/websocket"
	query := endpoint.Query()
	query.Set("service", xiva.Service)
	query.Set("user", xiva.User)
	query.Set("sign", xiva.Sign)
	query.Set("ts", xiva.Timestamp)
	query.Set("client", "web")
	query.Set("session", c.config.SessionID)
	if xiva.FetchHistory {
		query.Set("fetch_history", xiva.User+":"+xiva.Service+":0:1")
	}
	query.Set("x_request_attempt", "0")
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func decodeLiveBundle(payload []byte) (serverBundle, string, bool, error) {
	var outer struct {
		Operation string `json:"operation"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(payload, &outer); err != nil {
		return serverBundle{}, "", false, fmt.Errorf("decode Yandex push envelope: %w", err)
	}
	if outer.Operation != "SESSION" || outer.Message == "" {
		return serverBundle{}, outer.Operation, false, nil
	}
	var kind struct {
		Type string `json:"t"`
	}
	if err := json.Unmarshal([]byte(outer.Message), &kind); err != nil {
		return serverBundle{}, outer.Operation, false, fmt.Errorf("decode Yandex push message type: %w", err)
	}
	if kind.Type != "update" {
		return serverBundle{}, outer.Operation, false, nil
	}
	var bundle serverBundle
	if err := json.Unmarshal([]byte(outer.Message), &bundle); err != nil {
		return serverBundle{}, outer.Operation, false, fmt.Errorf("decode Yandex update bundle: %w", err)
	}
	return bundle, outer.Operation, true, nil
}

func (c *Client) applyRemote(bundle serverBundle) ([]Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if bundle.Timeline != c.Timeline {
		return nil, fmt.Errorf("%w: timeline changed from %d to %d", ErrBundleGap, c.Timeline, bundle.Timeline)
	}
	if bundle.ID <= c.lastAppliedID {
		delete(c.pending, bundle.ID)
		return nil, nil
	}
	if bundle.ID != c.lastAppliedID+1 {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrBundleGap, c.lastAppliedID+1, bundle.ID)
	}
	if _, own := c.pending[bundle.ID]; own {
		delete(c.pending, bundle.ID)
		c.lastAppliedID = bundle.ID
		return nil, nil
	}
	known := make(map[string]struct{}, len(c.State.Comments))
	for _, comment := range c.State.Comments {
		known[strings.ToUpper(comment.ID)] = struct{}{}
	}
	if err := c.State.apply(bundle.Bundle); err != nil {
		return nil, fmt.Errorf("apply live bundle %d: %w", bundle.ID, err)
	}
	c.lastAppliedID = bundle.ID
	if bundle.ID >= c.CurrentBundleID {
		c.CurrentBundleID = bundle.ID + 1
	}
	var inserted []Comment
	for _, comment := range c.State.Comments {
		if _, ok := known[strings.ToUpper(comment.ID)]; !ok {
			inserted = append(inserted, comment)
		}
	}
	return inserted, nil
}
