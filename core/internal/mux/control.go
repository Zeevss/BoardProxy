package mux

// The send side has two queues: a control queue for the priority system channel
// (SYN and RESET) and a per-stream data queue for stream bytes and FIN. The
// single writer goroutine always drains control before data, so stream setup
// (SYN) and abort (RESET) are never stuck behind a backlog of data — this is
// the "system channel first priority" requirement. Control is never coalesced
// into a data batch and never waits for one to fill: if control has a frame
// ready, pickBatch returns it alone, immediately.
//
// FIN is deliberately on the data queue: it is an orderly end-of-stream marker
// that must follow the stream's data, whereas RESET is an out-of-band abort that
// is meant to overtake pending data.
//
// Both queues live behind one mutex (qMu/qCond) rather than Go channels: a
// channel-select-based priority pick has an unavoidable gap between checking
// "is control ready" and blocking on both queues, during which a control frame
// enqueued in that gap could lose to data in Go's pseudo-random select instead
// of preempting it. Under one mutex, enqueue and pick fully serialize, so
// control is never skipped once it is queued.
//
// Data frames are queued per stream (dataByStream) rather than in one shared
// FIFO: pickBatch drains them round-robin across streams (one frame per stream
// per lap) instead of letting one busy stream monopolise the link behind
// whatever it queued first. dataOrder is the round-robin ring of stream ids
// with a non-empty queue.

// enqueueControl queues a system-channel frame, blocking while the control
// queue is full (back-pressure) until space frees or the session closes.
func (s *Session) enqueueControl(f frameOut) error {
	s.qMu.Lock()
	defer s.qMu.Unlock()
	for len(s.control) >= controlQueueCap && !s.qClosed {
		s.qCond.Wait()
	}
	if s.qClosed {
		return ErrClosed
	}
	s.control = append(s.control, f)
	s.qCond.Broadcast()
	return nil
}

// enqueueData queues a data frame on its stream's queue, blocking while the
// total queued data (across all streams) is at capacity — back-pressure here
// is a shared ceiling, not per-stream, since each stream's own sendMax
// already bounds how much of it can be queued at once.
func (s *Session) enqueueData(f frameOut) error {
	s.qMu.Lock()
	defer s.qMu.Unlock()
	blocked := false
	for s.dataLen >= dataQueueCap && !s.qClosed {
		if !blocked {
			blocked = true
			s.blockedWriters.Add(1)
		}
		s.qCond.Wait()
	}
	if blocked {
		s.blockedWriters.Add(-1)
	}
	if s.qClosed {
		return ErrClosed
	}
	if len(s.dataByStream[f.stream]) == 0 {
		s.dataOrder = append(s.dataOrder, f.stream)
	}
	s.dataByStream[f.stream] = append(s.dataByStream[f.stream], f)
	s.dataLen++
	s.dataBytes += encodedLen(f)
	s.qCond.Broadcast()
	return nil
}

// pickBatch returns the next batch of frames to write: a lone control frame
// if one is queued, otherwise a round-robin sweep of data frames accumulated
// up to coalesceTarget bytes (or until the data queue drains) — never both in
// the same batch, and never waiting for more data to arrive. It blocks until
// something is queued or the session closes (ok=false).
func (s *Session) pickBatch() ([]frameOut, bool) {
	s.qMu.Lock()
	defer s.qMu.Unlock()
	for {
		if len(s.control) > 0 {
			f := s.control[0]
			s.control = s.control[1:]
			s.qCond.Broadcast() // будит отправителей, ждущих места в очереди
			return []frameOut{f}, true
		}
		if len(s.dataOrder) > 0 {
			batch := s.drainDataLocked()
			s.qCond.Broadcast()
			return batch, true
		}
		if s.qClosed {
			return nil, false
		}
		s.qCond.Wait()
	}
}

// drainDataLocked collects a round-robin sweep of data frames up to
// coalesceTarget bytes. Called with qMu held.
func (s *Session) drainDataLocked() []frameOut {
	var batch []frameOut
	size := 0
	target := int(s.coalesceTarget.Load())
	for len(s.dataOrder) > 0 && size < target {
		id := s.dataOrder[0]
		s.dataOrder = s.dataOrder[1:]
		queue := s.dataByStream[id]
		f := queue[0]
		queue = queue[1:]
		batch = append(batch, f)
		size += encodedLen(f)
		s.dataLen--
		s.dataBytes -= encodedLen(f)
		if len(queue) > 0 {
			s.dataByStream[id] = queue
			s.dataOrder = append(s.dataOrder, id) // вернуть в конец кольца
		} else {
			delete(s.dataByStream, id)
		}
	}
	return batch
}

// writer serialises queued batches onto the link, control first. It refreshes
// the coalesce target from conn.TargetBatchSize() on every iteration, so each
// batch picks up the latest value from the adaptive sizer (see
// internal/link/sizer.go) rather than a value fixed once at startup.
func (s *Session) writer() {
	defer s.wg.Done()
	for {
		s.setCoalesceTarget(s.conn.TargetBatchSize())
		batch, ok := s.pickBatch()
		if !ok {
			return
		}
		if err := s.conn.Send(s.ctx, encodeBatch(batch)); err != nil {
			s.closeWithError(err)
			return
		}
	}
}
