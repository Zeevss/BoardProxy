// Package yandex implements board.Session against Yandex Board (boards.yandex.ru).
//
// The join flow (see yandex-board-api/SPEC.md §6/§7) is:
//
//  1. REST request-guest-token   → sets the token_<hash> session cookie.
//  2. REST get-whiteboard-info    → participant, properties, socket server, csrf.
//  3. Socket.IO connect to the assigned socket server, forwarding the session
//     cookie on the WS handshake (SPEC §5.1 — without it the server silently
//     ignores meaningful actions).
//  4. dashboard/subscribe-slide-dashboard → page snapshot in the ack.
//  5. dashboard/modify-objects            → create/update objects.
package yandex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"bproxy-core/internal/netprotect"
)

// restClient performs the REST half of the join flow and owns the cookie jar
// that is later forwarded to the Socket.IO handshake.
type restClient struct {
	http *http.Client
	jar  *cookiejar.Jar
	base string // e.g. https://boards.yandex.ru/api
	hash string // board hash
}

func newRESTClient(base, hash string, protector netprotect.Protector) (*restClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	httpClient := netprotect.HTTPClient(protector)
	httpClient.Jar = jar
	return &restClient{
		http: httpClient,
		jar:  jar,
		base: base,
		hash: hash,
	}, nil
}

// call performs one POST /api action with a base64(JSON) content payload and
// decodes the JSON response into out (may be nil to ignore the body).
func (c *restClient) call(ctx context.Context, action string, payload any, out any) error {
	content, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", action, err)
	}
	form := url.Values{
		"action":  {action},
		"content": {base64.StdEncoding.EncodeToString(content)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "https://boards.yandex.ru")
	req.Header.Set("Referer", "https://boards.yandex.ru/whiteboard/?hash="+c.hash)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", action, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s read body: %w", action, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s response: HTTP %s (body: %.256s)", action, resp.Status, body)
	}
	if out != nil {
		if err := json.Unmarshal(bytes.TrimSpace(body), out); err != nil {
			return fmt.Errorf("%s decode response: %w (body: %.256s)", action, err, body)
		}
	}
	return nil
}

// cookieHeader renders the cookies applicable to rawURL as a single Cookie
// header value. The session token has Domain=.boards.yandex.ru, so it is
// returned for the socket<NN>.boards.yandex.ru handshake too (SPEC §5.1).
func (c *restClient) cookieHeader(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// cookiejar matches https scheme; the socket URL uses https/wss on 443.
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else if u.Scheme == "ws" {
		u.Scheme = "http"
	}
	parts := make([]string, 0, 4)
	for _, ck := range c.jar.Cookies(u) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; "), nil
}

// SocketServer is one assigned Socket.IO balancer endpoint.
type SocketServer struct {
	IP    string `json:"ip"`
	VPort int    `json:"vport"`
}

// URL returns the https base URL for the Socket.IO connection.
func (s SocketServer) URL() string {
	return fmt.Sprintf("https://%s:%d", s.IP, s.VPort)
}

// whiteboardInfo is the parsed get-whiteboard-info response. Participant and
// Properties are kept raw because subscribe-slide-dashboard must forward the
// whole objects verbatim (SPEC §5.3).
type whiteboardInfo struct {
	participant     json.RawMessage
	participantHash string
	properties      json.RawMessage
	currentSlide    string
	slides          []string // хэши всех слайдов доски (пул страниц), в порядке следования
	socketServers   []SocketServer
	csrf            string
}

// parseSlides декодирует presentation.items (base64 от JSON-массива слайдов) в
// список хэшей слайдов доски. Ошибки декодирования дают пустой список — вызывающий
// подстрахуется current_slide.
func parseSlides(itemsB64 string) []string {
	if itemsB64 == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(itemsB64)
	if err != nil {
		return nil
	}
	var items []struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil
	}
	slides := make([]string, 0, len(items))
	for _, it := range items {
		if it.Hash != "" {
			slides = append(slides, it.Hash)
		}
	}
	return slides
}

// requestGuestToken obtains the guest JWT cookie (stored in the jar).
func (c *restClient) requestGuestToken(ctx context.Context, name string) error {
	return c.call(ctx, "request-guest-token", map[string]string{
		"name": name,
		"hash": c.hash,
	}, nil)
}

// getWhiteboardInfo fetches and parses the bootstrap info for the board.
func (c *restClient) getWhiteboardInfo(ctx context.Context) (*whiteboardInfo, error) {
	var raw struct {
		Presentation struct {
			Properties json.RawMessage `json:"properties"`
			// items — base64(JSON-массив слайдов [{hash,url,width,height,name}])
			Items string `json:"items"`
		} `json:"presentation"`
		Participant   json.RawMessage `json:"participant"`
		SocketServers []SocketServer  `json:"socket_servers"`
		CSRF          string          `json:"csrf"`
	}
	if err := c.call(ctx, "get-whiteboard-info", map[string]string{"hash": c.hash}, &raw); err != nil {
		return nil, err
	}

	info := &whiteboardInfo{
		participant:   raw.Participant,
		properties:    raw.Presentation.Properties,
		slides:        parseSlides(raw.Presentation.Items),
		socketServers: raw.SocketServers,
		csrf:          raw.CSRF,
	}
	// Extract the fields we need by value without losing the raw blobs.
	var part struct {
		Hash string `json:"hash"`
	}
	if len(raw.Participant) > 0 {
		_ = json.Unmarshal(raw.Participant, &part)
	}
	info.participantHash = part.Hash

	var props struct {
		CurrentSlide string `json:"current_slide"`
	}
	if len(raw.Presentation.Properties) > 0 {
		_ = json.Unmarshal(raw.Presentation.Properties, &props)
	}
	info.currentSlide = props.CurrentSlide

	if info.participantHash == "" || len(info.socketServers) == 0 {
		return nil, fmt.Errorf("get-whiteboard-info incomplete: participant=%q socket_servers=%d",
			info.participantHash, len(info.socketServers))
	}
	return info, nil
}
