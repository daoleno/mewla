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

// A Session that becomes hidden leaves the list when it happens, and one that
// becomes visible appears as a new Session, without waiting for the reconcile.
func TestWorkerFeedFollowsHiddenAndVisibleFlipsImmediately(t *testing.T) {
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", feedWire("a", "shell"), recorder.emit)

	feed.retire("never-listed", recorder.emit)
	if len(recorder.sent) != 1 {
		t.Fatalf("retiring a Session clients never had sent %d messages", len(recorder.sent))
	}
	feed.retire("a", recorder.emit)
	feed.retire("a", recorder.emit)
	if len(recorder.sent) != 2 {
		t.Fatalf("hiding sent %d messages, want one archive", len(recorder.sent)-1)
	}
	gone := decodeFeed(t, recorder.sent[1])
	if gone.Type != "worker_session_archived" || gone.Seq != 2 || gone.WorkerSession.ID != "a" || gone.WorkerSession.State != string(classifier.StateRemoved) {
		t.Fatalf("archive = %+v", gone)
	}

	// Visible again: an update for a Session clients do not hold is a creation.
	feed.publishChange(feedWire("a", "shell"), recorder.emit)
	back := decodeFeed(t, recorder.sent[2])
	if back.Type != "worker_session_created" || back.Seq != 3 {
		t.Fatalf("reappearance = %+v", back)
	}
	feed.publishChange(feedWire("a", "renamed"), recorder.emit)
	if again := decodeFeed(t, recorder.sent[3]); again.Type != "worker_session_updated" || again.Seq != 4 {
		t.Fatalf("known Session update = %+v", again)
	}
	// And the reconcile finds nothing left to say.
	before := len(recorder.sent)
	feed.reconcile(func() []*workerSessionWire { return []*workerSessionWire{feedWire("a", "renamed")} }, recorder.emit)
	if len(recorder.sent) != before {
		t.Fatalf("reconcile re-announced %d messages", len(recorder.sent)-before)
	}
}

func TestWatcherEventsMoveASessionInAndOutOfTheListWhenItsVisibilityFlips(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	conn := feedTestSocket(t, srv)
	defer srv.shutdownAuthenticatedClients()
	readFeedType(t, conn, "worker_session_list")

	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "w", Worker: &classifier.Worker{ID: "w", Name: "shell"}})
	if m, _ := readFeedType(t, conn, "worker_session_created"); m.WorkerSession.ID != "w" {
		t.Fatalf("created = %+v", m)
	}
	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_metadata_change", WorkerID: "w", Worker: &classifier.Worker{ID: "w", Name: "shell", Hidden: true}})
	if m, _ := readFeedType(t, conn, "worker_session_archived"); m.WorkerSession.ID != "w" || m.Seq != 2 {
		t.Fatalf("hiding = %+v", m)
	}
	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_metadata_change", WorkerID: "w", Worker: &classifier.Worker{ID: "w", Name: "shell"}})
	if m, _ := readFeedType(t, conn, "worker_session_created"); m.WorkerSession.ID != "w" || m.Seq != 3 {
		t.Fatalf("showing = %+v", m)
	}
}

func outputWire(id string, tick int) *workerSessionWire {
	return &workerSessionWire{Worker: &classifier.Worker{ID: id, Name: "n", LastLines: []string{"• Working (" + strings.Repeat("s", tick%7) + ")", strings.Repeat("x", tick)}}}
}

func withOutputGap(t *testing.T, gap time.Duration) {
	t.Helper()
	previous := workerOutputUpdateGap
	workerOutputUpdateGap = gap
	t.Cleanup(func() { workerOutputUpdateGap = previous })
}

func waitForFeedCount(t *testing.T, recorder *feedRecorder, want int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		recorder.mu.Lock()
		n := len(recorder.sent)
		recorder.mu.Unlock()
		if n >= want {
			return
		}
	}
	t.Fatalf("feed never reached %d messages", want)
}

// A screen that changes on every poll reaches clients once per gap, and the
// last message is the newest state.
func TestWorkerFeedPacesOutputOnlyUpdates(t *testing.T) {
	withOutputGap(t, 100*time.Millisecond)
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", outputWire("w", 0), recorder.emit)
	for tick := 1; tick <= 8; tick++ {
		feed.publishOutput(outputWire("w", tick), recorder.emit)
	}
	if n := len(recorder.sent); n != 1 {
		t.Fatalf("%d messages inside the gap, want only the creation", n)
	}
	waitForFeedCount(t, recorder, 2)
	time.Sleep(250 * time.Millisecond)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.sent) != 2 {
		t.Fatalf("got %d messages, want creation plus one paced update", len(recorder.sent))
	}
	if !strings.Contains(string(recorder.sent[1]), strings.Repeat("x", 8)) || strings.Contains(string(recorder.sent[1]), strings.Repeat("x", 9)) {
		t.Fatalf("paced update is not the newest state: %s", recorder.sent[1])
	}
	if got := decodeFeed(t, recorder.sent[1]).Seq; got != 2 {
		t.Fatalf("paced update has seq %d", got)
	}
}

func TestWorkerFeedSendsNothingForUnchangedOutput(t *testing.T) {
	withOutputGap(t, 20*time.Millisecond)
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", outputWire("w", 3), recorder.emit)
	time.Sleep(40 * time.Millisecond)
	feed.publishOutput(outputWire("w", 3), recorder.emit)
	time.Sleep(60 * time.Millisecond)
	if n := len(recorder.sent); n != 1 {
		t.Fatalf("an unchanged Session was sent again: %d messages", n)
	}
}

// A state change is never held, and it supersedes a held output change: the
// stale output must not arrive after it.
func TestWorkerFeedStateChangeIsImmediateAndSupersedesHeldOutput(t *testing.T) {
	withOutputGap(t, 80*time.Millisecond)
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", outputWire("w", 0), recorder.emit)
	feed.publishOutput(outputWire("w", 4), recorder.emit) // held
	changed := outputWire("w", 5)
	changed.State = classifier.StateBlocked
	feed.publishChange(changed, recorder.emit)
	if n := len(recorder.sent); n != 2 {
		t.Fatalf("state change was held: %d messages", n)
	}
	time.Sleep(200 * time.Millisecond)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.sent) != 2 {
		t.Fatalf("a stale held update arrived after the state change: %d messages", len(recorder.sent))
	}
}

func TestWorkerFeedArchiveDropsHeldOutput(t *testing.T) {
	withOutputGap(t, 60*time.Millisecond)
	var feed workerFeed
	recorder := &feedRecorder{}
	feed.publish("worker_session_created", outputWire("w", 0), recorder.emit)
	feed.publishOutput(outputWire("w", 2), recorder.emit)
	feed.retire("w", recorder.emit)
	time.Sleep(150 * time.Millisecond)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.sent) != 2 || decodeFeed(t, recorder.sent[1]).Type != "worker_session_archived" {
		t.Fatalf("an archived Session was updated after it left: %d messages", len(recorder.sent))
	}
}

// The same output stream a phone is sent, before and after pacing: ten running
// Sessions whose screens change every poll (scaled: a 2 s gap against 500 ms
// polls becomes 160 ms against 40 ms).
func TestWorkerFeedOutputBytesBeforeAndAfterPacing(t *testing.T) {
	withOutputGap(t, 160*time.Millisecond)
	const sessions, polls, poll = 10, 50, 40 * time.Millisecond
	var before, after int
	var paced feedRecorder
	var feed workerFeed
	for s := 0; s < sessions; s++ {
		feed.publish("worker_session_created", outputWire(string(rune('a'+s)), 0), paced.emit)
	}
	for tick := 1; tick <= polls; tick++ {
		for s := 0; s < sessions; s++ {
			wire := outputWire(string(rune('a'+s)), tick)
			body, _ := json.Marshal(wire) // what the unpaced path emitted every poll
			before += len(body)
			feed.publishOutput(wire, paced.emit)
		}
		time.Sleep(poll)
	}
	time.Sleep(300 * time.Millisecond)
	paced.mu.Lock()
	defer paced.mu.Unlock()
	for _, data := range paced.sent[sessions:] {
		after += len(data)
	}
	t.Logf("output-only updates: before %d B (%d messages), after %d B (%d messages)", before, sessions*polls, after, len(paced.sent)-sessions)
	if after*3 > before {
		t.Fatalf("pacing saved too little: %d B before, %d B after", before, after)
	}
}

// A running Session says what its agent is doing now, from the agent's own
// report; a Session that is not running says nothing of the kind.
func TestWorkerSessionWireCarriesTheActivityOfARunningSession(t *testing.T) {
	srv := &Server{}
	srv.SetAgentActivity(func(id string) AgentActivity {
		return map[string]AgentActivity{
			"%1": {Line: "Bash curl -sI https://example.org", Tool: "Bash", Summary: "curl -sI https://example.org"},
			"%2": {Line: "Read a.go", Tool: "Read", Summary: "a.go"},
		}[id]
	})
	since := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	running := srv.workerSessionWire(&classifier.Worker{ID: "%1", State: classifier.StateRunning, RunningSince: &since})
	encoded, _ := json.Marshal(running)
	var wire map[string]any
	_ = json.Unmarshal(encoded, &wire)
	// The tool and what it works on, apart, so the app words it as its thread's tool row does ("Run curl …").
	if wire["activity"] != "Bash curl -sI https://example.org" || wire["activity_tool"] != "Bash" || wire["activity_summary"] != "curl -sI https://example.org" ||
		wire["running_since"] != "2026-11-13T04:00:00Z" || wire["status"] != "running" {
		t.Fatalf("running wire = %s", encoded)
	}
	idle := srv.workerSessionWire(&classifier.Worker{ID: "%2", State: classifier.StateUnknown})
	encoded, _ = json.Marshal(idle)
	if strings.Contains(string(encoded), "activity") || strings.Contains(string(encoded), "running_since") {
		t.Fatalf("an idle Session's line from its last turn is not news: %s", encoded)
	}
}
