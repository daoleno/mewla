package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/gorilla/websocket"
)

// fakeCodexThread is a Codex app-server with one thread: it lists two
// models, reports the thread's settings, and takes thread/settings/update
// the way Codex does (an acknowledgement to every attached connection).
type fakeCodexThread struct {
	socket  string
	mu      sync.Mutex
	write   sync.Mutex
	conns   []*websocket.Conn
	model   string
	effort  string
	updates []string
}

func startFakeCodexThread(t *testing.T) *fakeCodexThread {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "cxm-") // a short path: sockets cap it
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	f := &fakeCodexThread{socket: filepath.Join(dir, "ctl.sock"), model: "gpt-6-luna", effort: "medium"}
	ln, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	upgrader := websocket.Upgrader{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, ws)
		f.mu.Unlock()
		f.serve(ws)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })
	return f
}

func (f *fakeCodexThread) send(ws *websocket.Conn, message map[string]any) {
	raw, _ := json.Marshal(message)
	f.write.Lock()
	defer f.write.Unlock()
	_ = ws.WriteMessage(websocket.TextMessage, raw)
}

func (f *fakeCodexThread) serve(ws *websocket.Conn) {
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var call struct {
			ID     uint64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(raw, &call) != nil || call.Method == "" {
			continue
		}
		var result any = map[string]any{}
		switch call.Method {
		case "model/list":
			efforts := []map[string]any{{"reasoningEffort": "low"}, {"reasoningEffort": "medium"}, {"reasoningEffort": "high"}}
			result = map[string]any{"data": []map[string]any{
				{"model": "gpt-6-luna", "displayName": "Luna", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": efforts},
				{"model": "gpt-6-sol", "displayName": "Sol", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": efforts},
			}}
		case "thread/read":
			f.mu.Lock()
			result = map[string]any{"thread": map[string]any{"id": "t-1", "cwd": "/work", "model": f.model, "reasoningEffort": f.effort, "status": map[string]any{"type": "idle"}}}
			f.mu.Unlock()
		case "thread/resume":
			result = map[string]any{"thread": map[string]any{"id": "t-1"}}
		case "thread/settings/update":
			model, _ := call.Params["model"].(string)
			effort, _ := call.Params["effort"].(string)
			f.mu.Lock()
			f.model, f.effort = model, effort
			f.updates = append(f.updates, model+"/"+effort)
			conns := append([]*websocket.Conn(nil), f.conns...)
			f.mu.Unlock()
			f.send(ws, map[string]any{"id": call.ID, "result": map[string]any{}})
			for _, conn := range conns {
				f.send(conn, map[string]any{"method": "thread/settings/updated", "params": map[string]any{
					"threadId": "t-1", "threadSettings": map[string]any{"model": model, "effort": effort}}})
			}
			continue
		}
		f.send(ws, map[string]any{"id": call.ID, "result": result})
	}
}

func (f *fakeCodexThread) applied() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.updates...)
}

// codexModelsForTest is a codexEvents with one connected Session on the fake.
func codexModelsForTest(t *testing.T, f *fakeCodexThread, hub *agentevents.Hub) (*codexEvents, *classifier.Worker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := codexctl.Open(ctx, f.socket, codexctl.DialOptions{ClientName: "test-events"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	events := newCodexEvents(context.Background(), hub)
	events.later.poll = 10 * time.Millisecond
	events.sessions["w1"] = &codexSession{owner: events, workerID: "w1", socket: f.socket, cwd: "/work", client: client, thread: "t-1"}
	return events, &classifier.Worker{ID: "w1", Command: "codex", Cwd: "/work"}
}

// A model picked while Codex works is held, not set on the thread: Codex
// reads a thread's settings as a turn starts, so set mid-turn it was
// acknowledged while the running turn went on on the old model (0.4.30
// gate). It is set when the turn ends, and the menu says so until then.
func TestCodexSwitchWhileWorkingIsMadeWhenTheTurnEnds(t *testing.T) {
	f := startFakeCodexThread(t)
	hub := agentevents.NewHub()
	models, worker := codexModelsForTest(t, f, hub)
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "turn-1", At: time.Now()})
	after, err := models.SetModel(context.Background(), worker, "gpt-6-luna", "low")
	if err != nil {
		t.Fatalf("a switch while Codex works was refused: %v", err)
	}
	if after.Pending == nil || after.Pending.Model != "gpt-6-luna" || after.Pending.Effort != "low" {
		t.Fatalf("pending = %#v, want the pick held", after.Pending)
	}
	if after.Current != "gpt-6-luna" || after.Effort != "medium" {
		t.Fatalf("while held the menu reads %s/%s, want what the turn runs on (medium)", after.Current, after.Effort)
	}
	if got := f.applied(); len(got) != 0 {
		t.Fatalf("the thread's settings changed mid-turn: %v", got)
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnFinished, TurnID: "turn-1", Status: agentevents.TurnCompleted, At: time.Now()})
	deadline := time.Now().Add(5 * time.Second)
	for models.later.of("w1") != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := f.applied(); len(got) != 1 || got[0] != "gpt-6-luna/low" {
		t.Fatalf("after the turn the thread got %v, want gpt-6-luna/low", got)
	}
	now, err := models.Models(context.Background(), worker)
	if err != nil {
		t.Fatal(err)
	}
	if now.Pending != nil || now.Failed != nil || now.Effort != "low" {
		t.Fatalf("after the turn: pending %#v failed %#v effort %q", now.Pending, now.Failed, now.Effort)
	}
}

// A message sent right after the turn ends runs on the pick held during it:
// the send settles it first instead of racing the background wait.
func TestCodexHeldPickIsMadeBeforeTheNextMessage(t *testing.T) {
	f := startFakeCodexThread(t)
	hub := agentevents.NewHub()
	models, worker := codexModelsForTest(t, f, hub)
	models.later.poll = time.Hour // the background wait never gets there first
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "turn-1", At: time.Now()})
	if _, err := models.SetModel(context.Background(), worker, "gpt-6-sol", ""); err != nil {
		t.Fatal(err)
	}
	if err := models.Settle(context.Background(), worker); err != nil || len(f.applied()) != 0 {
		t.Fatalf("settle while the turn runs: %v, applied %v", err, f.applied())
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnFinished, TurnID: "turn-1", Status: agentevents.TurnCompleted, At: time.Now()})
	if err := models.Settle(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	if got := f.applied(); len(got) != 1 || got[0] != "gpt-6-sol/none" {
		t.Fatalf("settled pick = %v, want gpt-6-sol at its default effort", got)
	}
	if models.later.of("w1") != nil {
		t.Fatal("the pick stayed pending after the settle made it")
	}
}

// An idle Codex takes the pick at once; one it does not list is refused at
// once, also while it works, instead of failing when the turn ends.
func TestCodexSwitchIdleAppliesAndUnlistedIsRefused(t *testing.T) {
	f := startFakeCodexThread(t)
	hub := agentevents.NewHub()
	models, worker := codexModelsForTest(t, f, hub)
	after, err := models.SetModel(context.Background(), worker, "gpt-6-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != nil || after.Current != "gpt-6-sol" || after.Effort != "high" {
		t.Fatalf("idle switch: %#v", after)
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "turn-1", At: time.Now()})
	if _, err := models.SetModel(context.Background(), worker, "gpt-9-nova", ""); err == nil {
		t.Fatal("a model Codex does not list was taken")
	}
	if _, err := models.SetModel(context.Background(), worker, "gpt-6-luna", "ultra"); err == nil {
		t.Fatal("an effort the model does not take was taken")
	}
	if models.later.of("w1") != nil || len(f.applied()) != 1 {
		t.Fatalf("a refused pick was held or applied: pending %#v applied %v", models.later.of("w1"), f.applied())
	}
}
