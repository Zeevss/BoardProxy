// Пакет relay соединяет две половинно-закрываемые стороны (mux-стрим и TCP-сокет)
// и качает байты в обе стороны, пробрасывая EOF как half-close.
package relay

import "io"

// Stream — сторона релея: чтение, запись и половинное закрытие записи.
// Ему удовлетворяют *mux.Stream и *net.TCPConn.
type Stream interface {
	io.Reader
	io.Writer
	CloseWrite() error
}

// Pipe качает данные a<->b, закрывая запись противоположной стороны при EOF, и
// возвращается, когда обе стороны завершились. Полное закрытие ресурсов —
// ответственность вызывающего.
func Pipe(a, b Stream) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		_ = a.CloseWrite()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		_ = b.CloseWrite()
		done <- struct{}{}
	}()
	<-done
	<-done
}
