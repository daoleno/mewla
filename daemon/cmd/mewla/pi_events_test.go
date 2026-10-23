package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

const piSession = "/state/provider-sessions/pi/one.jsonl"

type piExtension struct {
	conn   net.Conn
	reader *bufio.Reader
}

func startPi(t *testing.T) (*piEvents, *agentevents.Hub, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi-ev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := agentevents.NewHub()
	events := newPiEvents(ctx, hub, func() []*classifier.Worker {
		return []*classifier.Worker{{ID: "%4", Command: "pi --session " + piSession + " -e /state/pi-extension.js"}, {ID: "%5", Command: "pi --session /state/provider-sessions/pi/two.jsonl"}}
	})
	events.ackWait = 300 * time.Millisecond
	socket := filepath.Join(dir, "pi.sock")
	if err := events.Listen(socket); err != nil {
		t.Fatal(err)
	}
	return events, hub, socket
}

func (e *piExtension) say(t *testing.T, v any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	if _, err := e.conn.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (e *piExtension) next(t *testing.T) map[string]any {
	t.Helper()
	_ = e.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := e.reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(line, &m)
	return m
}

func connectPi(t *testing.T, socket, session string) *piExtension {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	e := &piExtension{conn: conn, reader: bufio.NewReader(conn)}
	e.say(t, map[string]any{"t": "hello", "reason": "startup", "session": session, "id": "uuid-1"})
	return e
}

func TestPiExtensionOfAnUnknownSessionIsHungUpOn(t *testing.T) {
	_, hub, socket := startPi(t)
	e := connectPi(t, socket, "/somewhere/else.jsonl")
	_ = e.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := e.reader.ReadByte(); err == nil {
		t.Fatal("expected the connection to be closed")
	}
	if len(hub.Workers()) != 0 {
		t.Fatalf("hub learned of %v", hub.Workers())
	}
}

func TestPiWorkerIsBoundByItsLaunchSessionAndFollowsItsTurns(t *testing.T) {
	_, hub, socket := startPi(t)
	e := connectPi(t, socket, piSession)
	waitFor(t, "the Worker to connect", func() bool { s, ok := hub.State("%4"); return ok && s.Connected && s.Bound })
	s, _ := hub.State("%4")
	if s.Binding.ConversationID != "uuid-1" || s.Binding.Ref != piSession || s.Binding.By != "launch" {
		t.Fatalf("binding = %+v", s.Binding)
	}
	e.say(t, map[string]any{"t": "input", "source": "interactive", "sha": "d1"})
	e.say(t, map[string]any{"t": "turn_start", "turn": "uuid-1:1"})
	waitFor(t, "the turn", func() bool {
		s, _ := hub.State("%4")
		return s.Current != nil && s.Current.Status == agentevents.TurnRunning
	})
	s, _ = hub.State("%4")
	if s.Admission == nil || s.Admission.InputSHA256 != "d1" {
		t.Fatalf("admission = %+v", s.Admission)
	}
	e.say(t, map[string]any{"t": "turn_end", "turn": "uuid-1:1", "aborted": false, "stop": "stop"})
	waitFor(t, "the end", func() bool { s, _ := hub.State("%4"); return s.Current.Status == agentevents.TurnCompleted })
	e.say(t, map[string]any{"t": "bye"})
	waitFor(t, "the exit", func() bool { s, _ := hub.State("%4"); return s.Exited })
}

func TestPiSendReachesTheExtensionAndWaitsForItsAcknowledgement(t *testing.T) {
	events, hub, socket := startPi(t)
	if handled, err := events.send("%4", "hi", "r0", func() error { t.Fatal("begin ran with no extension"); return nil }); handled || err != nil {
		t.Fatalf("send with no extension = (%v, %v)", handled, err)
	}
	e := connectPi(t, socket, piSession)
	waitFor(t, "connect", func() bool { s, ok := hub.State("%4"); return ok && s.Connected })

	go func() {
		m := e.next(t)
		if m["t"] != "send" || m["text"] != "do it" {
			t.Errorf("extension got %v", m)
		}
		e.say(t, map[string]any{"t": "sent", "id": m["id"]})
	}()
	began := false
	if handled, err := events.send("%4", "do it", "mewla-1", func() error { began = true; return nil }); !handled || err != nil || !began {
		t.Fatalf("send = (%v, %v), began %v", handled, err, began)
	}

	// An extension that refuses, and one that stays silent.
	go func() {
		m := e.next(t)
		e.say(t, map[string]any{"t": "send_failed", "id": m["id"], "error": "pi is shutting down"})
	}()
	_, err := events.send("%4", "x", "mewla-2", func() error { return nil })
	if in, ok := err.(*watcher.StructuredInputError); !ok || !in.NotSent {
		t.Fatalf("a refused message must be NotSent, got %v", err)
	}
	go func() { e.next(t) }()
	_, err = events.send("%4", "x", "mewla-3", func() error { return nil })
	if in, ok := err.(*watcher.StructuredInputError); !ok || in.NotSent {
		t.Fatalf("an unacknowledged message is ambiguous, got %v", err)
	}
}

func TestPiInterruptSendsAnAbortOnlyToARunningTurn(t *testing.T) {
	events, hub, socket := startPi(t)
	e := connectPi(t, socket, piSession)
	waitFor(t, "connect", func() bool { s, ok := hub.State("%4"); return ok && s.Connected })
	if handled, err := events.interrupt("%4"); !handled || err != nil {
		t.Fatalf("idle interrupt = (%v, %v)", handled, err)
	}
	e.say(t, map[string]any{"t": "turn_start", "turn": "uuid-1:1"})
	waitFor(t, "the turn", func() bool {
		s, _ := hub.State("%4")
		return s.Current != nil && s.Current.Status == agentevents.TurnRunning
	})
	if handled, err := events.interrupt("%4"); !handled || err != nil {
		t.Fatalf("interrupt = (%v, %v)", handled, err)
	}
	if m := e.next(t); m["t"] != "abort" {
		t.Fatalf("extension got %v", m)
	}
	if handled, _ := events.interrupt("%5"); handled {
		t.Fatal("a Worker with no extension was handled")
	}
}

func TestPiNewSessionOnTheSameConnectionRebinds(t *testing.T) {
	_, hub, socket := startPi(t)
	e := connectPi(t, socket, piSession)
	waitFor(t, "connect", func() bool { s, ok := hub.State("%4"); return ok && s.Bound })
	e.say(t, map[string]any{"t": "hello", "reason": "new", "session": "/pi/home/new.jsonl", "id": "uuid-2"})
	waitFor(t, "the new binding", func() bool { s, _ := hub.State("%4"); return s.Binding.ConversationID == "uuid-2" })
	if s, _ := hub.State("%4"); s.Binding.By != "agent" {
		t.Fatalf("binding = %+v", s.Binding)
	}
}
