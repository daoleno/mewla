package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// brainTickFixture drives a Brain-scoped conversation subscription one tick at
// a time over a real Claude transcript, a Store, a Calendar and a client socket,
// and holds what a client that follows the stream would have.
type brainTickFixture struct {
	t          *testing.T
	srv        *Server
	store      *brain.Store
	service    *brain.Service
	cal        *calendar.Store
	home       string
	cwd        string
	sessionID  string
	transcript string
	thread     string
	hostID     string

	serverConn *websocket.Conn
	messages   chan map[string]json.RawMessage
	liveID     string
	live       *codexConversationSubscriptionSnapshot
	reader     *work.ProviderConversationReader
	generation string

	// unstamped: the provider reader names no parse, so what a tick does is
	// decided by content and the kinds below shift accordingly.
	unstamped bool

	mu     sync.Mutex
	ticks  []brainTick
	lines  int
	replay map[string]json.RawMessage
}

func newBrainTickFixture(t *testing.T) *brainTickFixture {
	t.Helper()
	f := &brainTickFixture{
		t: t, home: t.TempDir(), cwd: "/repo/brain-tick", sessionID: "brain-tick-session",
		thread: "brain-tick-thread", hostID: "brain-tick-host", liveID: "live",
		messages: make(chan map[string]json.RawMessage, 256), replay: map[string]json.RawMessage{},
		generation: "generation-live",
	}
	t.Setenv("HOME", f.home)
	var err error
	if f.store, err = brain.NewStore(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	f.service = brain.NewService(f.store, nil, nil)
	if f.cal, err = calendar.NewStore(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	f.transcript = filepath.Join(f.home, ".claude", "projects", strings.NewReplacer("/", "-", ".", "-").Replace(f.cwd), f.sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(f.transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	f.appendLine("user", "hello Brain", time.Now().UTC().Add(-time.Hour))
	f.appendLine("assistant", "hello person", time.Now().UTC().Add(-time.Hour+time.Second))
	if err := f.store.SetChatState(brain.ChatState{ThreadID: f.thread}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ReplaceHostSessionBinding(f.hostID, "claude", f.sessionID, f.transcript, filepath.Join(f.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	f.srv = &Server{brain: f.service, calendar: f.cal, watcher: watcher.New(time.Second)}
	f.srv.brainTickObserve = func(tick brainTick) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.ticks = append(f.ticks, tick)
	}
	f.reader = work.NewProviderConversationReader()

	registered := make(chan *websocket.Conn, 1)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.srv.mu.Lock()
		f.srv.outboxes = map[*websocket.Conn]*clientOutbox{conn: newClientOutbox(conn)}
		f.srv.codexSubs = map[*websocket.Conn]map[string]codexConversationSubscription{conn: {}}
		f.srv.mu.Unlock()
		registered <- conn
		<-r.Context().Done()
	}))
	t.Cleanup(httpServer.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case f.serverConn = <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("socket was not registered")
	}
	go func() {
		for {
			var fields map[string]json.RawMessage
			if err := client.ReadJSON(&fields); err != nil {
				return
			}
			f.messages <- fields
		}
	}()
	f.register(f.liveID, f.generation)
	return f
}

func (f *brainTickFixture) register(id, generation string) {
	f.srv.mu.Lock()
	defer f.srv.mu.Unlock()
	f.srv.codexSubs[f.serverConn][id] = codexConversationSubscription{cancel: func() {}, generation: generation}
}

func (f *brainTickFixture) request(id string) clientMessage {
	return clientMessage{
		Type: "codex_conversation_subscribe", RequestID: id, TargetID: f.hostID, Cwd: f.cwd,
		Command: "claude --resume " + f.sessionID, StartedAt: json.RawMessage(`"2026-01-01T00:00:00Z"`),
		ConversationScopeKey: "brain-thread:" + f.thread,
	}
}

// appendLine adds one record to the transcript the way Claude does.
func (f *brainTickFixture) appendLine(kind, text string, at time.Time) {
	f.t.Helper()
	f.lines++
	record := map[string]any{
		"type": kind, "cwd": f.cwd, "sessionId": f.sessionID,
		"uuid": fmt.Sprintf("record-%d", f.lines), "timestamp": at.UTC().Format(time.RFC3339Nano),
	}
	if kind == "assistant" {
		record["message"] = map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": text}}}
	} else {
		record["message"] = map[string]any{"role": "user", "content": text}
	}
	line, err := json.Marshal(record)
	if err != nil {
		f.t.Fatal(err)
	}
	file, err := os.OpenFile(f.transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		f.t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		f.t.Fatal(err)
	}
	// Two appends inside one clock tick would otherwise look like no change.
	future := time.Now().Add(time.Duration(f.lines) * time.Second)
	if err := os.Chtimes(f.transcript, future, future); err != nil {
		f.t.Fatal(err)
	}
}

// idle is what a tick does when nothing moved, brain what it does when only
// the Brain side moved.
func (f *brainTickFixture) idle() brainTick {
	if f.unstamped {
		return brainTickSame
	}
	return brainTickIdle
}

func (f *brainTickFixture) brain() brainTick {
	if f.unstamped {
		return brainTickProvider
	}
	return brainTickBrain
}

func (f *brainTickFixture) takeTicks() []brainTick {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.ticks
	f.ticks = nil
	return out
}

func (f *brainTickFixture) nextMessage() map[string]json.RawMessage {
	f.t.Helper()
	select {
	case message := <-f.messages:
		return message
	case <-time.After(3 * time.Second):
		f.t.Fatal("no message arrived")
		return nil
	}
}

// follow applies a stream message to what the client holds.
func (f *brainTickFixture) follow(message map[string]json.RawMessage) {
	f.t.Helper()
	var kind string
	_ = json.Unmarshal(message["type"], &kind)
	switch kind {
	case "codex_conversation_snapshot":
		var snapshot struct {
			Conversation struct {
				Events []json.RawMessage `json:"events"`
			} `json:"conversation"`
		}
		raw, _ := json.Marshal(message)
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			f.t.Fatal(err)
		}
		f.replay = map[string]json.RawMessage{}
		for _, event := range snapshot.Conversation.Events {
			f.replay[eventID(f.t, event)] = event
		}
	case "codex_conversation_delta":
		var delta struct {
			Upserts []json.RawMessage `json:"upserts"`
			Deletes []string          `json:"deletes"`
		}
		raw, _ := json.Marshal(message)
		if err := json.Unmarshal(raw, &delta); err != nil {
			f.t.Fatal(err)
		}
		for _, event := range delta.Upserts {
			f.replay[eventID(f.t, event)] = event
		}
		for _, id := range delta.Deletes {
			delete(f.replay, id)
		}
	default:
		f.t.Fatalf("unexpected stream message %s", kind)
	}
}

func eventID(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	return head.ID
}

// tick runs one tick of the long-lived subscription and returns what it did
// and whether it sent the client anything.
func (f *brainTickFixture) tick() (ticks []brainTick, sent bool) {
	f.t.Helper()
	before := int64(0)
	if f.live != nil {
		before = f.live.revision
	}
	f.srv.publishCodexConversationSubscription(
		context.Background(), f.serverConn, f.request(f.liveID), f.liveID, f.generation, f.reader, &f.live)
	ticks = f.takeTicks()
	if f.live != nil && f.live.revision != before {
		f.follow(f.nextMessage())
		sent = true
	}
	return ticks, sent
}

// reference runs a subscription that has never seen the conversation: it can
// only answer with the full projection.
func (f *brainTickFixture) reference() (work.CodexConversation, map[string]json.RawMessage) {
	f.t.Helper()
	id, generation := fmt.Sprintf("reference-%d", time.Now().UnixNano()), "generation-reference"
	f.register(id, generation)
	var previous *codexConversationSubscriptionSnapshot
	f.srv.publishCodexConversationSubscription(
		context.Background(), f.serverConn, f.request(id), id, generation, work.NewProviderConversationReader(), &previous)
	f.takeTicks()
	if previous == nil {
		f.t.Fatal("the reference subscription answered nothing")
	}
	events := map[string]json.RawMessage{}
	snapshot := f.nextMessage()
	var wire struct {
		Conversation struct {
			Events []json.RawMessage `json:"events"`
		} `json:"conversation"`
	}
	raw, _ := json.Marshal(snapshot)
	if err := json.Unmarshal(raw, &wire); err != nil {
		f.t.Fatal(err)
	}
	for _, event := range wire.Conversation.Events {
		events[eventID(f.t, event)] = event
	}
	return previous.conversation, events
}

// check proves the long-lived subscription holds exactly what the full
// projection answers now: in the server's own state, in order, and in what a
// client that applied the stream has.
func (f *brainTickFixture) check(step string) {
	f.t.Helper()
	want, wantEvents := f.reference()
	if f.live == nil {
		f.t.Fatalf("%s: the subscription holds nothing", step)
	}
	got := f.live.conversation
	got.Updated, want.Updated = nil, nil
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		for index := 0; index < len(got.Events) && index < len(want.Events); index++ {
			left, _ := json.Marshal(got.Events[index])
			right, _ := json.Marshal(want.Events[index])
			if string(left) != string(right) {
				f.t.Fatalf("%s: event %d differs from the full projection\n got: %s\nwant: %s", step, index, left, right)
			}
		}
		f.t.Fatalf("%s: the subscription's conversation differs from the full projection\n got: %s\nwant: %s", step, clip(gotJSON), clip(wantJSON))
	}
	if !reflect.DeepEqual(f.replay, wantEvents) {
		f.t.Fatalf("%s: the client's events differ from the full projection: has %d, want %d", step, len(f.replay), len(wantEvents))
	}
}

func clip(raw []byte) string {
	if len(raw) > 1500 {
		return string(raw[:1500]) + "..."
	}
	return string(raw)
}

func (f *brainTickFixture) expectTicks(step string, want ...brainTick) {
	f.t.Helper()
	got, _ := f.tick()
	if !reflect.DeepEqual(got, want) {
		f.t.Fatalf("%s: ticks = %v, want %v", step, got, want)
	}
}

// The same walk twice: over a provider reader whose parse the subscription can
// name, and over one it cannot (a loader that proves nothing), where every
// tick must tell an unchanged conversation by its content instead.
func TestBrainTickFastPathAnswersWhatTheFullProjectionAnswers(t *testing.T) {
	for _, unstamped := range []bool{false, true} {
		t.Run(map[bool]string{false: "stamped", true: "unstamped"}[unstamped], func(t *testing.T) {
			f := newBrainTickFixture(t)
			if unstamped {
				f.srv.providerConversationLoader = func(_ *work.ProviderConversationReader, _ string) (work.CodexConversation, error) {
					// A reader of its own: the subscription's reader names no parse.
					return work.NewProviderConversationReader().Load(classifier.Worker{
						ID: f.hostID, Cwd: f.cwd, Command: "claude --resume " + f.sessionID,
					}, work.WorkerProviderClaude, time.Now())
				}
				f.unstamped = true
			}
			walkBrainTick(t, f)
		})
	}
}

func walkBrainTick(t *testing.T, f *brainTickFixture) {

	ticks, sent := f.tick()
	if !sent || !reflect.DeepEqual(ticks, []brainTick{brainTickProvider}) {
		t.Fatalf("first tick = %v sent=%v", ticks, sent)
	}
	f.check("first snapshot")
	// The first tick materialized the provider's rows, which moved the Store
	// after its key was read: one more tick of the Brain side, then rest.
	f.expectTicks("after the first materialization", f.brain())

	// Nothing moved: nothing is projected, however many ticks pass.
	for range 3 {
		if ticks, sent = f.tick(); sent || !reflect.DeepEqual(ticks, []brainTick{f.idle()}) {
			t.Fatalf("idle tick = %v sent=%v", ticks, sent)
		}
	}
	f.check("idle")

	// A write the conversation does not show moves the Store's counter. The
	// tick that finds it projects the Brain side once, finds the answer it
	// already sent, and remembers; the next tick has nothing to do.
	item, err := f.store.CreateWork(brain.Work{
		Title: "unrelated", Objective: "write elsewhere", Status: brain.WorkRunning,
		CompletionPolicy: brain.CompletionBounded, SourceThreadID: "some-other-thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	for n := range 3 {
		if _, _, err := f.service.AppendWorkEvent(brain.WorkEvent{
			WorkID: item.ID, Kind: "session.done", Summary: fmt.Sprintf("elsewhere %d", n),
			DedupeKey: fmt.Sprintf("session:elsewhere:turn:%d:session.done", n),
		}); err != nil {
			t.Fatal(err)
		}
		f.expectTicks("unrelated write", f.brain())
		f.expectTicks("after an unrelated write", f.idle())
	}
	f.check("unrelated writes")

	// The provider's transcript grows.
	f.appendLine("user", "what is next", time.Now().UTC().Add(-time.Minute))
	f.appendLine("assistant", "here is the plan", time.Now().UTC().Add(-time.Minute+time.Second))
	if ticks, sent = f.tick(); !sent || !reflect.DeepEqual(ticks, []brainTick{brainTickProvider}) {
		t.Fatalf("transcript growth: ticks = %v sent=%v", ticks, sent)
	}
	f.check("transcript growth")
	// The growth materialized rows: the Store moved with it, so one more tick
	// of the Brain side, then rest.
	f.expectTicks("after materialization", f.brain())
	f.expectTicks("after materialization", f.idle())
	f.check("after materialization")

	// A Work result for this thread becomes a card.
	card, err := f.store.CreateWork(brain.Work{
		Title: "research", Objective: "find out", Status: brain.WorkRunning,
		CompletionPolicy: brain.CompletionBounded, SourceThreadID: f.thread,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.AppendWorkEvent(brain.WorkEvent{
		WorkID: card.ID, Kind: "session.done", Summary: "first answer", DedupeKey: "session:card:turn:one:session.done",
	}); err != nil {
		t.Fatal(err)
	}
	if ticks, sent = f.tick(); !sent || ticks[0] != f.brain() {
		t.Fatalf("work card: ticks = %v sent=%v", ticks, sent)
	}
	f.check("work card")
	// A later result makes the earlier card no longer current: the annotation
	// of an old row changes with no row of its own written.
	if _, _, err := f.service.AppendWorkEvent(brain.WorkEvent{
		WorkID: card.ID, Kind: "session.done", Summary: "second answer", DedupeKey: "session:card:turn:two:session.done",
	}); err != nil {
		t.Fatal(err)
	}
	if _, sent = f.tick(); !sent {
		t.Fatal("a newer result did not reach the client")
	}
	f.check("newer work result")
	done := brain.WorkDone
	if _, err := f.service.UpdateWork(card.ID, brain.WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	ticks, sent = f.tick()
	t.Logf("work finished: ticks=%v sent=%v", ticks, sent)
	f.check("work finished")

	// A scheduled result.
	due := time.Now().Add(time.Hour).UTC()
	if _, err := f.cal.Create(calendar.Item{
		ID: "scheduled-1", Title: "Daily brief", Kind: calendar.KindScheduledAction, DueAt: &due, Timezone: "UTC",
		Recurrence: calendar.RecurrenceNone, ActionInstruction: "Brief me", SourceThreadID: f.thread,
	}); err != nil {
		t.Fatal(err)
	}
	f.expectTicks("calendar item created", f.brain())
	_, run, err := f.cal.Claim("scheduled-1", true)
	if err != nil {
		t.Fatal(err)
	}
	f.tick()
	if _, err := f.cal.FinishRun("scheduled-1", run.ID, "The brief.", ""); err != nil {
		t.Fatal(err)
	}
	if ticks, sent = f.tick(); !sent || !reflect.DeepEqual(ticks, []brainTick{f.brain()}) {
		t.Fatalf("scheduled result: ticks = %v sent=%v", ticks, sent)
	}
	f.check("scheduled result")
	f.expectTicks("after the scheduled result", f.idle())

	// A Brain input waits for its echo, then the provider's own record of it
	// arrives and is credited to the admission instead of showing twice.
	preparedAt := time.Now().UTC().Add(-30 * time.Second)
	candidate := brain.BrainInputAdmission{
		RequestID: "receipt-one", ThreadID: f.thread, HostSessionID: f.hostID, SessionID: f.hostID,
		DisplayBody: "please continue", CreatedAt: preparedAt,
	}
	if _, created, err := f.store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare admission created=%v err=%v", created, err)
	}
	accepted, _, _, err := f.store.AcceptBrainInputAdmission(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatal(err)
	}
	f.tick()
	f.check("admission accepted, no echo yet")
	f.appendLine("user", "please continue", preparedAt.Add(2*time.Second))
	f.tick()
	f.check("admission echoed")
	f.tick()
	f.tick()
	f.check("admission settled")
	rows := 0
	for _, raw := range f.replay {
		var event work.CodexConversationEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Kind == "user_message" && event.Body == "please continue" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("an admitted input shows %d times, want once", rows)
	}

	// A rewind hides the turns after a prompt, and releasing it brings them
	// back: the overlay is applied to what the provider answered, so a tick
	// that finds the overlay moved must not answer from the conversation it
	// projected before.
	_, events := f.reference()
	var rewindTo string
	for id, raw := range events {
		var event work.CodexConversationEvent
		_ = json.Unmarshal(raw, &event)
		if event.Kind == "assistant_message" && event.Body == "here is the plan" {
			rewindTo = id
		}
	}
	if rewindTo == "" {
		t.Fatalf("no provider event to rewind to in %d events", len(events))
	}
	f.srv.rewound.set(f.hostID, rewindTo)
	f.tick()
	f.check("rewound")
	f.expectTicks("after the rewind", f.idle())
	f.srv.rewound.mu.Lock()
	delete(f.srv.rewound.at, f.hostID)
	f.srv.rewound.changes++
	f.srv.rewound.mu.Unlock()
	f.tick()
	f.check("rewind released")

	// The Host binding and the chat state.
	if err := f.store.ReplaceHostSessionBinding(f.hostID, "claude", f.sessionID, f.transcript, filepath.Join(f.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	f.tick()
	f.check("host binding written")
	if err := f.store.SetChatState(brain.ChatState{ThreadID: "a-newer-thread"}); err != nil {
		t.Fatal(err)
	}
	f.tick()
	f.check("another thread is current")
	f.expectTicks("at rest", f.idle())

	select {
	case message := <-f.messages:
		t.Fatalf("an unexpected stream message arrived: %v", message)
	case <-time.After(100 * time.Millisecond):
	}
}
