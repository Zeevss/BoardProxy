// Пакет app — композиционный корень: собирает клиент и сервер из слоёв
// (board → link → mux → hub → proxy/egress) по конфигу.
package app

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"bproxy-core/internal/board"
	"bproxy-core/internal/board/yandex"
	"bproxy-core/internal/link"
	"bproxy-core/internal/serverconfig"
)

// yandexDialer создаёт гостевые сессии Yandex Board для хаба.
type yandexDialer struct{ opts yandex.Options }

func (d yandexDialer) Join(ctx context.Context) (board.Session, error) {
	return yandex.Join(ctx, d.opts)
}

func boardOptions(cfg serverconfig.Board) yandex.Options {
	apiBase := cfg.APIBase
	if apiBase == "" {
		apiBase = "https://boards.yandex.ru/api"
	}
	guestName := cfg.GuestName
	if guestName == "" {
		guestName = "bproxy"
	}
	return yandex.Options{
		APIBase: apiBase, Hash: cfg.Hash, GuestName: guestName,
	}
}

func linkOptions(cfg serverconfig.Config, log *slog.Logger) link.Options {
	return link.Options{RecvWindow: cfg.Transport.Window, Log: log}
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
func resolveHubSlide(explicit string, sess *yandex.Session) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	return firstSlideSorted(sess.Slides())
}
