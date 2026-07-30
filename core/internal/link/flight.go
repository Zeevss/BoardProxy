package link

import (
	"context"
	"sync"
	"time"
)

// flight ограничивает число объектов в полёте от этой стороны и задаёт им темп.
// Эффективный лимит — min(лимит limiter'а, rwnd_link): адаптивный лимит
// параллельных записей (что терпит бэкенд доски, выведено из задержки) и
// объявленное получателем окно приёма (сколько пир готов держать). Pacing
// размазывает отправки по RTT, чтобы весь лимит не уходил бурстом.
type flight struct {
	lim *limiter

	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	rwnd     int // объявленный пиром максимум объектов; обновляется WindowAdvertise
	lastSend time.Time
	closed   bool
}

func newFlight(lim *limiter, initialRwnd int) *flight {
	if initialRwnd < 1 {
		initialRwnd = 1
	}
	f := &flight{lim: lim, rwnd: initialRwnd}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// snapshot возвращает текущее состояние для диагностики: сколько объектов в
// полёте, окно, объявленное пиром (rwnd), и текущий эффективный потолок
// min(лимит limiter'а, rwnd).
func (f *flight) snapshot() (inflight, rwnd, limit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inflight, f.rwnd, f.limitLocked()
}

// limitLocked — текущий потолок отправки в объектах. Вызывающий держит f.mu.
func (f *flight) limitLocked() int {
	lim := f.lim.window()
	if f.rwnd < lim {
		lim = f.rwnd
	}
	if lim < 1 {
		lim = 1
	}
	return lim
}

// acquire блокируется, пока не освободится слот в полёте и не пройдёт интервал
// pacing с предыдущей отправки, затем резервирует слот.
func (f *flight) acquire(ctx context.Context) error {
	f.mu.Lock()
	for {
		if f.closed {
			f.mu.Unlock()
			return ErrClosed
		}
		if f.inflight < f.limitLocked() {
			break
		}
		f.wait(ctx)
		if err := ctx.Err(); err != nil {
			f.mu.Unlock()
			return err
		}
	}
	f.inflight++

	// Pacing: разносим отправки на shortRTT/лимит. Считаем ожидание под замком,
	// затем спим вне его.
	interval := f.lim.pacingInterval()
	wait := time.Duration(0)
	if interval > 0 {
		earliest := f.lastSend.Add(interval)
		if d := time.Until(earliest); d > 0 {
			wait = d
		}
	}
	f.lastSend = time.Now().Add(wait)
	f.mu.Unlock()

	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			f.release()
			return ctx.Err()
		}
	}
	return nil
}

// wait блокируется на условии, просыпаясь при отмене ctx.
func (f *flight) wait(ctx context.Context) {
	stop := context.AfterFunc(ctx, func() {
		f.mu.Lock()
		f.cond.Broadcast()
		f.mu.Unlock()
	})
	f.cond.Wait()
	stop()
}

// release возвращает слот в полёте.
func (f *flight) release() {
	f.mu.Lock()
	if f.inflight > 0 {
		f.inflight--
	}
	f.cond.Broadcast()
	f.mu.Unlock()
}

// setRwnd обновляет объявленное пиром окно уровня link.
func (f *flight) setRwnd(n int) {
	if n < 1 {
		n = 1
	}
	f.mu.Lock()
	f.rwnd = n
	f.cond.Broadcast()
	f.mu.Unlock()
}

func (f *flight) close() {
	f.mu.Lock()
	f.closed = true
	f.cond.Broadcast()
	f.mu.Unlock()
}
