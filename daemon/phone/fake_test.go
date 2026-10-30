package phone

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// fakeConn is the daemon's end of a phone that answers through handle, synchronously.
type fakeConn struct {
	mu     sync.Mutex
	link   *Link
	sent   []Request
	closed []closeRecord
	// handle answers a request; nil means the phone says nothing.
	handle func(Request) *Result
}

type closeRecord struct {
	code   int
	reason string
}

func (c *fakeConn) Send(frame []byte) error {
	var req Request
	if err := json.Unmarshal(frame, &req); err != nil {
		return err
	}
	c.mu.Lock()
	c.sent = append(c.sent, req)
	handle, link := c.handle, c.link
	c.mu.Unlock()
	if handle == nil {
		return nil
	}
	if res := handle(req); res != nil {
		res.Type, res.ID = TypeResult, req.ID
		raw, _ := json.Marshal(res)
		return link.Receive(raw)
	}
	return nil
}

func (c *fakeConn) Close(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, closeRecord{code, reason})
}

func (c *fakeConn) requests() []Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Request(nil), c.sent...)
}

func (c *fakeConn) ops() []string {
	var ops []string
	for _, r := range c.requests() {
		ops = append(ops, r.Op)
	}
	return ops
}

func (c *fakeConn) closes() []closeRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]closeRecord(nil), c.closed...)
}

func hello(state string) Hello {
	return Hello{Type: TypeHello, Version: Version, Model: "Pixel 8", SDK: 35, AppVersion: "1.0", State: state}
}

// connect attaches a fake phone in state to m.
func connect(t *testing.T, m *Manager, state string, handle func(Request) *Result) (*Link, *fakeConn) {
	t.Helper()
	conn := &fakeConn{handle: handle}
	link, err := m.Attach(hello(state), conn)
	if err != nil {
		t.Fatal(err)
	}
	conn.mu.Lock()
	conn.link = link
	conn.mu.Unlock()
	return link, conn
}

func sendFrame(t *testing.T, l *Link, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Receive(raw); err != nil {
		t.Fatal(err)
	}
}

func okResult(screen *Screen) func(Request) *Result {
	return func(Request) *Result { return &Result{OK: true, Screen: screen} }
}

// fakeClock is time the tests move.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

// Advance moves time forward and runs the timers that came due, on this goroutine.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
