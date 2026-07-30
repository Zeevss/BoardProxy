package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Client — HTTP-клиент управляющего API поверх unix-сокета (используется
// bproxy clients/boards).
type Client struct {
	http *http.Client
}

// NewClient создаёт клиента к сокету socketPath.
func NewClient(socketPath string) *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

func (c *Client) ListClients(ctx context.Context) ([]ClientInfo, error) {
	var out []ClientInfo
	return out, c.do(ctx, http.MethodGet, "/clients", nil, &out)
}

func (c *Client) AddClient(ctx context.Context, name string) (CreateClientResponse, error) {
	var out CreateClientResponse
	return out, c.do(ctx, http.MethodPost, "/clients", CreateClientRequest{Name: name}, &out)
}

func (c *Client) GetClient(ctx context.Context, id int64) (ClientInfo, error) {
	var out ClientInfo
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/clients/%d", id), nil, &out)
}

// UpdateClient переименовывает и/или меняет статус клиента (поля с nil не
// трогаются). Переход в disabled рвёт его живые сессии.
func (c *Client) UpdateClient(ctx context.Context, id int64, req UpdateClientRequest) (ClientInfo, error) {
	var out ClientInfo
	return out, c.do(ctx, http.MethodPatch, fmt.Sprintf("/clients/%d", id), req, &out)
}

func (c *Client) RemoveClient(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/clients/%d", id), nil, nil)
}

// GetClientConnections возвращает живые соединения клиента (пусто, если он
// сейчас не подключён).
func (c *Client) GetClientConnections(ctx context.Context, id int64) ([]ConnectionInfo, error) {
	var out []ConnectionInfo
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/clients/%d/connections", id), nil, &out)
}

func (c *Client) ListBoards(ctx context.Context) ([]BoardInfo, error) {
	var out []BoardInfo
	return out, c.do(ctx, http.MethodGet, "/boards", nil, &out)
}

func (c *Client) AddBoard(ctx context.Context, id, name string) (BoardInfo, error) {
	var out BoardInfo
	return out, c.do(ctx, http.MethodPost, "/boards", CreateBoardRequest{ID: id, Name: name}, &out)
}

func (c *Client) GetBoard(ctx context.Context, id string) (BoardInfo, error) {
	var out BoardInfo
	return out, c.do(ctx, http.MethodGet, "/boards/"+id, nil, &out)
}

// UpdateBoard переименовывает и/или меняет статус хаба (поля с nil не
// трогаются).
func (c *Client) UpdateBoard(ctx context.Context, id string, req UpdateBoardRequest) (BoardInfo, error) {
	var out BoardInfo
	return out, c.do(ctx, http.MethodPatch, "/boards/"+id, req, &out)
}

func (c *Client) RemoveBoard(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/boards/"+id, nil, nil)
}

// Restart просит сервер плавно перезапуститься.
func (c *Client) Restart(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/restart", nil, nil)
}

// do выполняет запрос к сокету. Хост в URL игнорируется (транспорт всегда идёт в
// сокет), поэтому используем условный "http://unix".
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mgmt: %s %s: %w (сервер запущен? проверьте --socket)", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mgmt: %s %s: %s", method, path, errorBody(resp.Body))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func errorBody(r io.Reader) string {
	var e struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r, 8<<10))
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		return e.Error
	}
	return string(raw)
}
