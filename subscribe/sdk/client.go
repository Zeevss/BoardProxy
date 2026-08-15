package sdk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/internal/yandex"
	"github.com/Zeevss/BoardProxy/subscribe/protocol"
	"github.com/Zeevss/BoardProxy/subscribe/recovery"
)

const Version = "0.1.0"

type Cache interface {
	Load(subscriptionURL string) (protocol.Subscription, bool)
	Store(subscriptionURL string, value protocol.Subscription)
}

type Client struct {
	HTTP  *http.Client
	Cache Cache
}

type StatusError struct {
	Status int
	Detail string
}

type RecoveryError struct {
	Code     string
	Message  string
	terminal bool
}

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("recovery failed (%s): %s", e.Code, e.Message)
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("subscription endpoint returned HTTP %d: %s", e.Status, e.Detail)
}

func (c *Client) Fetch(ctx context.Context, subscriptionURL string) (protocol.Subscription, error) {
	requestURL, _, capsule, err := protocol.ParseURL(subscriptionURL)
	if err != nil {
		return protocol.Subscription{}, err
	}

	primary, primaryErr := c.fetchHTTP(ctx, requestURL.String())
	if primaryErr == nil {
		c.store(subscriptionURL, primary)
		return primary, nil
	}
	var statusErr *StatusError
	if errors.As(primaryErr, &statusErr) && statusErr.Status >= 400 && statusErr.Status < 500 {
		return protocol.Subscription{}, primaryErr
	}

	fallback, fallbackErr := c.fetchYandex(ctx, capsule)
	if fallbackErr == nil {
		c.store(subscriptionURL, fallback)
		return fallback, nil
	}
	var recoveryErr *RecoveryError
	if errors.As(fallbackErr, &recoveryErr) && recoveryErr.terminal {
		return protocol.Subscription{}, recoveryErr
	}
	if c.Cache != nil {
		if cached, ok := c.Cache.Load(subscriptionURL); ok {
			return cached, nil
		}
	}
	return protocol.Subscription{}, errors.Join(primaryErr, fallbackErr)
}

func (c *Client) fetchHTTP(ctx context.Context, endpoint string) (protocol.Subscription, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return protocol.Subscription{}, err
	}
	request.Header.Set("Accept", protocol.MediaType)
	request.Header.Set("User-Agent", "BoardProxy-Subscribe-SDK/"+Version)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return protocol.Subscription{}, fmt.Errorf("fetch subscription over HTTPS: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return protocol.Subscription{}, err
	}
	if response.StatusCode != http.StatusOK {
		return protocol.Subscription{}, &StatusError{Status: response.StatusCode, Detail: string(bytes.TrimSpace(raw))}
	}
	var result protocol.Subscription
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.Subscription{}, fmt.Errorf("decode HTTPS subscription: %w", err)
	}
	if err := validate(result); err != nil {
		return protocol.Subscription{}, err
	}
	return result, nil
}

func (c *Client) fetchYandex(ctx context.Context, capsule protocol.Capsule) (protocol.Subscription, error) {
	clientPrivate, err := protocol.DecodeKey(capsule.ClientPrivateKey)
	if err != nil {
		return protocol.Subscription{}, err
	}
	serverPublic, err := protocol.DecodeKey(capsule.RecoveryServerPublic)
	if err != nil {
		return protocol.Subscription{}, err
	}
	requestID, err := newRequestID()
	if err != nil {
		return protocol.Subscription{}, err
	}
	hello := protocol.ClientHello{
		Version: 1, RequestID: requestID, CreatedAt: time.Now().UTC(),
		RequestedFormat: "boardproxy-keylink", SDKVersion: Version,
	}
	helloPayload, err := json.Marshal(hello)
	if err != nil {
		return protocol.Subscription{}, err
	}
	initiation, message, err := recovery.Initiate(clientPrivate, serverPublic, helloPayload)
	if err != nil {
		return protocol.Subscription{}, err
	}
	frameText, err := protocol.EncodeFrame(protocol.Frame{
		Type: "hello", RequestID: requestID, KeyID: capsule.RecoveryKeyID,
		Part: 1, Parts: 1, Payload: message,
	})
	if err != nil {
		return protocol.Subscription{}, err
	}

	sheet, err := yandex.Open(ctx, capsule.YandexURL)
	if err != nil {
		return protocol.Subscription{}, fmt.Errorf("open Yandex recovery channel: %w", err)
	}
	events := make(chan yandex.Event, 64)
	ready := make(chan struct{}, 1)
	watchResult := make(chan error, 1)
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	go func() { watchResult <- sheet.Watch(watchCtx, ready, events) }()
	select {
	case <-ready:
	case err := <-watchResult:
		return protocol.Subscription{}, err
	case <-ctx.Done():
		return protocol.Subscription{}, ctx.Err()
	}

	var root yandex.Comment
	for attempt := 0; attempt < 16; attempt++ {
		thread, createErr := sheet.CreateThread(ctx, protocol.MailboxCell(requestID, attempt), frameText)
		if createErr == nil {
			root = thread.Root
			break
		}
		if !errors.Is(createErr, yandex.ErrThreadExists) && !errors.Is(createErr, yandex.ErrConflict) {
			return protocol.Subscription{}, createErr
		}
	}
	if root.ID == "" {
		return protocol.Subscription{}, errors.New("cannot allocate a Yandex recovery mailbox")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = sheet.DeleteComment(cleanupCtx, root.ID)
	}()

	parts := make(map[int][]byte)
	expected := 0
	for {
		select {
		case event := <-events:
			frame, decodeErr := protocol.DecodeFrame(event.Comment.Text)
			if decodeErr != nil || frame.Type != "response" || frame.RequestID != requestID || frame.KeyID != capsule.RecoveryKeyID {
				continue
			}
			if expected != 0 && expected != frame.Parts {
				return protocol.Subscription{}, errors.New("Yandex recovery response has inconsistent part counts")
			}
			expected = frame.Parts
			parts[frame.Part] = append([]byte(nil), frame.Payload...)
			if len(parts) != expected {
				continue
			}
			var encrypted []byte
			for part := 1; part <= expected; part++ {
				chunk, ok := parts[part]
				if !ok {
					return protocol.Subscription{}, errors.New("Yandex recovery response is missing a part")
				}
				encrypted = append(encrypted, chunk...)
			}
			payload, completeErr := initiation.Complete(encrypted)
			if completeErr != nil {
				return protocol.Subscription{}, completeErr
			}
			var answer protocol.ServerHello
			if err := json.Unmarshal(payload, &answer); err != nil {
				return protocol.Subscription{}, fmt.Errorf("decode Yandex recovery response: %w", err)
			}
			if answer.Version != 1 || answer.RequestID != requestID {
				return protocol.Subscription{}, errors.New("Yandex recovery response metadata mismatch")
			}
			if answer.Error != nil {
				return protocol.Subscription{}, &RecoveryError{
					Code: answer.Error.Code, Message: answer.Error.Message,
					terminal: answer.Error.Code == "not_found" || answer.Error.Code == "disabled" || answer.Error.Code == "revoked",
				}
			}
			if err := validate(answer.Subscription); err != nil {
				return protocol.Subscription{}, err
			}
			return answer.Subscription, nil
		case err := <-watchResult:
			return protocol.Subscription{}, err
		case <-ctx.Done():
			return protocol.Subscription{}, ctx.Err()
		}
	}
}

func validate(value protocol.Subscription) error {
	if value.Version != 1 || value.ID == "" || value.Revision == "" || value.State != "enabled" {
		return errors.New("invalid or inactive subscription snapshot")
	}
	return nil
}

func (c *Client) store(subscriptionURL string, value protocol.Subscription) {
	if c.Cache != nil {
		c.Cache.Store(subscriptionURL, value)
	}
}

func newRequestID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
