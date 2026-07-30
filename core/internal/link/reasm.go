package link

// reasm переупорядочивает входящие payload'ы по порядковому номеру и отбрасывает
// дубли. Носитель доски надёжен, поэтому пропуски временны (доставка не по
// порядку или снапшот после переподключения), а не потери навсегда.
type reasm struct {
	next uint64
	buf  map[uint64][]byte
}

func newReasm() *reasm {
	return &reasm{buf: make(map[uint64][]byte)}
}

// accept принимает кадр. Возвращает payload'ы, готовые к доставке по порядку
// (пусто, если кадр занимает будущий слот), и dup == true, если кадр уже
// встречался.
func (r *reasm) accept(seq uint64, payload []byte) (ready [][]byte, dup bool) {
	if seq < r.next {
		return nil, true
	}
	if _, ok := r.buf[seq]; ok {
		return nil, true
	}
	if seq > r.next {
		r.buf[seq] = payload
		return nil, false
	}
	ready = append(ready, payload)
	r.next++
	for {
		p, ok := r.buf[r.next]
		if !ok {
			break
		}
		delete(r.buf, r.next)
		ready = append(ready, p)
		r.next++
	}
	return ready, false
}
