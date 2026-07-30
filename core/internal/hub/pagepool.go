package hub

import "sync"

// pagePool раздаёт страницы (слайды) из фиксированного набора: хаб резервирует
// первый слайд под rendezvous, а остальные выдаёт клиентам по одному.
type pagePool struct {
	mu   sync.Mutex
	free []string
	busy map[string]bool
}

func newPagePool(pages []string) *pagePool {
	free := make([]string, len(pages))
	copy(free, pages)
	return &pagePool{free: free, busy: make(map[string]bool)}
}

// acquire выдаёт свободную страницу. ok == false, если пул исчерпан.
func (p *pagePool) acquire() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return "", false
	}
	page := p.free[0]
	p.free = p.free[1:]
	p.busy[page] = true
	return page, true
}

// release возвращает страницу в пул.
func (p *pagePool) release(page string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.busy[page] {
		return
	}
	delete(p.busy, page)
	p.free = append(p.free, page)
}

// available возвращает число свободных страниц.
func (p *pagePool) available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.free)
}
