package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bproxy-core/pkg/bproxy"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	subscribesdk "github.com/Zeevss/BoardProxy/subscribe/sdk"
)

const subscriptionResolveTimeout = 45 * time.Second

type subscriptionFetcher interface {
	Fetch(context.Context, string) (protocol.Subscription, error)
}

type SubscriptionKeyInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	NodeID    string   `json:"nodeId"`
	State     string   `json:"state"`
	UsedBytes uint64   `json:"usedBytes"`
	Boards    []string `json:"boards"`
}

type LinkInfo struct {
	Kind           string                `json:"kind"`
	Label          string                `json:"label"`
	Boards         []string              `json:"boards"`
	SubscriptionID string                `json:"subscriptionId,omitempty"`
	Revision       string                `json:"revision,omitempty"`
	Keys           []SubscriptionKeyInfo `json:"keys"`
}

func newSubscriptionFetcher() subscriptionFetcher {
	return &subscribesdk.Client{
		HTTP:  &http.Client{Timeout: 12 * time.Second},
		Cache: subscribesdk.NewMemoryCache(),
	}
}

// ParseLink validates either a direct bproxy keylink or a subscription URL.
// Subscription URLs are resolved through the public channel with the SDK's
// authenticated Yandex recovery and last-known-good fallback.
func (a *App) ParseLink(raw string) (LinkInfo, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "bproxy://") {
		info, err := bproxy.InspectKeylink(raw)
		if err != nil {
			return LinkInfo{}, err
		}
		return LinkInfo{Kind: "keylink", Label: info.Label, Boards: info.Boards, Keys: []SubscriptionKeyInfo{}}, nil
	}

	snapshot, err := a.fetchSubscription(raw)
	if err != nil {
		return LinkInfo{}, fmt.Errorf("не удалось получить подписку: %w", err)
	}
	if _, err := selectSubscriptionKey(snapshot); err != nil {
		return LinkInfo{}, err
	}
	result := LinkInfo{
		Kind: "subscription", Label: snapshot.Name, SubscriptionID: snapshot.ID,
		Revision: snapshot.Revision, Keys: make([]SubscriptionKeyInfo, 0, len(snapshot.Keys)),
	}
	seenBoards := make(map[string]struct{})
	for _, key := range snapshot.Keys {
		boards := keyBoards(key)
		result.Keys = append(result.Keys, SubscriptionKeyInfo{
			ID: key.ID, Name: key.Name, NodeID: key.NodeID, State: key.State,
			UsedBytes: key.UsedBytes, Boards: boards,
		})
		for _, board := range boards {
			if _, exists := seenBoards[board]; exists {
				continue
			}
			seenBoards[board] = struct{}{}
			result.Boards = append(result.Boards, board)
		}
	}
	return result, nil
}

func (a *App) resolveConnectionLink(raw string) (string, protocol.Key, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "bproxy://") {
		if _, err := bproxy.InspectKeylink(raw); err != nil {
			return "", protocol.Key{}, err
		}
		return raw, protocol.Key{}, nil
	}
	snapshot, err := a.fetchSubscription(raw)
	if err != nil {
		return "", protocol.Key{}, fmt.Errorf("не удалось обновить подписку: %w", err)
	}
	key, err := selectSubscriptionKey(snapshot)
	if err != nil {
		return "", protocol.Key{}, err
	}
	return key.Keylink, key, nil
}

func (a *App) fetchSubscription(raw string) (protocol.Subscription, error) {
	base := a.runtimeContext()
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, subscriptionResolveTimeout)
	defer cancel()
	return a.subscriptions.Fetch(ctx, raw)
}

func selectSubscriptionKey(snapshot protocol.Subscription) (protocol.Key, error) {
	for _, key := range snapshot.EnabledKeys() {
		if _, err := bproxy.InspectKeylink(key.Keylink); err == nil {
			return key, nil
		}
	}
	return protocol.Key{}, fmt.Errorf("подписка %q не содержит доступных ключей", snapshot.Name)
}

func keyBoards(key protocol.Key) []string {
	if key.Keylink == "" {
		return []string{}
	}
	info, err := bproxy.InspectKeylink(key.Keylink)
	if err != nil {
		return []string{}
	}
	return info.Boards
}
