package sdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

func TestFetchHTTPReturnsMultipleKeys(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) *http.Response {
		if request.Header.Get("Accept") != protocol.MediaType {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		return response(http.StatusOK, `{
            "version":1,"id":"family","name":"Family","state":"enabled","revision":"r1",
            "issuedAt":"2026-08-15T12:00:00Z","usedBytes":30,
            "keys":[
              {"id":"phone","name":"Phone","nodeId":"node-1","userId":"alice","state":"enabled","usedBytes":10,"keylink":"bproxy://one"},
              {"id":"laptop","name":"Laptop","nodeId":"node-2","userId":"alice","state":"enabled","usedBytes":20,"keylink":"bproxy://two"}
            ]}`)
	})}}

	snapshot, err := client.Fetch(context.Background(), testSubscriptionURL(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Keys) != 2 || snapshot.Keys[0].ID != "phone" || snapshot.Keys[1].ID != "laptop" {
		t.Fatalf("unexpected keys: %+v", snapshot.Keys)
	}
}

func TestFetchDoesNotBypassTerminalHTTPStatusWithCache(t *testing.T) {
	rawURL := testSubscriptionURL(t)
	cache := NewMemoryCache()
	cache.Store(rawURL, protocol.Subscription{Version: 1, ID: "cached", State: "enabled", Revision: "old"})
	client := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
			return response(http.StatusGone, "revoked")
		})},
		Cache: cache,
	}

	if _, err := client.Fetch(context.Background(), rawURL); err == nil {
		t.Fatal("expected terminal HTTP 410 error")
	}
}

// Клиент не должен разбирать текст ошибки, чтобы понять, что показать человеку:
// у каждого отказа свой устойчивый код, и для каждого нужен свой совет.
func TestFetchClassifiesFailures(t *testing.T) {
	valid := testSubscriptionURL(t)

	cases := []struct {
		name   string
		url    string
		client *Client
		want   Reason
	}{
		{
			// Хвост `#bp1=…` теряют мессенджеры и копирование по двойному
			// щелчку. Сети тут нет вовсе: отказ приходит мгновенно.
			name: "ссылка без капсулы",
			url:  "https://subscribe.example.com/s/bps_token",
			want: ReasonLink,
		},
		{
			name: "сервис отказал",
			url:  valid,
			client: &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
				return response(http.StatusGone, "revoked")
			})}},
			want: ReasonRejected,
		},
		{
			// 502 — не отказ, а недоступность: клиент идёт в резервный канал,
			// а тот в тесте без сети тоже не отвечает.
			name: "оба канала молчат",
			url:  valid,
			client: &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
				return response(http.StatusBadGateway, "down")
			})}},
			want: ReasonUnreachable,
		},
		{
			name: "подписка без включённых ключей",
			url:  valid,
			client: &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
				return response(http.StatusOK, `{
                    "version":1,"id":"family","name":"Family","state":"enabled","revision":"r1",
                    "keys":[{"id":"phone","nodeId":"node-1","userId":"alice","state":"disabled","keylink":"bproxy://one"}]}`)
			})}},
			want: ReasonEmpty,
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			client := item.client
			if client == nil {
				client = &Client{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := client.Fetch(ctx, item.url)

			var failure *FetchError
			if !errors.As(err, &failure) {
				t.Fatalf("Fetch() = %v, ждали *FetchError", err)
			}
			if failure.Reason != item.want {
				t.Fatalf("Reason = %q, ждали %q (ошибка: %v)", failure.Reason, item.want, err)
			}
		})
	}
}

// Пустая подписка получена честно: имя и остаток трафика показать можно,
// подключиться — нельзя. Поэтому снимок отдаётся вместе с ошибкой.
func TestFetchReturnsTheSnapshotOfAnEmptySubscription(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
		return response(http.StatusOK, `{
            "version":1,"id":"family","name":"Family","state":"enabled","revision":"r1","keys":[]}`)
	})}}

	snapshot, err := client.Fetch(context.Background(), testSubscriptionURL(t))

	var failure *FetchError
	if !errors.As(err, &failure) || failure.Reason != ReasonEmpty {
		t.Fatalf("Fetch() = %v, ждали ReasonEmpty", err)
	}
	if snapshot.Name != "Family" {
		t.Fatalf("Name = %q, снимок обязан дойти вместе с ошибкой", snapshot.Name)
	}
}

func testSubscriptionURL(t *testing.T) string {
	t.Helper()
	value, err := protocol.BuildURL("https://subscribe.example.com", "bps_token", protocol.Capsule{
		Version: 1, YandexURL: "https://disk.yandex.ru/edit/example", RecoveryKeyID: "r1",
		ClientPrivateKey:     protocol.EncodeKey(bytes.Repeat([]byte{1}, 32)),
		RecoveryServerPublic: protocol.EncodeKey(bytes.Repeat([]byte{2}, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type roundTripFunc func(*http.Request) *http.Response

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request), nil
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Request:    &http.Request{},
	}
}
