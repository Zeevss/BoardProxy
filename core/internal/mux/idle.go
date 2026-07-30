package mux

import "time"

// streamIdleSweepInterval — как часто проверять простаивающие стримы. var, а
// не const, чтобы тесты могли ускорить цикл вместо ожидания реальных 30с.
var streamIdleSweepInterval = 30 * time.Second

// idleSweep периодически резетит стримы, не видевшие трафика (Write или
// deliver) дольше streamIdleTimeout, даже если сессия в целом активна —
// иначе забытая, но не оборванная сторонами вкладка/соединение висела бы в
// s.streams бесконечно.
func (s *Session) idleSweep() {
	defer s.wg.Done()
	t := time.NewTicker(streamIdleSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			for _, st := range s.idleStreams() {
				_ = st.Reset()
			}
		}
	}
}

// idleStreams снимает снимок стримов, простаивающих дольше streamIdleTimeout.
func (s *Session) idleStreams() []*Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	var idle []*Stream
	for _, st := range s.streams {
		last := time.Unix(0, st.lastActivity.Load())
		if time.Since(last) > s.streamIdleTimeout {
			idle = append(idle, st)
		}
	}
	return idle
}
