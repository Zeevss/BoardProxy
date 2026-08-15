package yandex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrReadOnly = errors.New("Yandex Sheets session has no edit permission")
	ErrConflict = errors.New("Yandex Sheets bundle conflict; synchronize and retry")
)

type Client struct {
	mu              sync.Mutex
	httpClient      *http.Client
	baseURL         string
	token           string
	spreadsheetURL  string
	config          sessionConfig
	currentUserName string
	pending         map[int][]Operation
	lastAppliedID   int

	State            State
	Timeline         int
	CurrentBundleID  int
	SnapshotIndex    int
	SnapshotBundleID int
}

type statusResponse struct {
	Status string `json:"status"`
	Users  []struct {
		ID         int    `json:"id"`
		WOPIUserID string `json:"wopiUserId"`
		Name       string `json:"name"`
		Anonymous  bool   `json:"anonymous"`
	} `json:"users"`
	Info struct {
		Timeline                int    `json:"timeline"`
		CurrentSnapshotIndex    int    `json:"currentSnapshotIndex"`
		CurrentBundleID         int    `json:"currentBundleId"`
		CurrentSnapshotBundleID int    `json:"currentSnapshotBundleId"`
		RequestedEditor         bool   `json:"requestedEditor"`
		ResourceName            string `json:"resourceName"`
	} `json:"info"`
}

func Open(ctx context.Context, shareURL string) (*Client, error) {
	result, err := bootstrap(ctx, shareURL)
	if err != nil {
		return nil, err
	}
	client := &Client{
		httpClient: result.HTTPClient,
		baseURL:    "https://volga.yandex.ru/session/main/" + url.PathEscape(result.RequestPath),
		token:      result.Token, spreadsheetURL: result.SpreadsheetURL, config: result.Config,
		pending: make(map[int][]Operation),
	}
	if err := client.Sync(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) ResourceName() string { return c.config.ResourceName }
func (c *Client) Permissions() int     { return c.config.Permissions }
func (c *Client) CanEdit() bool        { return c.config.Permissions&0x02 != 0 }

func (c *Client) Sync(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syncLocked(ctx)
}

func (c *Client) syncLocked(ctx context.Context) error {
	var status statusResponse
	if err := c.post(ctx, "poll/status/sync?includeUsers=true", nil, &status); err != nil {
		return fmt.Errorf("read session status: %w", err)
	}
	if status.Status != "ACTIVE" {
		return fmt.Errorf("Yandex Sheets session is %q", status.Status)
	}
	c.Timeline = status.Info.Timeline
	serverNextBundleID := status.Info.CurrentBundleID
	c.lastAppliedID = serverNextBundleID - 1
	if serverNextBundleID > c.CurrentBundleID {
		c.CurrentBundleID = serverNextBundleID
	}
	c.SnapshotIndex = status.Info.CurrentSnapshotIndex
	c.SnapshotBundleID = status.Info.CurrentSnapshotBundleID
	for _, user := range status.Users {
		if user.ID == c.config.UserID {
			c.currentUserName = user.Name
			break
		}
	}

	request := struct {
		SnapshotIndex int    `json:"snapshotIndex"`
		FileName      string `json:"fileName"`
	}{SnapshotIndex: c.SnapshotIndex, FileName: "main.json"}
	var snapshot json.RawMessage
	if err := c.post(ctx, "snapshot/download", request, &snapshot); err != nil {
		return fmt.Errorf("download snapshot: %w", err)
	}
	snapshot, err := c.hydrateCommentParts(ctx, snapshot)
	if err != nil {
		return err
	}
	state, err := parseSnapshot(snapshot)
	if err != nil {
		return err
	}
	c.State = state

	lastCommittedBundleID := serverNextBundleID - 1
	if lastCommittedBundleID >= c.SnapshotBundleID {
		if err := c.applyBundleTail(ctx, c.SnapshotBundleID, lastCommittedBundleID); err != nil {
			return err
		}
	}
	if err := c.overlayPending(serverNextBundleID); err != nil {
		return err
	}
	return nil
}

func (c *Client) overlayPending(serverNextBundleID int) error {
	ids := make([]int, 0, len(c.pending))
	for id := range c.pending {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if id < serverNextBundleID {
			delete(c.pending, id)
			continue
		}
		if err := c.State.apply(c.pending[id]); err != nil {
			return fmt.Errorf("apply locally accepted bundle %d: %w", id, err)
		}
	}
	return nil
}

func (c *Client) hydrateCommentParts(ctx context.Context, snapshot json.RawMessage) (json.RawMessage, error) {
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &parts); err != nil {
		return nil, fmt.Errorf("decode main.json index: %w", err)
	}
	wanted := []string{
		"xl/persons/person.xml",
		"xl/threadedComments/threadedComment1.xml",
		"xl/comments1.xml",
		"xl/drawings/vmlDrawing1.vml",
	}
	for _, partName := range wanted {
		raw, ok := parts[partName]
		if !ok {
			continue
		}
		var external struct {
			Type string `json:"t_"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &external) != nil || external.Type != "external" {
			continue
		}
		if external.Name == "" {
			return nil, fmt.Errorf("snapshot part %s has no external file name", partName)
		}
		request := struct {
			SnapshotIndex int    `json:"snapshotIndex"`
			FileName      string `json:"fileName"`
		}{SnapshotIndex: c.SnapshotIndex, FileName: external.Name}
		var content json.RawMessage
		if err := c.post(ctx, "snapshot/download", request, &content); err != nil {
			return nil, fmt.Errorf("download snapshot part %s: %w", partName, err)
		}
		parts[partName] = content
	}
	result, err := json.Marshal(parts)
	if err != nil {
		return nil, fmt.Errorf("assemble comments snapshot: %w", err)
	}
	return result, nil
}

func (c *Client) applyBundleTail(ctx context.Context, start, target int) error {
	next := start
	for next <= target {
		request := struct {
			StartingBundleID int `json:"startingBundleId"`
			Count            int `json:"count"`
		}{StartingBundleID: next, Count: 100}
		var response struct {
			Bundles []serverBundle `json:"bundles"`
		}
		if err := c.post(ctx, "bundles", request, &response); err != nil {
			return fmt.Errorf("download bundles from %d: %w", next, err)
		}
		if len(response.Bundles) == 0 {
			return fmt.Errorf("bundle history ended before bundle %d", target)
		}
		last := next
		for _, bundle := range response.Bundles {
			last = bundle.ID
			if bundle.ID < c.SnapshotBundleID || bundle.ID > target {
				continue
			}
			if err := c.State.apply(bundle.Bundle); err != nil {
				return fmt.Errorf("apply bundle %d: %w", bundle.ID, err)
			}
		}
		if last >= target {
			break
		}
		next = last + 1
	}
	return nil
}

func (c *Client) sendUpdate(ctx context.Context, operations []Operation) error {
	if !c.CanEdit() {
		return ErrReadOnly
	}
	request := updateRequest{
		BundleID: c.CurrentBundleID, Bundle: operations,
		BuildSnapshot: false, Timeline: c.Timeline,
	}
	var status int
	var body []byte
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		status, body, err = c.doPost(ctx, "update", request)
		if err != nil {
			return err
		}
		if status != http.StatusTooManyRequests {
			break
		}
		if err := waitForRetry(ctx, time.Duration(attempt+1)*time.Second); err != nil {
			return err
		}
	}
	if status == http.StatusConflict || status == http.StatusPreconditionFailed {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return ErrConflict
		}
		return fmt.Errorf("%w: %s", ErrConflict, detail)
	}
	if status != http.StatusNoContent {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return fmt.Errorf("update: unexpected HTTP %d", status)
		}
		return fmt.Errorf("update: unexpected HTTP %d: %s", status, detail)
	}
	if err := c.State.apply(operations); err != nil {
		return fmt.Errorf("apply accepted local update: %w", err)
	}
	c.pending[c.CurrentBundleID] = append([]Operation(nil), operations...)
	c.CurrentBundleID++
	return nil
}

func waitForRetry(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) post(ctx context.Context, endpoint string, input any, output any) error {
	status, body, err := c.doPost(ctx, endpoint, input)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("POST %s: HTTP %d: %s", endpoint, status, strings.TrimSpace(string(body)))
	}
	if output == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode POST %s response: %w", endpoint, err)
	}
	return nil
}

func (c *Client) doPost(ctx context.Context, endpoint string, input any) (int, []byte, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, nil, fmt.Errorf("encode POST %s: %w", endpoint, err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+endpoint, body)
	if err != nil {
		return 0, nil, fmt.Errorf("create POST %s: %w", endpoint, err)
	}
	setBrowserHeaders(request)
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Origin", "https://volga.yandex.ru")
	request.Header.Set("Referer", c.spreadsheetURL)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read POST %s response: %w", endpoint, err)
	}
	return response.StatusCode, responseBody, nil
}
