package proxyproto

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestHeader(t *testing.T) {
	for _, test := range []struct {
		source, destination net.Addr
		want                string
	}{
		{
			&net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51000},
			&net.TCPAddr{IP: net.ParseIP("172.18.0.6"), Port: 443},
			"PROXY TCP4 203.0.113.7 172.18.0.6 51000 443\r\n",
		},
		{
			// IPv4-mapped addresses are IPv4.
			&net.TCPAddr{IP: net.ParseIP("::ffff:203.0.113.7"), Port: 1},
			&net.TCPAddr{IP: net.ParseIP("::ffff:172.18.0.6"), Port: 2},
			"PROXY TCP4 203.0.113.7 172.18.0.6 1 2\r\n",
		},
		{
			&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1},
			&net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443},
			"PROXY TCP6 2001:db8::1 2001:db8::2 1 443\r\n",
		},
		{
			&net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 1},
			&net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443},
			"PROXY UNKNOWN\r\n",
		},
		{&net.UnixAddr{Name: "a"}, &net.UnixAddr{Name: "b"}, "PROXY UNKNOWN\r\n"},
	} {
		if got := string(Header(test.source, test.destination)); got != test.want {
			t.Errorf("Header(%v, %v) = %q, want %q", test.source, test.destination, got, test.want)
		}
	}
}

func listen(t *testing.T) *Listener {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	return &Listener{Listener: raw, Timeout: time.Second}
}

func exchange(t *testing.T, listener *Listener, sent string) (net.Addr, string, error) {
	t.Helper()
	go func() {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(sent))
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	remote := conn.RemoteAddr()
	body, err := io.ReadAll(conn)
	return remote, string(body), err
}

func TestListenerReportsTheHeaderAddress(t *testing.T) {
	listener := listen(t)
	remote, body, err := exchange(t, listener, "PROXY TCP4 203.0.113.7 172.18.0.6 51000 443\r\nhello")
	if err != nil || body != "hello" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if remote.String() != "203.0.113.7:51000" {
		t.Fatalf("remote = %v", remote)
	}

	remote, body, err = exchange(t, listener, "PROXY TCP6 2001:db8::1 2001:db8::2 7 443\r\nx")
	if err != nil || body != "x" || remote.String() != "[2001:db8::1]:7" {
		t.Fatalf("v6 remote=%v body=%q err=%v", remote, body, err)
	}

	remote, body, err = exchange(t, listener, "PROXY UNKNOWN\r\ny")
	if err != nil || body != "y" || remote.(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Fatalf("unknown remote=%v body=%q err=%v", remote, body, err)
	}
}

func TestListenerRejectsConnectionsWithoutAHeader(t *testing.T) {
	listener := listen(t)
	for _, sent := range []string{
		"\x16\x03\x01\x00\x05hello\n",
		"PROXY TCP4 203.0.113.7 172.18.0.6 51000 443\n",
		"PROXY TCP4 2001:db8::1 172.18.0.6 1 2\r\n",
		"PROXY TCP4 203.0.113.7 172.18.0.6 70000 443\r\n",
		"PROXY TCP4 " + string(make([]byte, 120)) + "\r\n",
	} {
		if _, body, err := exchange(t, listener, sent); err == nil {
			t.Errorf("%q: read %q, want an error", sent, body)
		}
	}
}
