package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"bproxy-core/internal/relay"
)

// Минимальная реализация SOCKS5 (RFC 1928): без аутентификации, только CONNECT.
const (
	socks5Version = 0x05

	socksNoAuth       = 0x00
	socksNoMethod     = 0xff
	socksCmdConnect   = 0x01
	socksCmdUDP       = 0x03
	socksAtypIPv4     = 0x01
	socksAtypDomain   = 0x03
	socksAtypIPv6     = 0x04
	socksRepSuccess   = 0x00
	socksRepCmdNotSup = 0x07
)

func serveSOCKS5(conn net.Conn, br *bufio.Reader, r *router, log *slog.Logger) {
	if err := socksHandshake(br, conn); err != nil {
		log.Debug("socks5 handshake failed", "err", err)
		return
	}
	cmd, target, err := socksReadRequest(br)
	if err != nil {
		log.Debug("socks5 request failed", "err", err)
		_ = socksReply(conn, socksRepCmdNotSup)
		return
	}
	clearConnDeadline(conn)

	if cmd == socksCmdUDP {
		serveSOCKS5UDP(conn, br, r, log)
		return
	}
	if cmd != socksCmdConnect {
		_ = socksReply(conn, socksRepCmdNotSup)
		return
	}

	st, err := r.dial(target)
	if err != nil {
		log.Debug("dial failed", "target", target, "err", err)
		_ = socksReply(conn, socksRepCmdNotSup)
		return
	}
	defer st.Close()
	if err := socksReply(conn, socksRepSuccess); err != nil {
		return
	}
	log.Debug("socks5 connect", "target", target)
	relay.Pipe(st, bufConn{r: br, c: conn})
}

// socksHandshake читает приветствие и отвечает «без аутентификации».
func socksHandshake(br *bufio.Reader, w io.Writer) error {
	ver, err := br.ReadByte()
	if err != nil {
		return err
	}
	if ver != socks5Version {
		return fmt.Errorf("socks5: bad version %#x", ver)
	}
	n, err := br.ReadByte()
	if err != nil {
		return err
	}
	methods := make([]byte, int(n))
	if _, err := io.ReadFull(br, methods); err != nil {
		return err
	}
	supported := false
	for _, method := range methods {
		if method == socksNoAuth {
			supported = true
			break
		}
	}
	if !supported {
		_, _ = w.Write([]byte{socks5Version, socksNoMethod})
		return fmt.Errorf("socks5: no supported authentication method")
	}
	_, err = w.Write([]byte{socks5Version, socksNoAuth})
	return err
}

// socksReadRequest читает CONNECT-запрос и возвращает target "host:port".
func socksReadRequest(br *bufio.Reader) (byte, string, error) {
	hdr := make([]byte, 4) // ver, cmd, rsv, atyp
	if _, err := io.ReadFull(br, hdr); err != nil {
		return 0, "", err
	}
	if hdr[0] != socks5Version || hdr[2] != 0 {
		return 0, "", errUnsupported
	}

	var host string
	switch hdr[3] {
	case socksAtypIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = net.IP(b).String()
	case socksAtypIPv6:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = net.IP(b).String()
	case socksAtypDomain:
		l, err := br.ReadByte()
		if err != nil {
			return 0, "", err
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(br, b); err != nil {
			return 0, "", err
		}
		host = string(b)
	default:
		return 0, "", errUnsupported
	}

	portb := make([]byte, 2)
	if _, err := io.ReadFull(br, portb); err != nil {
		return 0, "", err
	}
	port := binary.BigEndian.Uint16(portb)
	return hdr[1], net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// socksReply отправляет ответ с фиктивным bound-адресом 0.0.0.0:0.
func socksReply(w io.Writer, rep byte) error {
	return socksReplyAddr(w, rep, &net.UDPAddr{IP: net.IPv4zero})
}

func socksReplyAddr(w io.Writer, rep byte, addr *net.UDPAddr) error {
	ip := addr.IP
	if v4 := ip.To4(); v4 != nil {
		buf := []byte{socks5Version, rep, 0x00, socksAtypIPv4}
		buf = append(buf, v4...)
		buf = append(buf, byte(addr.Port>>8), byte(addr.Port))
		_, err := w.Write(buf)
		return err
	}
	buf := []byte{socks5Version, rep, 0x00, socksAtypIPv6}
	buf = append(buf, ip.To16()...)
	buf = append(buf, byte(addr.Port>>8), byte(addr.Port))
	_, err := w.Write(buf)
	return err
}

func serveSOCKS5UDP(control net.Conn, br *bufio.Reader, r *router, log *slog.Logger) {
	d, err := r.openDatagram()
	if err != nil {
		_ = socksReply(control, socksRepCmdNotSup)
		return
	}
	defer d.Close()

	bindIP := net.IPv4(127, 0, 0, 1)
	if local, ok := control.LocalAddr().(*net.TCPAddr); ok && local.IP != nil && !local.IP.IsUnspecified() {
		bindIP = local.IP
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: bindIP})
	if err != nil {
		_ = socksReply(control, socksRepCmdNotSup)
		return
	}
	defer udp.Close()
	if err := socksReplyAddr(control, socksRepSuccess, udp.LocalAddr().(*net.UDPAddr)); err != nil {
		return
	}

	var once sync.Once
	stop := func() { once.Do(func() { _ = udp.Close() }) }
	defer stop()
	go func() {
		_, _ = br.ReadByte() // UDP association lives exactly as long as TCP control.
		stop()
	}()

	var clientMu sync.RWMutex
	var clientAddr *net.UDPAddr
	var expectedIP net.IP
	if remote, ok := control.RemoteAddr().(*net.TCPAddr); ok {
		expectedIP = remote.IP
	}
	go func() {
		for {
			packet, err := d.Receive(context.Background())
			if err != nil {
				stop()
				return
			}
			clientMu.RLock()
			client := clientAddr
			clientMu.RUnlock()
			if client == nil {
				continue
			}
			encoded, err := encodeSOCKS5UDP(packet.Target, packet.Payload)
			if err == nil {
				_, _ = udp.WriteToUDP(encoded, client)
			}
		}
	}()

	buf := make([]byte, 65535)
	for {
		n, source, err := udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if expectedIP != nil && !source.IP.Equal(expectedIP) {
			continue
		}
		clientMu.Lock()
		if clientAddr == nil {
			clientAddr = source
		}
		client := clientAddr
		clientMu.Unlock()
		if !source.IP.Equal(client.IP) || source.Port != client.Port {
			continue
		}
		target, payload, err := decodeSOCKS5UDP(buf[:n])
		if err != nil {
			log.Debug("socks5 udp packet rejected", "err", err)
			continue
		}
		if err := d.Send(target, payload); err != nil {
			return
		}
	}
}

func decodeSOCKS5UDP(packet []byte) (string, []byte, error) {
	if len(packet) < 4 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
		return "", nil, fmt.Errorf("socks5: fragmented or malformed UDP packet")
	}
	br := bufio.NewReader(bytes.NewReader(append([]byte{socks5Version, socksCmdUDP, 0}, packet[3:]...)))
	_, target, err := socksReadRequest(br)
	if err != nil {
		return "", nil, err
	}
	payload, err := io.ReadAll(br)
	return target, payload, err
}

func encodeSOCKS5UDP(target string, payload []byte) ([]byte, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("socks5: invalid UDP port")
	}
	buf := []byte{0, 0, 0}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			buf = append(buf, socksAtypIPv4)
			buf = append(buf, v4...)
		} else {
			buf = append(buf, socksAtypIPv6)
			buf = append(buf, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return nil, fmt.Errorf("socks5: UDP hostname too long")
		}
		buf = append(buf, socksAtypDomain, byte(len(host)))
		buf = append(buf, host...)
	}
	buf = append(buf, byte(port>>8), byte(port))
	return append(buf, payload...), nil
}
