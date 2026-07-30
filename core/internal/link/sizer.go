package link

import (
	"sync"
	"time"
)

// sizer — адаптивный целевой размер батча при коалесинге в mux (см.
// internal/mux/control.go): вместо фиксированного размера, зависящего от
// конкретной сети/бэкенда, target находится непрерывно из наблюдаемого RTT.
//
// Формула та же по духу, что у limiter (Gradient2: короткая/длинная
// сглаженные оценки, gradient, пересчёт не чаще раза за характерный RTT), но
// сэмплирует не сырой RTT, а RTT, нормированный на размер отправленного
// объекта (cost = rtt/size, условно «наносекунд на байт»). Это принципиально:
// сырой RTT объекта сам по себе сильно зависит от его размера (не только от
// перегрузки бэкенда), поэтому сравнивать short/long-оценки сырого RTT
// перестаёт быть честным сигналом перегрузки, как только размер объекта
// переменный — батч побольше почти механически покажет RTT хуже базового, и
// такой сигнал заставлял бы регулятор резать себя после каждой попытки
// вырасти. cost = rtt/size от размера уже не зависит.
//
// limiter (сколько объектов параллельно) и sizer (какого размера один объект)
// — разные оси, но не полностью развязанные на практике: cost = rtt/size
// наследует всю вариацию rtt, когда перегрузка вызвана не размером, а именно
// параллелизмом (лимитер сам разогнал слишком много одновременных отправок) —
// тогда оба регулятора видят «стало хуже» на одном и том же событии и
// одновременно режут себя, раскачивая систему сильнее, чем каждый по
// отдельности. sizerGateMultiplier и более мягкий minCostGradient (см. ниже)
// держат sizer заметно медленнее limiter'а, чтобы он не подхватывал каждую
// его коррекцию как собственный сигнал.
type sizer struct {
	mu sync.Mutex

	target float64 // текущая целевая величина батча, байт

	shortCost float64 // реактивная оценка стоимости (быстрая EWMA), нс/байт
	longCost  float64 // базовая оценка стоимости (медленная EWMA), нс/байт
	haveCost  bool

	// shortRTT — отдельная EWMA сырого RTT, только для гейтинга интервала
	// пересчёта (тот же приём, что у limiter). Не смешивается с shortCost —
	// sizer полностью независим от limiter, никакого доступа к нему нет.
	shortRTT time.Duration

	lastAdjust time.Time
}

const (
	initTarget = 3 << 20  // 3 МиБ — стартовая точка = прежний статический дефолт (не регрессия на day 0); дальше sizer ищет сам
	minTarget  = 64 << 10 // 64 КиБ — пол: ниже него оверхед на один board-объект съедает выгоду от коалесинга
	maxTarget  = 16 << 20 // 16 МиБ — потолок: страховка от неограниченного роста, с большим запасом над эмпирическим максимумом

	// growthRate — доля роста target за один здоровый пересчёт. Мультипликативно
	// (не аддитивным +headroom, как у limiter): диапазон target — многие
	// порядки величины (десятки КБ .. единицы МБ), фиксированный аддитивный шаг
	// рос бы от минимума до максимума неприемлемо долго.
	growthRate = 0.1

	// minCostGradient — нижний пол gradient, аналог limiter.minGradient, но
	// мягче (у limiter — 0.5): sizer должен реагировать на устойчивый рост
	// стоимости заметно спокойнее limiter'а (см. комментарий к типу sizer),
	// а не обваливаться так же резко на каждой просадке.
	minCostGradient = 0.7

	// sizerGateMultiplier растягивает гейт "не чаще одного пересчёта за
	// характерный RTT" (у limiter — множитель 1) в несколько раз — sizer
	// реагирует на устойчивые тренды заметно медленнее limiter'а, вместо того
	// чтобы подхватывать те же RTT-всплески на той же частоте и усиливать его
	// коррекции вместо того, чтобы дать им отыграть самостоятельно.
	sizerGateMultiplier = 4

	// sizerMinSampleSize — сэмплы мельче игнорируются. mux-уровневые
	// control-кадры (SYN/RESET/WINDOW_UPDATE) идут тем же путём Send()/ACK,
	// что и data-батчи; для limiter это фоновый шум, но для sizer — системный
	// выброс: одиночный SYN в десятки байт даёт cost на порядки выше
	// мегабайтного батча при той же настоящей RTT, и без этого гейта
	// систематически обваливал бы shortCost на каждый OpenStream/RESET.
	sizerMinSampleSize = 4 << 10
)

func newSizer() *sizer {
	return &sizer{target: initTarget}
}

// onAck принимает один сэмпл (RTT, размер отправленного объекта в байтах) и
// обновляет целевой размер батча. Сэмплы мельче sizerMinSampleSize
// игнорируются (см. комментарий к константе) — вызывающий всё равно должен
// сам отфильтровать их перед вызовом, но проверка дублируется здесь ради
// безопасности вызова.
func (s *sizer) onAck(rtt time.Duration, size int) {
	if rtt <= 0 || size < sizerMinSampleSize {
		return
	}
	cost := float64(rtt) / float64(size)

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.haveCost {
		s.shortCost = cost
		s.longCost = cost
		s.shortRTT = rtt
		s.haveCost = true
		return
	}
	s.shortCost = ewmaFloat(s.shortCost, cost, shortAlpha)
	s.longCost = ewmaFloat(s.longCost, cost, longAlpha)
	s.shortRTT = ewma(s.shortRTT, rtt, shortAlpha)

	now := time.Now()
	if !s.lastAdjust.IsZero() && now.Sub(s.lastAdjust) < s.shortRTT*sizerGateMultiplier {
		return
	}
	s.lastAdjust = now

	gradient := clampFloat(s.longCost/s.shortCost, minCostGradient, 1.0)
	s.target = clampFloat(s.target*(gradient+growthRate), minTarget, maxTarget)
}

func ewmaFloat(old, sample, alpha float64) float64 {
	return (1-alpha)*old + alpha*sample
}

// targetSize возвращает текущий целевой размер батча в байтах.
func (s *sizer) targetSize() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(s.target)
}

// snapshot возвращает текущее состояние для диагностики (не влияет на
// управление, только для логирования).
func (s *sizer) snapshot() (target int, shortCost, longCost float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(s.target), s.shortCost, s.longCost
}
