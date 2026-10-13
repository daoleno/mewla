package server

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/gorilla/websocket"
)

// startNoAuthRFB is a loopback RFB 3.8 server with no sign-in that echoes
// every byte after ServerInit.
func startNoAuthRFB(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.WriteString(conn, "RFB 003.008\n")
				buf := make([]byte, 12)
				if _, err := io.ReadFull(conn, buf); err != nil {
					return
				}
				_, _ = conn.Write([]byte{1, 1})
				if _, err := io.ReadFull(conn, buf[:1]); err != nil {
					return
				}
				_, _ = conn.Write([]byte{0, 0, 0, 0})
				if _, err := io.ReadFull(conn, buf[:1]); err != nil {
					return
				}
				init := make([]byte, 24)
				binary.BigEndian.PutUint16(init[0:], 800)
				binary.BigEndian.PutUint16(init[2:], 600)
				_, _ = conn.Write(init) // empty name
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

type screenViewerClient struct {
	t    *testing.T
	conn *websocket.Conn
	buf  []byte
}

// read returns the next n RFB bytes, skipping JSON control messages.
func (c *screenViewerClient) read(n int) []byte {
	c.t.Helper()
	for len(c.buf) < n {
		_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		kind, data, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("read RFB: %v", err)
		}
		if kind == websocket.BinaryMessage {
			c.buf = append(c.buf, data...)
		}
	}
	out := c.buf[:n]
	c.buf = c.buf[n:]
	return out
}

func (c *screenViewerClient) write(data []byte) {
	c.t.Helper()
	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		c.t.Fatal(err)
	}
}

func TestScreenViewerRequiresDeviceAuthAndBridgesRFB(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	stateDir := t.TempDir()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(screen.NewService(stateDir))
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	wsURL := "ws" + strings.TrimPrefix(h.URL, "http") + "/screen/viewer"
	dial := func(purpose string) (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		if purpose != "" {
			header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		}
		return websocket.DefaultDialer.Dial(wsURL, header)
	}
	for _, purpose := range []string{"", "mewla-connect", "mewla-screen"} {
		if c, resp, err := dial(purpose); err == nil {
			c.Close()
			t.Fatalf("purpose %q opened the screen", purpose)
		} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("purpose %q: %v", purpose, err)
		}
	}

	// Off by default: the viewer is told why and closed.
	c, _, err := dial("mewla-screen-view")
	if err != nil {
		t.Fatal(err)
	}
	var msg struct{ Type, Error string }
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.ReadJSON(&msg); err != nil || msg.Type != "error" || !strings.Contains(msg.Error, "mewla screen enable") {
		t.Fatalf("disabled: %+v %v", msg, err)
	}
	c.Close()

	// Status needs its own signed purpose.
	statusReq, _ := http.NewRequest(http.MethodGet, h.URL+"/screen", nil)
	if resp, err := http.DefaultClient.Do(statusReq); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned status: %v %v", resp, err)
	}

	if err := screen.SaveConfig(stateDir, screen.Config{Enabled: true, Address: startNoAuthRFB(t)}); err != nil {
		t.Fatal(err)
	}
	statusReq, _ = http.NewRequest(http.MethodGet, h.URL+"/screen", nil)
	statusReq.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen"))
	resp, err := http.DefaultClient.Do(statusReq)
	if err != nil {
		t.Fatal(err)
	}
	var status screen.Status
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if !status.Available || status.Width != 800 || status.Height != 600 {
		t.Fatalf("status = %+v", status)
	}

	open := func() *screenViewerClient {
		conn, _, err := dial("mewla-screen-view")
		if err != nil {
			t.Fatal(err)
		}
		v := &screenViewerClient{t: t, conn: conn}
		if got := string(v.read(12)); got != "RFB 003.008\n" {
			t.Fatalf("version %q", got)
		}
		v.write([]byte("RFB 003.008\n"))
		if got := v.read(2); got[0] != 1 || got[1] != 1 {
			t.Fatalf("viewer was offered %v, want only None", got)
		}
		v.write([]byte{1})
		if got := v.read(4); binary.BigEndian.Uint32(got) != 0 {
			t.Fatalf("security result %v", got)
		}
		v.write([]byte{1}) // ClientInit
		init := v.read(24)
		if binary.BigEndian.Uint16(init[0:]) != 800 {
			t.Fatalf("ServerInit %v", init)
		}
		return v
	}
	first := open()
	first.write([]byte("hello"))
	if got := string(first.read(5)); got != "hello" {
		t.Fatalf("echo %q", got)
	}
	if err := first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","t":42}`)); err != nil {
		t.Fatal(err)
	}
	var pong struct {
		Type string
		T    int
	}
	for pong.Type != "pong" {
		_ = first.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := first.conn.ReadJSON(&pong); err != nil {
			t.Fatal(err)
		}
	}
	if pong.T != 42 {
		t.Fatalf("pong %+v", pong)
	}

	// A second viewer takes over; the first is closed with the reason.
	second := open()
	defer second.conn.Close()
	_ = first.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := first.conn.ReadMessage()
		if err == nil {
			continue
		}
		if !websocket.IsCloseError(err, websocket.CloseNormalClosure) || !strings.Contains(err.Error(), "took over") {
			t.Fatalf("first viewer close: %v", err)
		}
		break
	}

	// Turning screen control off ends the open session.
	if err := screen.SaveConfig(stateDir, screen.Config{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	second.write([]byte("x"))
	_ = second.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		kind, data, err := second.conn.ReadMessage()
		if err != nil {
			t.Fatal("closed without saying screen control is off")
		}
		if kind == websocket.TextMessage && strings.Contains(string(data), "mewla screen enable") {
			break
		}
	}
}
