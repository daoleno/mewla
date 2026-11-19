package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/gorilla/websocket"
)

// stopAppServer is a Codex app-server double for Stop: it answers the
// handshake, turn/interrupt and the background-terminal calls, and records
// every method it was asked.
type stopAppServer struct {
	mu        sync.Mutex
	methods   []string
	params    []map[string]any
	terminals []codexctl.BackgroundTerminal
	// pageSize splits the list into pages of that many terminals.
	pageSize int
	// keep names processes terminate answers terminated=false for.
	keep map[string]bool
}

func startStopAppServer(t *testing.T, terminals []codexctl.BackgroundTerminal, pageSize int) (*stopAppServer, string) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "cz-stop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "codex-ctl.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	f := &stopAppServer{terminals: terminals, pageSize: pageSize}
	upgrader := websocket.Upgrader{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			if len(req.ID) == 0 {
				continue
			}
			result := f.answer(req.Method, req.Params)
			_ = ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	return f, socket
}

func (f *stopAppServer) answer(method string, params map[string]any) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods = append(f.methods, method)
	f.params = append(f.params, params)
	switch method {
	case "thread/backgroundTerminals/list":
		start := 0
		if cursor, ok := params["cursor"].(string); ok {
			for i, terminal := range f.terminals {
				if terminal.ProcessID == cursor {
					start = i + 1
				}
			}
		}
		end := len(f.terminals)
		if f.pageSize > 0 && start+f.pageSize < end {
			end = start + f.pageSize
		}
		page := map[string]any{"data": f.terminals[start:end], "nextCursor": nil}
		if end < len(f.terminals) {
			page["nextCursor"] = f.terminals[end-1].ProcessID
		}
		return page
	case "thread/backgroundTerminals/terminate":
		for i, terminal := range f.terminals {
			if terminal.ProcessID == params["processId"] && !f.keep[terminal.ProcessID] {
				f.terminals = append(f.terminals[:i:i], f.terminals[i+1:]...)
				return map[string]any{"terminated": true}
			}
		}
		return map[string]any{"terminated": false}
	}
	return map[string]any{}
}

func (f *stopAppServer) calls() ([]string, []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.methods...), append([]map[string]any(nil), f.params...)
}

func commandItem(turnID, itemID, source, command string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"threadId": "t-1", "turnId": turnID,
		"item": map[string]any{"type": "commandExecution", "id": itemID, "source": source, "command": command, "status": "inProgress"},
	})
	return raw
}

func TestCodexStopTerminatesOnlyTheInterruptedTurnsBackgroundTerminal(t *testing.T) {
	// Turn A left `sleep 300` running; turn B started the count. The list
	// pages one terminal at a time, so the count is on the second page.
	server, socket := startStopAppServer(t, []codexctl.BackgroundTerminal{
		{ItemID: "a1", ProcessID: "100", Command: "sleep 300"},
		{ItemID: "b1", ProcessID: "200", Command: "for i in $(seq 1 30); do echo $i; sleep 1; done"},
	}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := codexctl.Open(ctx, socket, codexctl.DialOptions{ClientName: "test", DialTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	hub := agentevents.NewHub()
	var reports []agentevents.Event
	hub.OnCommandsStopped(func(e agentevents.Event) { reports = append(reports, e) })
	events := newCodexEvents(ctx, hub)
	session := &codexSession{workerID: "%1", client: client, thread: "t-1"}
	events.sessions["%1"] = session
	session.noteCommand("item/started", commandItem("turn-a", "a1", "unifiedExecStartup", "/bin/zsh -lc 'sleep 300'"))
	session.noteCommand("item/started", commandItem("turn-b", "b0", "agent", "/bin/zsh -lc 'ls'"))
	session.noteCommand("item/completed", commandItem("turn-b", "b0", "agent", "/bin/zsh -lc 'ls'"))
	session.noteCommand("item/started", commandItem("turn-b", "b1", "unifiedExecStartup", "/bin/zsh -lc 'for i in $(seq 1 30); do echo $i; sleep 1; done'"))
	// Polling turn A's terminal from turn B is still turn A's command.
	session.noteCommand("item/started", commandItem("turn-b", "a1-poll", "unifiedExecInteraction", ""))
	hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindTurnStarted, TurnID: "turn-b", Status: agentevents.TurnRunning, At: time.Now()})

	if handled, err := events.interrupt("%1"); !handled || err != nil {
		t.Fatalf("interrupt = (%v, %v), want handled", handled, err)
	}
	methods, params := server.calls()
	want := []string{"initialize", "turn/interrupt", "thread/backgroundTerminals/list", "thread/backgroundTerminals/list", "thread/backgroundTerminals/terminate"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	if got := params[1]["turnId"]; got != "turn-b" {
		t.Fatalf("turn/interrupt turnId = %v, want turn-b", got)
	}
	if got := params[4]["processId"]; got != "200" {
		t.Fatalf("terminated processId = %v, want only turn B's 200", got)
	}
	if len(reports) != 1 {
		t.Fatalf("commands_stopped events = %d, want 1", len(reports))
	}
	report := reports[0]
	wantStopped := []agentevents.StoppedCommand{{Command: "for i in $(seq 1 30); do echo $i; sleep 1; done", Ended: true}}
	if report.TurnID != "turn-b" || !reflect.DeepEqual(report.Stopped, wantStopped) {
		t.Fatalf("report = turn %q stopped %+v, want turn-b %+v", report.TurnID, report.Stopped, wantStopped)
	}
	state, _ := hub.State("%1")
	if state.Current == nil || state.Current.Status != agentevents.TurnRunning {
		t.Fatalf("the report moved the turn: %+v", state.Current)
	}
}

func TestCodexStopOfATurnWithNoOpenCommandListsNothing(t *testing.T) {
	server, socket := startStopAppServer(t, []codexctl.BackgroundTerminal{{ItemID: "a1", ProcessID: "100", Command: "sleep 300"}}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := codexctl.Open(ctx, socket, codexctl.DialOptions{ClientName: "test", DialTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hub := agentevents.NewHub()
	reported := false
	hub.OnCommandsStopped(func(agentevents.Event) { reported = true })
	events := newCodexEvents(ctx, hub)
	session := &codexSession{workerID: "%1", client: client, thread: "t-1"}
	events.sessions["%1"] = session
	session.noteCommand("item/started", commandItem("turn-a", "a1", "unifiedExecStartup", "sleep 300"))

	// Idle: nothing is interrupted or terminated.
	if handled, err := events.interrupt("%1"); !handled || err != nil {
		t.Fatalf("idle interrupt = (%v, %v)", handled, err)
	}
	if methods, _ := server.calls(); !reflect.DeepEqual(methods, []string{"initialize"}) {
		t.Fatalf("idle Codex was asked %v", methods)
	}
	// A running turn that started no command: interrupt only.
	hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindTurnStarted, TurnID: "turn-b", Status: agentevents.TurnRunning, At: time.Now()})
	if handled, err := events.interrupt("%1"); !handled || err != nil {
		t.Fatalf("interrupt = (%v, %v)", handled, err)
	}
	if methods, _ := server.calls(); !reflect.DeepEqual(methods, []string{"initialize", "turn/interrupt"}) {
		t.Fatalf("a turn with no command was asked %v", methods)
	}
	if reported {
		t.Fatal("a Stop that ended nothing reported a line")
	}
}

func TestCodexStopReportsATerminalTheServerDidNotEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The list names b1, but terminate answers terminated=false (it went
	// away in between): the line says it could not be stopped.
	server, socket := startStopAppServer(t, []codexctl.BackgroundTerminal{{ItemID: "b1", ProcessID: "200", Command: "make serve"}}, 0)
	server.keep = map[string]bool{"200": true}
	client, err := codexctl.Open(ctx, socket, codexctl.DialOptions{ClientName: "test", DialTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	stopped := stopCodexTurnCommands(ctx, client, "t-1", []codexCommand{{itemID: "b1", command: "/bin/zsh -lc 'make serve'"}})
	want := []agentevents.StoppedCommand{{Command: "make serve", Reason: "Codex no longer had it"}}
	if !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stopped = %+v, want %+v", stopped, want)
	}
	methods, _ := server.calls()
	for _, method := range methods {
		if method == "thread/backgroundTerminals/clean" {
			t.Fatal("Stop called clean")
		}
	}
}
