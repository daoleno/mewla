// Package proxyproto carries a client's address across the relay's SNI
// passthrough with the PROXY protocol, version 1 (the text form):
// https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt
//
// The relay writes one header line before the client's bytes; a service
// behind it reads the line and reports that address as the connection's
// RemoteAddr. Only listen with it where the relay is the only possible
// client: whoever can connect can claim any address.
package proxyproto

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxHeader is the longest v1 header, CRLF included.
const maxHeader = 107

// Header returns the v1 header for a connection from source to destination.
// Addresses that are not both TCP over the same IP family give "UNKNOWN",
// which tells the receiver to keep the connection's own address.
func Header(source, destination net.Addr) []byte {
	src, srcOK := source.(*net.TCPAddr)
	dst, dstOK := destination.(*net.TCPAddr)
	if !srcOK || !dstOK {
		return []byte("PROXY UNKNOWN\r\n")
	}
	srcIP, dstIP := src.IP, dst.IP
	family := "TCP6"
	if src4, dst4 := srcIP.To4(), dstIP.To4(); src4 != nil && dst4 != nil {
		family, srcIP, dstIP = "TCP4", src4, dst4
	} else if src4 != nil || dst4 != nil || srcIP.To16() == nil || dstIP.To16() == nil {
		return []byte("PROXY UNKNOWN\r\n")
	}
	return fmt.Appendf(nil, "PROXY %s %s %s %d %d\r\n", family, srcIP, dstIP, src.Port, dst.Port)
}

// parse returns the source address of a v1 header line without CRLF, or
// nil for UNKNOWN.
func parse(line string) (net.Addr, error) {
	fields := strings.Split(line, " ")
	if len(fields) < 2 || fields[0] != "PROXY" {
		return nil, errors.New("proxy protocol: not a v1 header")
	}
	if fields[1] == "UNKNOWN" {
		return nil, nil
	}
	if len(fields) != 6 || fields[1] != "TCP4" && fields[1] != "TCP6" {
		return nil, errors.New("proxy protocol: malformed header")
	}
	ip := net.ParseIP(fields[2])
	port, err := strconv.ParseUint(fields[4], 10, 16)
	if ip == nil || net.ParseIP(fields[3]) == nil || err != nil ||
		(ip.To4() != nil) != (fields[1] == "TCP4") {
		return nil, errors.New("proxy protocol: malformed header")
	}
	return &net.TCPAddr{IP: ip, Port: int(port)}, nil
}

// Listener requires a v1 header on every accepted connection. Accept does
// not wait for it; the first Read or RemoteAddr does, for at most Timeout.
type Listener struct {
	net.Listener
	Timeout time.Duration
}

func (l *Listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Conn{Conn: conn, timeout: timeout}, nil
}

// Conn is a connection whose RemoteAddr comes from its PROXY header.
type Conn struct {
	net.Conn
	timeout time.Duration
	once    sync.Once
	reader  *bufio.Reader
	remote  net.Addr
	err     error
}

func (c *Conn) readHeader() {
	c.once.Do(func() {
		_ = c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
		defer c.Conn.SetReadDeadline(time.Time{})
		c.reader = bufio.NewReaderSize(c.Conn, 512)
		line := make([]byte, 0, maxHeader)
		for {
			b, err := c.reader.ReadByte()
			if err != nil {
				c.err = fmt.Errorf("proxy protocol: %w", err)
				return
			}
			line = append(line, b)
			if b == '\n' {
				break
			}
			if len(line) == maxHeader {
				c.err = errors.New("proxy protocol: header too long")
				return
			}
		}
		text, ok := strings.CutSuffix(string(line), "\r\n")
		if !ok {
			c.err = errors.New("proxy protocol: header must end in CRLF")
			return
		}
		c.remote, c.err = parse(text)
	})
}

func (c *Conn) Read(p []byte) (int, error) {
	c.readHeader()
	if c.err != nil {
		return 0, c.err
	}
	return c.reader.Read(p)
}

// RemoteAddr is the client from the header, or the connection's own peer
// when the header said UNKNOWN or could not be read (Read then fails).
func (c *Conn) RemoteAddr() net.Addr {
	c.readHeader()
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}
