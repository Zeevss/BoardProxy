package proxy

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"

	"bproxy-core/internal/relay"
)

// serveHTTP обслуживает HTTP-прокси: CONNECT-туннель (в т.ч. HTTPS) и обычный
// forward-proxy по absolute-URI.
func serveHTTP(conn net.Conn, br *bufio.Reader, r *router, log *slog.Logger) {
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	clearConnDeadline(conn)

	if req.Method == http.MethodConnect {
		serveConnect(conn, br, req.Host, r, log)
		return
	}
	serveForward(conn, br, req, r, log)
}

// serveConnect устанавливает туннель к target и качает сырые байты.
func serveConnect(conn net.Conn, br *bufio.Reader, target string, r *router, log *slog.Logger) {
	st, err := r.dial(target)
	if err != nil {
		writeStatus(conn, http.StatusBadGateway)
		return
	}
	defer st.Close()
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	log.Debug("http connect", "target", target)
	relay.Pipe(st, bufConn{r: br, c: conn})
}

// serveForward пересылает обычный HTTP-запрос по absolute-URI на origin в
// origin-form и качает ответ обратно.
func serveForward(conn net.Conn, br *bufio.Reader, req *http.Request, r *router, log *slog.Logger) {
	host := req.URL.Host
	if host == "" {
		writeStatus(conn, http.StatusBadRequest)
		return
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "80")
	}

	st, err := r.dial(host)
	if err != nil {
		writeStatus(conn, http.StatusBadGateway)
		return
	}
	defer st.Close()

	// req.Write отправляет запрос в origin-form (путь+query, заголовок Host).
	if err := req.Write(st); err != nil {
		return
	}
	log.Debug("http forward", "target", host, "method", req.Method)
	relay.Pipe(st, bufConn{r: br, c: conn})
}

func writeStatus(conn net.Conn, code int) {
	_, _ = io.WriteString(conn, "HTTP/1.1 "+statusLine(code)+"\r\nConnection: close\r\n\r\n")
}

func statusLine(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "400 Bad Request"
	case http.StatusBadGateway:
		return "502 Bad Gateway"
	default:
		return "500 Internal Server Error"
	}
}
