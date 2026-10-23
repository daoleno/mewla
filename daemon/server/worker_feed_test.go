package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

type feedMessage struct {
	Type          string `json:"type"`
	Seq           uint64 `json:"seq"`
	WorkerSession *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State string `json:"status"`
	} `json:"worker_session"`
	WorkerSessions []struct {
		ID string `json:"id"`
	} `json:"worker_sessions"`
}

func feedWire(id, name string) *workerSessionWire {
	return &workerSessionWire{Worker: &classifier.Worker{ID: id, Name: name}}
}

func decodeFeed(t *testing.T, data []byte) feedMessage {
	t.Helper()
	var m feedMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

type feedRecorder struct {
	mu   sync.Mutex
	sent [][]byte
}

func (r *feedRecorder) emit(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, data)
}

func TestWorkerFeedAssignsContiguousSeqUnderConcurrentEvents(t *testing.T) {
	var feed workerFeed
	recorder := &feedRecorder{}
	const writers, each = 8, 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				feed.publish("worker_session_updated", feedWire("w", "n"), recorder.emit)
			}
		}(w)
	}
	wg.Wait()
	if len(recorder.sent) != writers*each {
		t.Fatalf("sent %d messages, want %d", len(recorder.sent), writers*each)
	}
	// The order messages were queued in is the order of seq: no gap, no repeat.
	for i, data := range recorder.sent {
		if got := decodeFeed(t, data).Seq; got != uint64(i+1) {
			t.Fatalf("message %d has seq %d", i, got)
		}
	}
}

func TestWorkerFeedSnapshotCarriesTheSeqItIsCurrentAs(t *testing.T) {
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", feedWire("a", "A"), recorder.emit)
	feed.publish("worker_session_updated", feedWire("a", "A2"), recorder.emit)
	var snapshot []byte
	feed.snapshot(func() []*workerSessionWire { return []*workerSessionWire{feedWire("a", "A2")} }, func(d []byte) { snapshot = d })
	list := decodeFeed(t, snapshot)
	if list.Type != "worker_session_list" || list.Seq != 2 || len(list.WorkerSessions) != 1 {
		t.Fatalf("snapshot = %s", snapshot)
	}
	feed.publish("worker_session_updated", feedWire("a", "A3"), recorder.emit)
	if got := decodeFeed(t, recorder.sent[2]).Seq; got != 3 {
		t.Fatalf("change after the snapshot has seq %d, want snapshot seq + 1", got)
	}
	// Taking a snapshot never consumes a seq.
	feed.snapshot(func() []*workerSessionWire { return nil }, func(d []byte) { snapshot = d })
	empty := decodeFeed(t, snapshot)
	if empty.Seq != 3 || empty.WorkerSessions == nil || len(empty.WorkerSessions) != 0 {
		t.Fatalf("empty snapshot = %s", snapshot)
	}
	if !strings.Contains(string(snapshot), `"worker_sessions":[]`) {
		t.Fatalf("an empty list must be [], got %s", snapshot)
	}
}

func TestWorkerFeedHeartbeatCarriesTheSeqWithoutConsumingOne(t *testing.T) {
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.heartbeat(recorder.emit)
	feed.publish("worker_session_created", feedWire("a", "A"), recorder.emit)
	feed.heartbeat(recorder.emit)
	feed.publish("worker_session_updated", feedWire("a", "A2"), recorder.emit)
	if len(recorder.sent) != 4 {
		t.Fatalf("sent %d messages", len(recorder.sent))
	}
	first, second, last := decodeFeed(t, recorder.sent[0]), decodeFeed(t, recorder.sent[2]), decodeFeed(t, recorder.sent[3])
	if first.Type != "worker_session_heartbeat" || first.Seq != 0 || second.Type != "worker_session_heartbeat" || second.Seq != 1 {
		t.Fatalf("heartbeats = %s / %s", recorder.sent[0], recorder.sent[2])
	}
	if last.Seq != 2 {
		t.Fatalf("a heartbeat consumed a seq: next change has %d, want 2", last.Seq)
	}
	if strings.Contains(string(recorder.sent[0]), "worker_session\"") {
		t.Fatalf("a heartbeat must not carry a Session: %s", recorder.sent[0])
	}
}

func TestWorkerFeedReconcileSendsOnlyDifferencesAsSequencedChanges(t *testing.T) {
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", feedWire("keep", "K"), recorder.emit)
	feed.publish("worker_session_created", feedWire("change", "C"), recorder.emit)
	feed.publish("worker_session_created", feedWire("hide", "H"), recorder.emit)
	recorder.sent = nil

	current := []*workerSessionWire{feedWire("keep", "K"), feedWire("change", "C!"), feedWire("new", "N")}
	feed.reconcile(func() []*workerSessionWire { return current }, recorder.emit)

	got := map[string]string{}
	var last uint64 = 3
	for _, data := range recorder.sent {
		m := decodeFeed(t, data)
		if m.Seq != last+1 {
			t.Fatalf("reconcile seq %d after %d", m.Seq, last)
		}
		last = m.Seq
		got[m.WorkerSession.ID] = m.Type
		if m.Type == "worker_session_archived" && m.WorkerSession.State != string(classifier.StateRemoved) {
			t.Fatalf("archived state = %q", m.WorkerSession.State)
		}
	}
	want := map[string]string{
		"change": "worker_session_updated",
		"new":    "worker_session_created",
		"hide":   "worker_session_archived",
	}
	if len(got) != len(want) {
		t.Fatalf("reconcile sent %v, want %v", got, want)
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Fatalf("reconcile sent %v, want %v", got, want)
		}
	}

	// Nothing differs now, so an idle reconcile sends nothing at all.
	recorder.sent = nil
	feed.reconcile(func() []*workerSessionWire { return current }, recorder.emit)
	if len(recorder.sent) != 0 {
		t.Fatalf("idle reconcile sent %d messages", len(recorder.sent))
	}
}

func feedTestSocket(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.auth = manager
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(),
		"feed-test", "Feed Test", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(host.Close)
	header := http.Header{"Authorization": []string{calendarAuthHeader(private, manager.DaemonID(), "feed-test", "mewla-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readFeedType(t *testing.T, conn *websocket.Conn, types ...string) (feedMessage, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		m := decodeFeed(t, raw)
		for _, want := range types {
			if m.Type == want {
				return m, raw
			}
		}
	}
}

func TestWorkerFeedConnectSnapshotThenSequencedChangesAndResync(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	conn := feedTestSocket(t, srv)
	defer srv.shutdownAuthenticatedClients()

	snapshot, _ := readFeedType(t, conn, "worker_session_list")
	if snapshot.Seq != 0 {
		t.Fatalf("connect snapshot seq = %d", snapshot.Seq)
	}
	for i := 1; i <= 2; i++ {
		srv.publishWorkerSession("worker_session_updated", &classifier.Worker{ID: "w", Name: "n"})
		change, _ := readFeedType(t, conn, "worker_session_updated")
		if change.Seq != uint64(i) {
			t.Fatalf("change %d has seq %d", i, change.Seq)
		}
	}
	// A client that noticed a gap asks again and is current as of the latest seq.
	if err := conn.WriteJSON(map[string]any{"type": "list_worker_sessions"}); err != nil {
		t.Fatal(err)
	}
	resync, _ := readFeedType(t, conn, "worker_session_list")
	if resync.Seq != 2 {
		t.Fatalf("resync seq = %d, want 2", resync.Seq)
	}
	srv.publishWorkerSession("worker_session_archived", &classifier.Worker{ID: "w"})
	next, _ := readFeedType(t, conn, "worker_session_archived")
	if next.Seq != 3 {
		t.Fatalf("change after resync has seq %d", next.Seq)
	}
}

func TestKillWorkerRepliesWithAnExplicitSuccessFact(t *testing.T) {
	owner := startProfileOwner(t)
	commitTestRoute(t, owner, "tmux:@ws-kill-ok")
	srv := New(nil, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetModelProfiles(owner)
	srv.killSessionOverride = func(string) error { return nil }
	srv.probeSessionOverride = func(string) (watcher.SessionPresence, error) {
		return watcher.SessionPresenceAbsent, nil
	}
	conn := feedTestSocket(t, srv)
	defer srv.shutdownAuthenticatedClients()
	if err := conn.WriteJSON(map[string]any{"type": "kill_worker", "request_id": "kill-ok", "worker_id": "tmux:@ws-kill-ok"}); err != nil {
		t.Fatal(err)
	}
	_, raw := readFeedType(t, conn, "worker_killed", "error")
	var reply map[string]any
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	if reply["type"] != "worker_killed" || reply["request_id"] != "kill-ok" || reply["worker_id"] != "tmux:@ws-kill-ok" {
		t.Fatalf("reply = %v", reply)
	}
}
