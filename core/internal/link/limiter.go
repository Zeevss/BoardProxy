package link

import (
	"sync"
	"time"
)

// limiter — адаптивный лимит числа параллельных записей на доску. Вдохновлён
// Netflix Gradient2 (adaptive concurrency limit для клиентов backend-сервисов),
// а не сетевым TCP AIMD — и разница здесь принципиальная, не косметическая.
//
// AIMD придуман для сети: много независимых потоков по-честному делят
// пропускную способность общего канала, и единственный способ узнать её
// границу — намеренно её проскочить (потеря пакета) и тут же резко подрезать
// окно, чтобы освободить место соседям. У нас же ровно один поток (участник)
// против одного бэкенда (Yandex Board), никого делить не с кем, а потерь нет —
// канал (WebSocket) надёжен. Формой перегрузки, которую мы реально наблюдаем,
// оказалась не сетевая, а очередь на обработку запросов на стороне бэкенда:
// RTT растёт под нашей же нагрузкой и плавно спадает, когда она снижается —
// не бинарное "потеряли/не потеряли", а непрерывный градиент.
//
// Модель: короткая (реактивная) и длинная (базовая) сглаженные оценки RTT.
// Их отношение (gradient) показывает, насколько сейчас хуже обычного, и лимит
// подстраивается пропорционально одной формулой — без раздельных веток
// роста/падения и без намеренных просадок AIMD. Длинная оценка — EWMA, а не
// исторический минимум: она дрейфует в обе стороны вслед за реальностью,
// вместо того чтобы залипать на однажды увиденном лучшем RTT навсегда.
type limiter struct {
	mu sync.Mutex

	limit float64

	shortRTT time.Duration // реактивная оценка RTT (быстрая EWMA)
	longRTT  time.Duration // базовая оценка RTT (медленная EWMA, дрейфует в обе стороны)
	haveRTT  bool

	// lastAdjust — когда лимит последний раз пересчитывался. Пересчёт — не
	// чаще одного раза за shortRTT: без этого пачка ACK, пришедших почти
	// одновременно (типично при большом лимите — десятки объектов в полёте
	// подтверждаются в одном окне), продавливала бы лимит за раз слишком
	// резко в любую сторону (см. TestLimiterRapidAcksDoNotCompound).
	lastAdjust time.Time
}

// MaxConcurrency — потолок лимита. Экспортирован, чтобы link.go мог держать
// окно приёма (rwnd, которое мы объявляем пиру) заведомо не ниже этого
// потолка: rwnd — это ограничение по памяти/бэклогу, а не второй регулятор
// скорости — эту роль уже играет limiter.
//
// Держим потолок невысоким: адаптивный sizer (см. sizer.go) сам растит размер
// объекта до нескольких МБ, и десятков параллельных МБ-объектов для
// заполнения RTT не нужно — это в основном добавляет нагрузку на бэкенд, а не
// полезный пайплайнинг, и раскачивает limiter вместе с sizer'ом сильнее, чем
// нужно (оба реагируют на один и тот же RTT-сигнал).
const MaxConcurrency = 24

const (
	initLimit = 4
	minLimit  = 1

	shortAlpha = 0.2  // вес нового сэмпла в короткой EWMA — реагирует за ~5 сэмплов
	longAlpha  = 0.02 // вес нового сэмпла в длинной EWMA — реагирует за ~50 сэмплов

	// minGradient — нижняя граница отношения longRTT/shortRTT: не даёт одному
	// пересчёту обвалить лимит больше чем вдвое за раз.
	minGradient = 0.5
	// headroom — небольшой запас в формуле пересчёта: при здоровом RTT
	// (gradient≈1) это и есть шаг роста, а при просадке — то, что не даёт
	// лимиту схлопнуться в ноль даже при длительной перегрузке (у формулы
	// limit = limit*gradient + headroom при gradient=minGradient есть
	// устойчивая точка около headroom/(1-minGradient), а не 0).
	headroom = 2.0
)

func newLimiter() *limiter {
	return &limiter{limit: initLimit}
}

// onAck принимает один сэмпл ACK-RTT и обновляет лимит.
func (l *limiter) onAck(rtt time.Duration) {
	if rtt <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.haveRTT {
		l.shortRTT = rtt
		l.longRTT = rtt
		l.haveRTT = true
		return
	}
	l.shortRTT = ewma(l.shortRTT, rtt, shortAlpha)
	l.longRTT = ewma(l.longRTT, rtt, longAlpha)

	now := time.Now()
	if !l.lastAdjust.IsZero() && now.Sub(l.lastAdjust) < l.shortRTT {
		return
	}
	l.lastAdjust = now

	gradient := clampFloat(float64(l.longRTT)/float64(l.shortRTT), minGradient, 1.0)
	l.limit = clampFloat(l.limit*gradient+headroom, minLimit, MaxConcurrency)
}

func ewma(old, sample time.Duration, alpha float64) time.Duration {
	return time.Duration((1-alpha)*float64(old) + alpha*float64(sample))
}

// snapshot возвращает текущее состояние для диагностики (не влияет на
// управление, только для логирования).
func (l *limiter) snapshot() (limit int, shortRTT, longRTT time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(l.limit), l.shortRTT, l.longRTT
}

// window возвращает текущий лимит в целых объектах (>= 1).
func (l *limiter) window() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := int(l.limit)
	if n < 1 {
		n = 1
	}
	return n
}

// pacingInterval возвращает целевой интервал между отправками объектов, чтобы
// лимит размазывался по RTT, а не уходил бурстом. Ноль, пока RTT неизвестен.
func (l *limiter) pacingInterval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.haveRTT || l.limit < 1 {
		return 0
	}
	return time.Duration(float64(l.shortRTT) / l.limit)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
