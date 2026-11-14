package server

import (
	"bufio"
	"crypto/tls"
	"net"
	"time"
)

type peekedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c peekedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// NetConn is the accepted connection under the peek, as tls.Conn's NetConn is
// under TLS (keepBacklogInOutbox reaches the TCP socket through both).
func (c peekedConn) NetConn() net.Conn {
	return c.Conn
}

type tlsHTTPListener struct {
	net.Listener
	config *tls.Config
}

func (l *tlsHTTPListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		first, err := reader.Peek(1)
		if err != nil {
			// The failure belongs to this connection. net/http stops the
			// listener when Accept returns a non-temporary error.
			_ = conn.Close()
			continue
		}
		inner := peekedConn{Conn: conn, reader: reader}
		_ = conn.SetDeadline(time.Time{})
		if first[0] == 0x16 {
			return tls.Server(inner, l.config), nil
		}
		return inner, nil
	}
}
