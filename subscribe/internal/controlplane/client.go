package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

type StatusError struct {
	Status int
	Detail string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("control-plane HTTP %d: %s", e.Status, e.Detail)
}

func New(endpoint, token string, client *http.Client) *Client {
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), token: token, http: client}
}

func (c *Client) ResolveToken(ctx context.Context, token string) (protocol.Subscription, error) {
	return c.resolve(ctx, map[string]string{"token": token})
}

func (c *Client) ResolveRecoveryKey(ctx context.Context, publicKey string) (protocol.Subscription, error) {
	return c.resolve(ctx, map[string]string{"recoveryPublicKey": publicKey})
}

func (c *Client) resolve(ctx context.Context, input map[string]string) (protocol.Subscription, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return protocol.Subscription{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint+"/api/v1/subscriptions/resolve", bytes.NewReader(body),
	)
	if err != nil {
		return protocol.Subscription{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return protocol.Subscription{}, fmt.Errorf("resolve subscription: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return protocol.Subscription{}, err
	}
	if response.StatusCode != http.StatusOK {
		return protocol.Subscription{}, &StatusError{Status: response.StatusCode, Detail: strings.TrimSpace(string(raw))}
	}
	var result protocol.Subscription
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.Subscription{}, fmt.Errorf("decode subscription snapshot: %w", err)
	}
	if result.Version != 1 || result.ID == "" || result.Revision == "" {
		return protocol.Subscription{}, errors.New("control-plane returned an invalid subscription snapshot")
	}
	return result, nil
}
