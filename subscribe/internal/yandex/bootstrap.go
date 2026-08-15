package yandex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type officeActionData struct {
	ActionURL      string `json:"action_url"`
	AccessToken    string `json:"access_token"`
	AccessTokenTTL int64  `json:"access_token_ttl"`
}

type sessionConfig struct {
	Xiva               xivaConfig `json:"xiva"`
	ResourceName       string     `json:"resourceName"`
	SessionID          string     `json:"sessionId"`
	SessionIndex       int        `json:"sessionIndex"`
	UserID             int        `json:"userId"`
	Permissions        int        `json:"permissions"`
	SessionInitialized bool       `json:"sessionInitialized"`
	Anonymous          bool       `json:"anonymous"`
	WOPIUserID         string     `json:"wopiUserId"`
}

type xivaConfig struct {
	URL          string `json:"url"`
	Service      string `json:"service"`
	User         string `json:"user"`
	Sign         string `json:"sign"`
	Timestamp    string `json:"ts"`
	FetchHistory bool   `json:"fetchHistory"`
}

type bootstrapResult struct {
	HTTPClient     *http.Client
	SpreadsheetURL string
	RequestPath    string
	Token          string
	Mode           string
	Config         sessionConfig
}

func bootstrap(ctx context.Context, shareURL string) (bootstrapResult, error) {
	share, err := url.Parse(shareURL)
	if err != nil || share.Scheme != "https" || !allowedYandexHost(share.Hostname()) {
		return bootstrapResult{}, fmt.Errorf("share URL must use a trusted Yandex host")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar: jar, Timeout: 30 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" || !allowedYandexHost(request.URL.Hostname()) {
				return fmt.Errorf("refuse redirect outside trusted Yandex hosts")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareURL, nil)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("create share request: %w", err)
	}
	setBrowserHeaders(req)
	response, err := client.Do(req)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("open share link: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	response.Body.Close()
	if readErr != nil {
		return bootstrapResult{}, fmt.Errorf("read share page: %w", readErr)
	}
	if response.StatusCode != http.StatusOK {
		return bootstrapResult{}, fmt.Errorf("open share link: HTTP %d", response.StatusCode)
	}

	action, err := parseOfficeActionData(body)
	if err != nil {
		return bootstrapResult{}, err
	}
	actionURL, err := url.Parse(action.ActionURL)
	if err != nil || actionURL.Scheme != "https" || !allowedYandexHost(actionURL.Hostname()) {
		return bootstrapResult{}, fmt.Errorf("officeActionData points outside trusted Yandex hosts")
	}
	form := url.Values{
		"access_token":     {action.AccessToken},
		"access_token_ttl": {strconv.FormatInt(action.AccessTokenTTL, 10)},
	}
	authRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPost, action.ActionURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("create Volga auth request: %w", err)
	}
	setBrowserHeaders(authRequest)
	authRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authRequest.Header.Set("Origin", "https://disk.yandex.ru")
	authRequest.Header.Set("Referer", response.Request.URL.String())

	redirectClient := *client
	redirectClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	authResponse, err := redirectClient.Do(authRequest)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("initialize Volga session: %w", err)
	}
	io.Copy(io.Discard, authResponse.Body)
	authResponse.Body.Close()
	if authResponse.StatusCode != http.StatusFound {
		return bootstrapResult{}, fmt.Errorf("initialize Volga session: HTTP %d", authResponse.StatusCode)
	}

	location := authResponse.Header.Get("Location")
	spreadsheetURL, err := url.Parse(location)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("parse Volga redirect: %w", err)
	}
	query := spreadsheetURL.Query()
	var config sessionConfig
	if err := json.Unmarshal([]byte(query.Get("json")), &config); err != nil {
		return bootstrapResult{}, fmt.Errorf("decode Volga session config: %w", err)
	}
	if query.Get("request-path") == "" || query.Get("token") == "" {
		return bootstrapResult{}, fmt.Errorf("Volga redirect lacks request-path or token")
	}

	return bootstrapResult{
		HTTPClient: client, SpreadsheetURL: location,
		RequestPath: query.Get("request-path"), Token: query.Get("token"),
		Mode: query.Get("mode"), Config: config,
	}, nil
}

func allowedYandexHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "yandex.ru" || strings.HasSuffix(host, ".yandex.ru") ||
		host == "yandex.com" || strings.HasSuffix(host, ".yandex.com")
}

func parseOfficeActionData(page []byte) (officeActionData, error) {
	const marker = `"officeActionData":`
	index := bytes.Index(page, []byte(marker))
	if index < 0 {
		return officeActionData{}, fmt.Errorf("share page has no officeActionData")
	}
	decoder := json.NewDecoder(bytes.NewReader(page[index+len(marker):]))
	var data officeActionData
	if err := decoder.Decode(&data); err != nil {
		return officeActionData{}, fmt.Errorf("decode officeActionData: %w", err)
	}
	if data.ActionURL == "" || data.AccessToken == "" {
		return officeActionData{}, fmt.Errorf("officeActionData is incomplete")
	}
	return data, nil
}

func setBrowserHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/151 Safari/537.36")
}
