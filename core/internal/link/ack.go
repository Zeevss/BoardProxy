package link

import (
	"sync"
	"time"
)

// sent описывает объект, положенный на страницу, но ещё не подтверждённый
// (удалённый пиром): его порядковый номер, время отправки и размер, чтобы из
// ACK получить сэмплы RTT (для limiter) и RTT/размер (для sizer) для контроля
// перегрузки.
type sent struct {
	seq     uint64
	sent    time.Time
	size    int
	receipt chan struct{}
}

// outstanding отслеживает неподтверждённые объекты этой стороны по id.
type outstanding struct {
	mu  sync.Mutex
	ids map[string]sent
}

func newOutstanding() *outstanding {
	return &outstanding{ids: make(map[string]sent)}
}

func (o *outstanding) add(id string, seq uint64, size int, at time.Time, receipt chan struct{}) {
	o.mu.Lock()
	o.ids[id] = sent{seq: seq, sent: at, size: size, receipt: receipt}
	o.mu.Unlock()
}

// ack убирает id объекта и возвращает его запись отправки и признак того, что он
// был неподтверждённым. ok == false означает, что id уже подтверждён (дубль ACK)
// или это не data-объект, — тогда вызывающий не должен освобождать слот.
func (o *outstanding) ack(id string) (sent, bool) {
	o.mu.Lock()
	rec, ok := o.ids[id]
	delete(o.ids, id)
	o.mu.Unlock()
	return rec, ok
}

// snapshotIDs возвращает id, неподтверждённые в данный момент.
func (o *outstanding) snapshotIDs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ids := make([]string, 0, len(o.ids))
	for id := range o.ids {
		ids = append(ids, id)
	}
	return ids
}
