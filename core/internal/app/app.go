// Пакет app — композиционный корень: собирает клиент и сервер из слоёв
// (board → link → mux → hub → proxy/egress) по конфигу.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/config"
	"bproxy-core/internal/crypto"
	"bproxy-core/internal/link"
)

// yandexDialer создаёт гостевые сессии Yandex Board для хаба.
type yandexDialer struct{ opts yandex.Options }

func (d yandexDialer) Join(ctx context.Context) (board.Session, error) {
	return yandex.Join(ctx, d.opts)
}

func boardOptions(cfg config.Config) yandex.Options {
	return yandex.Options{
		APIBase:   cfg.Board.APIBase,
		Hash:      cfg.Board.Hash,
		GuestName: cfg.Board.GuestName,
	}
}

func linkOptions(cfg config.Config, log *slog.Logger) link.Options {
	return link.Options{RecvWindow: cfg.Transport.Window, Log: log}
}

// serverKeypair загружает постоянный ключ сервера из файла path, создавая его
// при первом запуске. Идентичность сервера так переживает рестарты — иначе
// каждый рестарт делал бы недействительными все выданные keylink'и (клиент
// пинит публичный ключ сервера, а приватный ключ клиента нигде на сервере не
// хранится — перевыпустить keylink для существующего пользователя нечем).
// Чтобы намеренно сменить идентичность (инвалидировав все keylink'и), файл
// достаточно удалить — на следующем запуске сгенерируется новый.
func serverKeypair(path string) (crypto.Keypair, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		return crypto.KeypairFromPrivate(raw)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return crypto.Keypair{}, fmt.Errorf("read server key: %w", err)
	}
	kp, err := crypto.Generate()
	if err != nil {
		return crypto.Keypair{}, fmt.Errorf("generate server key: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return crypto.Keypair{}, fmt.Errorf("create key dir: %w", err)
		}
	}
	// 0600 — приватный ключ, читать должен только владелец.
	if err := os.WriteFile(path, kp.Private(), 0o600); err != nil {
		return crypto.Keypair{}, fmt.Errorf("write server key: %w", err)
	}
	return kp, nil
}

// poolExcluding возвращает слайды без hub-слайда — пул страниц для раздачи.
func poolExcluding(slides []string, hub string) []string {
	out := make([]string, 0, len(slides))
	for _, s := range slides {
		if s != hub {
			out = append(out, s)
		}
	}
	return out
}

// firstSlideSorted возвращает первый по алфавиту хэш слайда — детерминированный
// выбор hub-страницы, не зависящий от порядка, в котором API доски вернул
// список (он не отсортирован и может отличаться между запусками).
func firstSlideSorted(slides []string) (string, error) {
	if len(slides) == 0 {
		return "", errors.New("board has no slides")
	}
	sorted := append([]string(nil), slides...)
	sort.Strings(sorted)
	return sorted[0], nil
}

// resolveHubSlide определяет hub-страницу: явно заданная в конфиге всегда в
// приоритете, иначе — первый по алфавиту слайд доски.
func resolveHubSlide(cfg config.Config, sess *yandex.Session) (string, error) {
	if cfg.Server.HubPage != "" {
		return cfg.Server.HubPage, nil
	}
	return firstSlideSorted(sess.Slides())
}
