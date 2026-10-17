package work

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeDSHNative records `$events/result` posts and serves one `$events` stream.
type fakeDSHNative struct {
	mu      sync.Mutex
	results []map[string]any
	frames  chan string
}

func newFakeDSHNative(t *testing.T) (*fakeDSHNative, *httptest.Server) {
	fake := &fakeDSHNative{frames: make(chan string, 16)}
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "dsh-auth-x=signed" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/api/remote.mux":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			var open struct {
				Type, StreamID, Endpoint string
				Payload                  map[string]any
			}
			if conn.ReadJSON(&open) != nil || open.Type != "open" || open.Endpoint != "$events" {
				t.Error("events stream not opened")
				return
			}
			for frame := range fake.frames {
				if conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"item","streamId":"`+open.StreamID+`","value":`+frame+`}`)) != nil {
					return
				}
			}
		case "/api/$events/result":
			var body struct {
				Type    string `json:"type"`
				RPCID   string `json:"rpcId"`
				Method  string `json:"method"`
				Payload struct {
					Args map[string]any `json:"args"`
				} `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type != "client-request" || body.Method != "$events/result" {
				t.Error("wrong result envelope")
			}
			fake.mu.Lock()
			fake.results = append(fake.results, body.Payload.Args)
			fake.mu.Unlock()
			_, _ = w.Write([]byte(`{"type":"server-response","rpcId":"` + body.RPCID + `","result":{"ok":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return fake, server
}

func (f *fakeDSHNative) recorded() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.results...)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if condition() {
			return
		}
	}
	t.Fatal("condition not reached")
}

func TestDSHInteractionForwardedEventsAndAnswerAuthority(t *testing.T) {
	fake, server := newFakeDSHNative(t)
	defer server.Close()
	native := &dshNative{address: server.URL, cookie: "dsh-auth-x=signed"}
	owner := newDSHInteractionOwner("session-owned")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go owner.run(ctx, native)

	fake.frames <- `{"type":"ready","clientId":"client-1","host":{"home":"/home/x"}}`
	fake.frames <- `{"type":"waterfall","event":"approval/request","eventId":"approval","agentId":"agent-1","request":{"toolName":"bash","callId":"call-1","reason":"Write outside workspace"}}`
	fake.frames <- `{"type":"waterfall","event":"user-questions/request","eventId":"question","agentId":"agent-1","request":{"questions":[{"id":"q","question":"Choose","options":[{"label":"A"}]}],"wait":{"callId":"call-2"}}}`
	fake.frames <- `{"type":"waterfall","event":"some/other","eventId":"other","agentId":"agent-1","request":{}}`
	waitFor(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		return len(owner.pending) == 2
	})
	waitFor(t, func() bool { return len(fake.recorded()) == 1 })
	if passed := fake.recorded()[0]; passed["eventId"] != "other" || passed["clientId"] != "client-1" || passed["outcome"].(map[string]any)["kind"] != "next" {
		t.Fatalf("unanswered event not passed on: %v", passed)
	}
	snapshot, _ := json.Marshal(owner.snapshot())
	if !strings.Contains(string(snapshot), `"type":"approval/requested"`) || !strings.Contains(string(snapshot), `"approvalId":"approval"`) || !strings.Contains(string(snapshot), `"type":"question/requested"`) || !strings.Contains(string(snapshot), `"sessionId":"session-owned"`) {
		t.Fatalf("snapshot=%s", snapshot)
	}

	owner.mu.Lock()
	epoch := owner.epoch
	owner.mu.Unlock()
	if err := owner.answer(ctx, native, map[string]any{"epoch": "old", "rpcId": "approval", "outcome": "allowed-once"}); err == nil {
		t.Fatal("stale epoch accepted")
	}
	if err := owner.answer(ctx, native, map[string]any{"epoch": epoch, "rpcId": "question"}); err == nil {
		t.Fatal("empty question answer accepted")
	}
	if err := owner.answer(ctx, native, map[string]any{"epoch": epoch, "rpcId": "approval", "outcome": "allowed-always"}); err == nil {
		t.Fatal("unsupported approval accepted")
	}
	if len(fake.recorded()) != 1 {
		t.Fatal("invalid answer crossed native boundary")
	}
	if err := owner.answer(ctx, native, map[string]any{"epoch": epoch, "rpcId": "approval", "outcome": "rejected"}); err != nil {
		t.Fatal(err)
	}
	if err := owner.answer(ctx, native, map[string]any{"epoch": epoch, "rpcId": "approval", "outcome": "allowed-once"}); err == nil {
		t.Fatal("answered request reused")
	}
	answer := map[string]any{"answers": []any{map[string]any{"id": "q", "selected": []string{"A"}}}}
	if err := owner.answer(ctx, native, map[string]any{"epoch": epoch, "rpcId": "question", "answer": answer}); err != nil {
		t.Fatal(err)
	}
	results := fake.recorded()
	approval, question := results[1], results[2]
	if approval["eventId"] != "approval" || approval["outcome"].(map[string]any)["value"] != "rejected" {
		t.Fatalf("approval=%v", approval)
	}
	if outcome := question["outcome"].(map[string]any); outcome["kind"] != "result" || len(outcome["value"].(map[string]any)["answers"].([]any)) != 1 {
		t.Fatalf("question=%v", question)
	}

	fake.frames <- `{"type":"waterfall","event":"approval/request","eventId":"approval-2","agentId":"agent-1","request":{"toolName":"bash"}}`
	waitFor(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		return len(owner.pending) == 1
	})
	fake.frames <- `{"type":"cancel","eventId":"approval-2"}`
	waitFor(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		return len(owner.pending) == 0
	})
	fake.frames <- `{"type":"waterfall","event":"approval/request","eventId":"approval-3","agentId":"agent-1","request":{"toolName":"bash"}}`
	waitFor(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		return len(owner.pending) == 1
	})
	close(fake.frames)
	waitFor(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		return !owner.connected && len(owner.pending) == 0 && owner.epoch != epoch
	})
}

func TestDSHQuestionCancelRejectsNativeEvent(t *testing.T) {
	fake, server := newFakeDSHNative(t)
	defer server.Close()
	native := &dshNative{address: server.URL, cookie: "dsh-auth-x=signed"}
	owner := newDSHInteractionOwner("session-owned")
	owner.reset(true, "client-1")
	owner.consume([]byte(`{"type":"waterfall","event":"user-questions/request","eventId":"question","agentId":"a","request":{"questions":[{"id":"q","question":"Choose"}]}}`))
	if err := owner.answer(context.Background(), native, map[string]any{"epoch": owner.epoch, "rpcId": "question", "cancel": true}); err != nil {
		t.Fatal(err)
	}
	outcome := fake.recorded()[0]["outcome"].(map[string]any)
	if outcome["kind"] != "rejected" || outcome["error"].(map[string]any)["message"] != "Cancelled by user" {
		t.Fatalf("outcome=%v", outcome)
	}
	if len(owner.pending) != 0 {
		t.Fatal("cancelled question retained")
	}
}

func TestDSHLaunchTokenFromNativeOutput(t *testing.T) {
	output := []byte("booting\ndsh web: http://127.0.0.1:38771/?token=abc_DEF-123\n")
	if token := dshLaunchToken(output); token != "abc_DEF-123" {
		t.Fatalf("token=%q", token)
	}
	if dshLaunchToken([]byte("dsh: startup failed")) != "" {
		t.Fatal("token invented")
	}
}
