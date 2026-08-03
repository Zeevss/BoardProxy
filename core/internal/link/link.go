// Пакет link — уровень канала передачи данных (L2): поверх одной board.Session
// и кодека он даёт надёжный, упорядоченный канал кадров с контролем потока и
// адаптивным лимитом параллельных записей до единственного пира на той же
// странице.
//
// Модель:
//   - Send кладёт объект с кадром, несущим порядковый номер. Свои data-объекты мы
//     никогда не удаляем и не ретрансмитим — носитель доски надёжен.
//   - Получатель декодирует объект пира и сразу удаляет его (удаление и есть ACK),
//     затем доставляет payload по порядку. ACK на приёме (а не после чтения
//     приложением) держит RTT чистым сигналом задержки доски/сети, не зависящим
//     от скорости потребителя.
//   - Контроль потока двухчастный. Адаптивный лимит параллельных записей
//     (limiter, gradient-based; см. limiter.go) выводит допустимый темп из
//     ACK-RTT. Окно приёма (rwnd_link) объявляется пиром по control-каналу.
//     Отправка ограничена min(лимит, rwnd_link) и размазана по RTT через
//     pacing (см. flight.go).
//   - Сама запись/удаление объекта на доске — сетевой вызов (ack от сервера
//     доски на сам modify-objects/drop-objects), а не мгновенная операция.
//     Send и обработка ACK-очереди не ждут этот вызов синхронно: иначе лимит
//     регулировал бы параллелизм, которого негде взяться — все записи шли бы
//     по одной за RTT. Вместо этого flight резервирует слот синхронно, а сама
//     доска-операция уходит в фоновый горутин; в полёте может быть до
//     min(лимит, rwnd_link) таких вызовов одновременно.
//
// Слой предполагает страницу 1:1 (один клиентский и один серверный конец).
// Многосторонние страницы (хаб) обрабатываются уровнем выше.
package link

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"bproxy-core/internal/board"
	"bproxy-core/internal/codec"
	"bproxy-core/internal/transportstats"
)

// ErrClosed возвращается из Send/Reconcile и внутренних ожиданий после закрытия
// канала.
var ErrClosed = errors.New("link: closed")

const (
	recvBuffer = 64
	ackBuffer  = 128

	// ackBatchCap — сколько id удаляется одним board-вызовом. Протокол доски
	// поддерживает пакетное drop-objects; воркер добирает уже накопившиеся в
	// ackCh id без ожидания, до этого потолка, и шлёт их одним Delete.
	ackBatchCap = 32

	// minAckWorkers — нижняя граница пула ACK-воркеров (см. New: пул растёт
	// вместе с recvWindow, но не бывает меньше этого). Каждый вызов теперь
	// гасит до ackBatchCap id разом, поэтому нужный параллелизм ниже, чем при
	// одиночных удалениях.
	minAckWorkers = 4

	// defaultRecvWindow — окно приёма (rwnd), которое мы объявляем пиру, если
	// вызывающий не задал своё. Не меньше MaxConcurrency: rwnd — это потолок
	// по памяти/бэклогу, а не второй регулятор скорости, и не должен быть тем,
	// что реально ограничивает min(лимит, rwnd) — эта работа у limiter'а.
	defaultRecvWindow = MaxConcurrency
	defaultSendWindow = 4

	// keepaliveInterval — период переотправки объявления окна. Служит и
	// keepalive'ом: пир видит активность и не считает нас пропавшими, и заодно
	// освежает rwnd_link.
	keepaliveInterval = 30 * time.Second

	// statsInterval — период отладочного лога состояния лимита/rwnd/RTT. Не
	// влияет на управление потоком, только на видимость: подбирать
	// Window/MaxFramePayload вслепую бессмысленно, не видя, что на самом деле
	// является потолком.
	statsInterval = 1 * time.Second
)

// Options конфигурирует Link.
type Options struct {
	// RecvWindow — сколько объектов эта сторона готова держать от пира (окно
	// приёма уровня link, которое мы предоставляем пиру).
	RecvWindow int
	// InitialSendWindow — предполагаемое окно пира, пока не пришло его объявление.
	InitialSendWindow int
	// Log — куда писать периодическую диагностику (лимит/rwnd/RTT) уровня
	// Debug. Если nil, используется slog.Default().
	Log *slog.Logger
}

// Link — надёжный канал кадров с контролем потока поверх одной страницы доски.
type Link struct {
	sess  board.Session
	codec codec.Codec
	self  string
	log   *slog.Logger

	lim         *limiter
	sizer       *sizer
	flight      *flight
	outstanding *outstanding
	reasm       *reasm // трогает только цикл run

	recvWindow int

	sendSeq atomic.Uint64
	// confirmedBytes counts link payload bytes whose board objects were
	// deleted by the peer (or disappeared from a reconnect snapshot). It is
	// the lane-level delivered-goodput counter used by the future v3 bundle
	// controller; unlike mux Written it does not count merely queued bytes.
	confirmedBytes atomic.Uint64
	lastRecv       atomic.Int64 // время последнего принятого события (UnixNano)
	recvCh         chan []byte
	ackCh          chan string

	reconcileCh chan []board.Object
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	wg          sync.WaitGroup

	// sendMu/sendCond/sendN считают фоновые горутины записи на доску (см. put),
	// отдельно от wg: они не запускаются заранее известным числом, а рождаются
	// по одной на Send. closing взводится в Close под тем же мьютексом, чтобы
	// Send не мог начать новую запись после того, как Close решил, что новых
	// быть не должно (без этого — гонка между "проверили closing" и "закрыли
	// sess, пока горутина ещё пишет").
	sendMu   sync.Mutex
	sendCond *sync.Cond
	sendN    int
	closing  bool
}

// ResolveRecvWindow возвращает эффективное окно приёма для n: n, если задано
// (>0), иначе дефолт транспортного слоя. Вынесено отдельно, чтобы вызывающий
// код (например, стартовые логи в app) мог показать реальное используемое
// значение, а не сырой (возможно нулевой) конфиг.
func ResolveRecvWindow(n int) int {
	if n <= 0 {
		return defaultRecvWindow
	}
	return n
}

// New запускает Link поверх sess с кодеком c. Link берёт владение sess и
// закрывает её в Close.
func New(sess board.Session, c codec.Codec, opts Options) *Link {
	recvWin := ResolveRecvWindow(opts.RecvWindow)
	initSend := opts.InitialSendWindow
	if initSend <= 0 {
		initSend = defaultSendWindow
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	lim := newLimiter()
	ctx, cancel := context.WithCancel(context.Background())
	l := &Link{
		sess:        sess,
		codec:       c,
		self:        sess.Participant(),
		log:         log,
		lim:         lim,
		sizer:       newSizer(),
		flight:      newFlight(lim, initSend),
		outstanding: newOutstanding(),
		reasm:       newReasm(),
		recvWindow:  recvWin,
		recvCh:      make(chan []byte, recvBuffer),
		ackCh:       make(chan string, ackBuffer),
		reconcileCh: make(chan []board.Object),
		ctx:         ctx,
		cancel:      cancel,
	}
	l.sendCond = sync.NewCond(&l.sendMu)
	l.lastRecv.Store(time.Now().UnixNano())
	// Число ACK-воркеров держим не меньше recvWindow/ackBatchCap: это верхняя
	// граница того, сколько параллельных батчей нужно, чтобы удаление чужих
	// объектов (наш ACK) шло параллельно, а не закладывалось в очередь — то же
	// узкое место, что чинили у Send, просто на приёме. Каждый воркер гасит до
	// ackBatchCap id одним вызовом, поэтому воркеров нужно меньше, чем окно.
	ackWorkers := (recvWin + ackBatchCap - 1) / ackBatchCap
	if ackWorkers < minAckWorkers {
		ackWorkers = minAckWorkers
	}
	l.wg.Add(3 + ackWorkers)
	go l.run()
	for i := 0; i < ackWorkers; i++ {
		go l.ackWorker()
	}
	go l.keepaliveLoop()
	go l.statsLoop()
	return l
}

// Send резервирует слот окна следующим кадром, блокируясь, пока окно отправки
// полно или pacing требует подождать, затем передаёт запись на доску фоновому
// горутину и возвращается, не дожидаясь ack самого modify-objects. Если ждать
// его здесь, единственный вызывающий (мультиплексор гонит кадры одним
// горутином) не смог бы начать следующую отправку, пока не придёт ack на
// предыдущую — лимит в таком случае ничего не регулирует, потому что
// параллелизму просто неоткуда взяться. Реальный предел одновременных записей
// на доску — это flight; Send лишь резервирует слот.
func (l *Link) Send(ctx context.Context, payload []byte) error {
	_, err := l.send(ctx, payload, nil)
	return err
}

// SendTracked is Send with a delivery receipt. The returned channel closes only
// after the peer confirms the board object (including confirmation discovered
// by reconnect reconciliation). A local Put failure closes the lane, not the
// receipt, so the bond layer can replay the packet elsewhere.
func (l *Link) SendTracked(ctx context.Context, payload []byte) (<-chan struct{}, error) {
	receipt := make(chan struct{})
	if _, err := l.send(ctx, payload, receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (l *Link) send(ctx context.Context, payload []byte, receipt chan struct{}) (<-chan struct{}, error) {
	seq := l.sendSeq.Add(1) - 1
	value, err := l.codec.Encode(encodeData(seq, payload))
	if err != nil {
		return nil, err
	}
	id := board.NewID()

	if err := l.flight.acquire(ctx); err != nil {
		return nil, err
	}
	l.outstanding.add(id, seq, len(payload), time.Now(), receipt)

	l.sendMu.Lock()
	if l.closing {
		l.sendMu.Unlock()
		l.outstanding.ack(id)
		l.flight.release()
		return nil, ErrClosed
	}
	l.sendN++
	l.sendMu.Unlock()

	go l.put(id, value)
	return receipt, nil
}

// Flush waits until all board writes accepted by Send have completed. The mux
// uses it for the final GOAWAY: Send is deliberately asynchronous for normal
// traffic, so closing the link immediately after Send could otherwise cancel
// the board Put before the peer ever observed the shutdown notification.
func (l *Link) Flush(ctx context.Context) error {
	// Wake a Cond waiter when the deadline/cancellation fires. AfterFunc is
	// stopped on the normal path, so it does not leave a goroutine behind.
	stop := context.AfterFunc(ctx, func() {
		l.sendMu.Lock()
		l.sendCond.Broadcast()
		l.sendMu.Unlock()
	})
	defer stop()

	l.sendMu.Lock()
	defer l.sendMu.Unlock()
	for l.sendN > 0 && ctx.Err() == nil {
		l.sendCond.Wait()
	}
	return ctx.Err()
}

// put выполняет саму запись на доску вне пути Send. Использует l.ctx (не ctx
// вызова Send — тот уже вернул управление вызывающему и не должен ограничивать
// фоновую работу). Ошибка здесь означает, что связь со средой потеряна
// (соединение мертво или сессия закрылась), поэтому глушим link целиком, а не
// пытаемся вернуть ошибку из уже завершившегося Send.
func (l *Link) put(id string, value string) {
	defer func() {
		l.sendMu.Lock()
		l.sendN--
		l.sendCond.Broadcast()
		l.sendMu.Unlock()
	}()
	if err := l.sess.Put(l.ctx, board.Object{ID: id, Value: value}); err != nil {
		l.outstanding.ack(id)
		l.flight.release()
		l.cancel()
	}
}

// Recv возвращает поток упорядоченных payload'ов от пира. Канал закрывается при
// закрытии link.
func (l *Link) Recv() <-chan []byte { return l.recvCh }

// Done closes when this physical lane can no longer send or receive.
func (l *Link) Done() <-chan struct{} { return l.ctx.Done() }

// Reconcile синхронизирует состояние по свежему снапшоту страницы после
// повторной подписки: наши объекты, которых нет в снапшоте, были заacked, пока
// нас не было (их слоты освобождаются), а объекты пира из снапшота переигрываются
// заново (дубли отсекаются по порядковому номеру).
func (l *Link) Reconcile(ctx context.Context, snapshot []board.Object) error {
	select {
	case l.reconcileCh <- snapshot:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-l.ctx.Done():
		return ErrClosed
	}
}

// Close останавливает link и закрывает нижележащую сессию. Ждёт завершения
// фоновых записей (put), запущенных из Send, прежде чем закрыть sess — иначе
// возможна гонка "пишем в уже закрытую сессию".
func (l *Link) Close() error {
	l.closeOnce.Do(func() {
		l.cancel()
		l.flight.close()
		l.sendMu.Lock()
		l.closing = true
		for l.sendN > 0 {
			l.sendCond.Wait()
		}
		l.sendMu.Unlock()
	})
	l.wg.Wait()
	return l.sess.Close()
}

// CloseGracefully notifies the peer that only this physical lane is being
// retired, then closes locally. It does not emit mux GOAWAY for the bundle.
func (l *Link) CloseGracefully(ctx context.Context) error {
	value, err := l.codec.Encode(encodeControl(encodeLaneClose()))
	if err == nil {
		err = l.sess.Put(ctx, board.Object{ID: board.NewID(), Value: value})
	}
	closeErr := l.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// keepaliveLoop периодически переотправляет объявление окна: это и keepalive
// (пир видит активность и не считает нас пропавшими), и обновление rwnd_link.
func (l *Link) keepaliveLoop() {
	defer l.wg.Done()
	l.advertise()
	t := time.NewTicker(keepaliveInterval)
	defer t.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-t.C:
			l.advertise()
		}
	}
}

// advertise отправляет пиру объявление окна приёма этой стороны.
func (l *Link) advertise() {
	value, err := l.codec.Encode(encodeControl(encodeWindowAdvertise(uint32(l.recvWindow))))
	if err != nil {
		return
	}
	if err := l.sess.Put(l.ctx, board.Object{ID: board.NewID(), Value: value}); err != nil && l.ctx.Err() == nil {
		// Молчаливо потерянный heartbeat оставляет пиру ложное впечатление, что
		// мы умерли. Неисправимая на уровне board.Session ошибка завершает link,
		// чтобы верхний клиент выполнил полноценный reconnect.
		l.cancel()
	}
}

// statsLoop периодически пишет в лог (Debug) текущее состояние лимита/rwnd/RTT
// — без этого подбор Window/MaxFramePayload идёт вслепую: непонятно, что
// реально является потолком в конкретном прогоне — лимит, объявленное пиром
// окно (rwnd) или что-то выше по стеку (например, per-stream окно mux).
func (l *Link) statsLoop() {
	defer l.wg.Done()
	t := time.NewTicker(statsInterval)
	defer t.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-t.C:
			l.logStats()
		}
	}
}

func (l *Link) logStats() {
	limit, shortRTT, longRTT := l.lim.snapshot()
	inflight, peerRwnd, effLimit := l.flight.snapshot()
	target, shortCost, longCost := l.sizer.snapshot()
	l.log.Debug("link transport stats",
		"cwnd", limit,
		"inflight", inflight,
		"peer_window", peerRwnd,
		"effective_window", effLimit,
		"receive_window", l.recvWindow,
		"rtt_ms", shortRTT.Milliseconds(),
		"base_rtt_ms", longRTT.Milliseconds(),
		"target_payload_bytes", target,
		"confirmed_payload_bytes", l.confirmedBytes.Load(),
		"recent_cost_ns_per_byte", shortCost,
		"base_cost_ns_per_byte", longCost,
	)
}

func (l *Link) run() {
	defer l.wg.Done()
	// A terminal board.Session failure is reported by closing Events. Propagate
	// it to Done so bond/hub do not leave a dead physical lane registered.
	defer l.cancel()
	defer close(l.recvCh)
	defer close(l.ackCh)
	events := l.sess.Events()
	// reconnects — снапшоты после прозрачного переподключения сессии (см.
	// board.Session.Reconnects). Обрабатываем их той же reconcile-логикой, что и
	// явный Reconcile при миграции. У драйверов без реконнекта канал nil и в
	// select не срабатывает.
	reconnects := l.sess.Reconnects()
	for {
		select {
		case <-l.ctx.Done():
			return
		case snap := <-l.reconcileCh:
			l.reconcile(snap)
		case snap, ok := <-reconnects:
			if !ok {
				reconnects = nil
				continue
			}
			l.reconcile(snap)
		case ev, ok := <-events:
			if !ok {
				return
			}
			l.handle(ev)
		}
	}
}

// LastActivity возвращает время последнего принятого от пира события. Хаб
// использует его, чтобы освобождать «повисшие» страницы простаивающих клиентов.
func (l *Link) LastActivity() time.Time {
	return time.Unix(0, l.lastRecv.Load())
}

// TargetBatchSize возвращает текущий адаптивный целевой размер батча в
// байтах (см. sizer.go) — mux использует его вместо статической константы
// при коалесинге data-кадров в один board-объект.
func (l *Link) TargetBatchSize() int {
	return l.sizer.targetSize()
}

// RTT возвращает текущую реактивную оценку RTT до пира (короткая EWMA) — для
// метрик. Ноль, пока не пришло ни одного сэмпла.
func (l *Link) RTT() time.Duration {
	_, shortRTT, _ := l.lim.snapshot()
	return shortRTT
}

// ConfirmedBytes returns the cumulative number of link payload bytes confirmed
// by the peer. The counter is monotonic for the lifetime of this Link.
func (l *Link) ConfirmedBytes() uint64 {
	return l.confirmedBytes.Load()
}

func (l *Link) TransportStats() []transportstats.Lane {
	cwnd, rtt, baseRTT := l.lim.snapshot()
	inflight, peerWindow, effectiveWindow := l.flight.snapshot()
	target, _, _ := l.sizer.snapshot()
	return []transportstats.Lane{{
		CongestionWindow: cwnd,
		Inflight:         inflight,
		PeerWindow:       peerWindow,
		EffectiveWindow:  effectiveWindow,
		TargetPayload:    target,
		RTT:              rtt,
		BaseRTT:          baseRTT,
		ConfirmedBytes:   l.confirmedBytes.Load(),
	}}
}

func (l *Link) handle(ev board.Event) {
	switch ev.Kind {
	case board.Deleted:
		// Пир удалил один из наших объектов — это его ACK. Control-объекты не
		// отслеживаются, поэтому их удаление проходит здесь безвредно.
		if rec, ok := l.outstanding.ack(ev.Object.ID); ok {
			l.lastRecv.Store(time.Now().UnixNano())
			l.confirmedBytes.Add(uint64(rec.size))
			confirmReceipt(rec)
			rtt := time.Since(rec.sent)
			l.lim.onAck(rtt)
			if rec.size >= sizerMinSampleSize {
				l.sizer.onAck(rtt, rec.size)
			}
			l.flight.release()
		}
	case board.Created:
		if ev.Object.CreatorHash == l.self {
			return // своих создаваний не бывает, но подстрахуемся
		}
		frame, err := l.codec.Decode(ev.Object.Value)
		if err != nil {
			// Lane pages are reserved transport storage. Garbage left by a prior
			// owner or a manual writer must not accumulate into the next snapshot.
			l.log.Debug("link removing undecodable foreign board object", "object", ev.Object.ID)
			l.enqueueAck(ev.Object.ID)
			return
		}
		kind, rest, ok := kindOf(frame)
		if !ok {
			l.enqueueAck(ev.Object.ID)
			return
		}
		// Обновляем peer-liveness только после проверки автора, sealed-кодека и
		// типа протокольного кадра. Реальная доска может эхоить наши собственные
		// broadcast-события; если считать их активностью, серверный heartbeat сам
		// себе бесконечно продлевает lease после SIGKILL клиента.
		l.lastRecv.Store(time.Now().UnixNano())
		switch kind {
		case frameControl:
			if t, v, ok := parseControl(rest); ok {
				switch t {
				case ctrlWindowAdvertise:
					l.flight.setRwnd(int(v))
				case ctrlLaneClose:
					l.enqueueAck(ev.Object.ID)
					l.cancel()
					return
				}
			}
			l.enqueueAck(ev.Object.ID)
		case frameData:
			// ACK на приёме — ради чистого RTT-сигнала, затем переупорядочивание и
			// доставка.
			seq, payload, ok := decodeData(frame)
			if !ok {
				l.enqueueAck(ev.Object.ID)
				return
			}
			l.enqueueAck(ev.Object.ID)
			ready, _ := l.reasm.accept(seq, payload)
			for _, p := range ready {
				select {
				case l.recvCh <- p:
				case <-l.ctx.Done():
					return
				}
			}
		}
	}
}

func (l *Link) reconcile(snapshot []board.Object) {
	present := make(map[string]bool, len(snapshot))
	for _, o := range snapshot {
		present[o.ID] = true
	}
	for _, id := range l.outstanding.snapshotIDs() {
		if !present[id] {
			if rec, ok := l.outstanding.ack(id); ok {
				l.confirmedBytes.Add(uint64(rec.size))
				confirmReceipt(rec)
				l.flight.release()
			}
		}
	}
	for _, o := range snapshot {
		if o.CreatorHash == l.self {
			continue
		}
		l.handle(board.Event{Kind: board.Created, Object: o})
	}
}

func confirmReceipt(rec sent) {
	if rec.receipt != nil {
		close(rec.receipt)
	}
}

// enqueueAck ставит id объекта пира в очередь на удаление (наш ACK).
func (l *Link) enqueueAck(id string) {
	select {
	case l.ackCh <- id:
	case <-l.ctx.Done():
	}
}

// ackWorker удаляет объекты пира вне цикла run, чтобы удаление-ACK никогда не
// тормозило обработку событий (что раздувало бы замеры RTT). Запускается в
// ackWorkers экземплярах на общий ackCh: Delete тоже ждёт ack от доски, и без
// пула эмиссия ACK шла бы по одному в RTT — тем же узким местом, что и Send.
//
// Каждый воркер, получив первый id, неблокирующе добирает из ackCh всё, что
// уже накопилось, до ackBatchCap, и шлёт один batched Delete — без ожидания
// новых поступлений (при пустом канале батч вырождается в один id, latency
// одиночного ACK не растёт).
func (l *Link) ackWorker() {
	defer l.wg.Done()
	for id := range l.ackCh {
		ids := make([]string, 1, ackBatchCap)
		ids[0] = id
	drain:
		for len(ids) < ackBatchCap {
			select {
			case more, ok := <-l.ackCh:
				if !ok {
					break drain
				}
				ids = append(ids, more)
			default:
				break drain
			}
		}
		if err := l.sess.Delete(l.ctx, ids...); err != nil && l.ctx.Err() == nil {
			// Потерянный ACK навсегда удержал бы flight-слот у пира. Ошибки
			// соединения драйвер ретраит сам; оставшаяся ошибка фатальна для link.
			l.log.Error("link failed to delete acknowledged board objects", "err", err, "objects", len(ids))
			l.cancel()
		}
	}
}
