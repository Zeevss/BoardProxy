package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestLogsEndpoint проверяет, что GET /logs отдаёт записи из Config.Logs и
// прокидывает ?limit в функцию.
func TestLogsEndpoint(t *testing.T) {
	var gotLimit int
	h := Handler(Config{
		Store: newFakeStore(),
		Logs: func(limit int) []LogEntry {
			gotLimit = limit
			return []LogEntry{{Time: time.Unix(1, 0), Level: "INFO", Message: "hello world"}}
		},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/logs?limit=50")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var entries []LogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Message != "hello world" || entries[0].Level != "INFO" {
		t.Fatalf("entries = %+v", entries)
	}
	if gotLimit != 50 {
		t.Fatalf("limit = %d, хочу 50", gotLimit)
	}
}

// TestLogsEndpointNilConfig — без Config.Logs эндпойнт отдаёт пустой список, а не 500.
func TestLogsEndpointNilConfig(t *testing.T) {
	srv := httptest.NewServer(Handler(Config{Store: newFakeStore()}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

// TestStatsEndpoint проверяет, что GET /stats возвращает данные из Config.Stats.
func TestStatsEndpoint(t *testing.T) {
	h := Handler(Config{
		Store: newFakeStore(),
		Stats: func() ServerStats {
			return ServerStats{
				Clients: 3, ClientsOnline: 1, Boards: 2, RxBytes: 10, TxBytes: 20, HubsUp: 2,
				ServingBoards: []string{"a", "b"},
				PerBoard: []BoardStat{
					{ID: "a", Name: "A", ClientsOnline: 1, RxBytes: 10},
					{ID: "b", Name: "B"},
				},
			}
		},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st ServerStats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Clients != 3 || st.ClientsOnline != 1 || st.Boards != 2 || st.RxBytes != 10 || st.TxBytes != 20 || st.HubsUp != 2 {
		t.Fatalf("stats = %+v", st)
	}
	if len(st.PerBoard) != 2 || st.PerBoard[0].ID != "a" || st.PerBoard[0].ClientsOnline != 1 {
		t.Fatalf("per_board = %+v", st.PerBoard)
	}
}

// TestBackupExport проверяет, что GET /backup стримит содержимое из Config.Backup
// и ставит заголовок вложения.
func TestBackupExport(t *testing.T) {
	payload := append([]byte("SQLite format 3\x00"), []byte("...rest of db...")...)
	h := Handler(Config{
		Store: newFakeStore(),
		Backup: func(context.Context) (io.ReadCloser, int64, error) {
			return io.NopCloser(bytes.NewReader(payload)), int64(len(payload)), nil
		},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".db") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, payload) {
		t.Fatalf("body mismatch: %q", body)
	}
}

// TestBackupExportNotImplemented — без Config.Backup эндпойнт отдаёт 501.
func TestBackupExportNotImplemented(t *testing.T) {
	srv := httptest.NewServer(Handler(Config{Store: newFakeStore()}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, хочу 501", resp.StatusCode)
	}
}

// postBackup отправляет multipart с полем backup и заданным содержимым.
func postBackup(t *testing.T, url string, content []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("backup", "dump.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestBackupImport проверяет валидацию магии SQLite и вызов Config.Restore.
func TestBackupImport(t *testing.T) {
	var restored []byte
	h := Handler(Config{
		Store: newFakeStore(),
		Restore: func(_ context.Context, r io.Reader) error {
			b, _ := io.ReadAll(r)
			restored = b
			return nil
		},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Мусор без магии — 400, Restore не вызван.
	resp := postBackup(t, srv.URL+"/backup", []byte("not a database at all"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage: status = %d, хочу 400", resp.StatusCode)
	}
	if restored != nil {
		t.Fatalf("Restore не должен вызываться на мусоре")
	}

	// Валидный SQLite-дамп — 202, Restore получает полный поток (с магией).
	valid := append([]byte("SQLite format 3\x00"), []byte("payload-bytes")...)
	resp = postBackup(t, srv.URL+"/backup", valid)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("valid: status = %d, хочу 202", resp.StatusCode)
	}
	if !bytes.Equal(restored, valid) {
		t.Fatalf("Restore получил %q, хочу %q", restored, valid)
	}
}

// TestWebAuthPasswordFlow проверяет вход по паролю: без cookie — 401, после
// /login с верным паролем — доступ по cookie, неверный пароль — 401.
func TestWebAuthPasswordFlow(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(WebAuth(WebAuthConfig{UIPassword: "s3cret"}, inner))
	defer srv.Close()

	jar := &cookieJar{}
	client := &http.Client{Jar: jar}

	// Без сессии — 401.
	resp, _ := client.Get(srv.URL + "/clients")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-session: status = %d, хочу 401", resp.StatusCode)
	}

	// Неверный пароль — 401, cookie не выдаётся.
	resp = login(t, client, srv.URL, "wrong")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-password: status = %d, хочу 401", resp.StatusCode)
	}

	// Верный пароль — 204 + cookie, дальше доступ открыт.
	resp = login(t, client, srv.URL, "s3cret")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: status = %d, хочу 204", resp.StatusCode)
	}
	resp, _ = client.Get(srv.URL + "/clients")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with-session: status = %d, хочу 200", resp.StatusCode)
	}

	// Logout сбрасывает сессию.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/logout", nil)
	resp, _ = client.Do(req)
	resp.Body.Close()
	resp, _ = client.Get(srv.URL + "/clients")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after-logout: status = %d, хочу 401", resp.StatusCode)
	}
}

// TestWebAuthBearerStillWorks — bearer-токен продолжает пускать без сессии.
func TestWebAuthBearerStillWorks(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(WebAuth(WebAuthConfig{Token: "tok"}, inner))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/clients", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer: status = %d, хочу 200", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/clients", nil)
	req.Header.Set("Authorization", "Bearer nope")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-bearer: status = %d, хочу 401", resp.StatusCode)
	}
}

// TestWebAuthOpenWhenUnconfigured — без токена и пароля доступ открыт (совместимо
// с прежним незащищённым --web-api на loopback).
func TestWebAuthOpenWhenUnconfigured(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(WebAuth(WebAuthConfig{}, inner))
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/clients")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open: status = %d, хочу 200", resp.StatusCode)
	}
}

func login(t *testing.T, client *http.Client, base, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	resp, err := client.Post(base+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// cookieJar — минимальный http.CookieJar для тестов (одна пачка cookie на все хосты).
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	for _, nc := range cookies {
		replaced := false
		for i, ex := range j.cookies {
			if ex.Name == nc.Name {
				j.cookies[i] = nc
				replaced = true
			}
		}
		if !replaced {
			j.cookies = append(j.cookies, nc)
		}
	}
}

func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie {
	// Отфильтровываем удалённые (MaxAge<0) cookie.
	var out []*http.Cookie
	for _, c := range j.cookies {
		if c.MaxAge < 0 {
			continue
		}
		out = append(out, c)
	}
	return out
}
