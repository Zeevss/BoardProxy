package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// App — ссылка на клиент для конкретной платформы.
type App struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

// Settings — конфигурация сервиса, которой владеет control-plane.
// Сам subscribe не хранит ничего из этого на диске.
type Settings struct {
	Revision           int64  `json:"revision"`
	Enabled            bool   `json:"enabled"`
	ServiceName        string `json:"serviceName"`
	Icon               string `json:"icon"`
	PublicURL          string `json:"publicUrl"`
	YandexEditorURL    string `json:"yandexEditorUrl"`
	RecoveryKeyID      string `json:"recoveryKeyId"`
	RecoveryPrivateKey string `json:"recoveryPrivateKey"`
	Apps               []App  `json:"apps"`
	// RestartRequested true ровно в той выдаче, что несёт запрошенный перезапуск.
	RestartRequested bool `json:"restartRequested"`
}

// Report — то, что сервис сообщает о себе тем же запросом.
type Report struct {
	Revision             *int64     `json:"revision"`
	ServiceVersion       string     `json:"serviceVersion,omitempty"`
	RecoveryWatcherReady *bool      `json:"recoveryWatcherReady,omitempty"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
}

// Poll отправляет отчёт и забирает конфигурацию. Возвращает nil без ошибки,
// когда у сервиса уже актуальная ревизия и перезапуск не запрошен.
func (c *Client) Poll(ctx context.Context, report Report) (*Settings, error) {
	body, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint+"/api/v1/subscription-service/poll", bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("poll subscription service settings: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, &StatusError{Status: response.StatusCode, Detail: string(bytes.TrimSpace(raw))}
	}
	var settings Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("decode subscription service settings: %w", err)
	}
	if settings.Revision <= 0 {
		return nil, fmt.Errorf("control-plane returned an invalid settings revision %d", settings.Revision)
	}
	return &settings, nil
}
