package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

const ocSession = "ses_followed0000000000000000"

// fakeOpencode is a service whose stream the test drives.
type fakeOpencode struct {
	mu          sync.Mutex
	exists      bool
	active      map[string]bool
	outcome     opencodeapi.Outcome
	permissions []opencodeapi.Permission
	prompts     []string
	interrupts  int
	replies     []string
	promptErr   error
	frames      chan []byte
}

func newFakeOpencode() *fakeOpencode {
	return &fakeOpencode{exists: true, active: map[string]bool{}, frames: make(chan []byte, 16)}
}

func (f *fakeOpencode) Stream(ctx context.Context, handle func([]byte)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case raw, ok := <-f.frames:
			if !ok {
				return fmt.Errorf("stream ended")
			}
			handle(raw)
		}
	}
}
func (f *fakeOpencode) Prompt(_ context.Context, session, id, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promptErr != nil {
		return "", f.promptErr
	}
	f.prompts = append(f.prompts, session+"|"+id+"|"+text)
	return id, nil
}
func (f *fakeOpencode) Interrupt(context.Context, string) error {
	f.mu.Lock()
	f.interrupts++
	f.mu.Unlock()
	return nil
}
func (f *fakeOpencode) Reply(_ context.Context, session, request, decision string) error {
	f.mu.Lock()
	f.replies = append(f.replies, session+"|"+request+"|"+decision)
	f.mu.Unlock()
	return nil
}
func (f *fakeOpencode) SessionExists(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exists, nil
}
func (f *fakeOpencode) Active(context.Context) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active, nil
}
func (f *fakeOpencode) Permissions(context.Context, string) ([]opencodeapi.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.permissions, nil
}
func (f *fakeOpencode) LastOutcome(context.Context, string) (opencodeapi.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outcome, nil
}

func frame(typ, data string) []byte {
	return []byte(fmt.Sprintf(`{"id":"evt_%s","type":%q,"data":%s}`, typ, typ, data))
}

func startOpencode(t *testing.T, service *fakeOpencode) (*opencodeEvents, *agentevents.Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := agentevents.NewHub()
	events := newOpencodeEvents(ctx, hub, func(*classifier.Worker) string { return work.WorkerProviderOpenCode })
	events.open = func(context.Context) (opencodeService, error) { return service, nil }
	worker := &classifier.Worker{ID: "%5", Command: "opencode --session " + ocSession}
	events.Observe(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "%5", Worker: worker})
	waitFor(t, "the Worker to connect", func() bool {
		state, ok := hub.State("%5")
		return ok && state.Connected && state.Bound
	})
	return events, hub
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestOpencodeWorkerIsBoundByItsLaunchSessionAndFollowsItsTurns(t *testing.T) {
	service := newFakeOpencode()
	_, hub := startOpencode(t, service)
	state, _ := hub.State("%5")
	if state.Binding.ConversationID != ocSession || state.Binding.By != "launch" {
		t.Fatalf("binding = %+v", state.Binding)
	}
	other := `{"sessionID":"ses_someone_else"}`
	service.frames <- frame("session.execution.started", other)
	d := `{"sessionID":"` + ocSession + `"}`
	service.frames <- frame("session.execution.started", d)
	waitFor(t, "the turn to start", func() bool {
		s, _ := hub.State("%5")
		return s.Current != nil && s.Current.Status == agentevents.TurnRunning
	})
	service.frames <- frame("session.execution.succeeded", d)
	waitFor(t, "the turn to end", func() bool {
		s, _ := hub.State("%5")
		return s.Current != nil && s.Current.Status == agentevents.TurnCompleted
	})
}

func TestOpencodeSendGoesToTheServiceWithTheReceiptAsItsMessageId(t *testing.T) {
	service := newFakeOpencode()
	events, _ := startOpencode(t, service)
	began := false
	handled, err := events.send("%5", "do the thing", "mewla-msg-1234", func() error { began = true; return nil })
	if !handled || err != nil || !began {
		t.Fatalf("send = (%v, %v), began %v", handled, err, began)
	}
	if got := service.prompts[0]; got != ocSession+"|msg_mewlamsg1234|do the thing" {
		t.Fatalf("prompt = %q", got)
	}

	service.promptErr = &opencodeapi.StatusError{Code: 400}
	_, err = events.send("%5", "x", "r2", func() error { return nil })
	var input *watcher.StructuredInputError
	if !asInputError(err, &input) || !input.NotSent {
		t.Fatalf("a refused message must be NotSent, got %v", err)
	}
	service.promptErr = fmt.Errorf("connection reset")
	_, err = events.send("%5", "x", "r3", func() error { return nil })
	if !asInputError(err, &input) || input.NotSent {
		t.Fatalf("a lost connection is ambiguous, not NotSent: %v", err)
	}
}

func asInputError(err error, target **watcher.StructuredInputError) bool {
	e, ok := err.(*watcher.StructuredInputError)
	if ok {
		*target = e
	}
	return ok
}

func TestOpencodeSendToASessionTheServiceDoesNotHaveYetIsTypedIntoThePane(t *testing.T) {
	service := newFakeOpencode()
	service.exists = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := agentevents.NewHub()
	events := newOpencodeEvents(ctx, hub, func(*classifier.Worker) string { return work.WorkerProviderOpenCode })
	events.open = func(context.Context) (opencodeService, error) { return service, nil }
	events.Observe(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "%5", Worker: &classifier.Worker{ID: "%5", Command: "opencode --session " + ocSession}})
	waitFor(t, "the service connection", func() bool { return events.current() != nil })
	handled, err := events.send("%5", "hi", "r", func() error { t.Fatal("begin ran for a session the service lacks"); return nil })
	if handled || err != nil {
		t.Fatalf("send = (%v, %v), want it left to the pane", handled, err)
	}
	// The first prompt, typed, creates the session; its events then connect.
	service.frames <- frame("session.execution.started", `{"sessionID":"`+ocSession+`"}`)
	waitFor(t, "the Worker to connect on its first event", func() bool {
		s, ok := hub.State("%5")
		return ok && s.Connected && s.Bound
	})
}

func TestOpencodeInterruptStopsOnlyARunningTurn(t *testing.T) {
	service := newFakeOpencode()
	events, hub := startOpencode(t, service)
	if handled, err := events.interrupt("%5"); !handled || err != nil || service.interrupts != 0 {
		t.Fatalf("idle interrupt = (%v, %v), %d calls", handled, err, service.interrupts)
	}
	service.frames <- frame("session.execution.started", `{"sessionID":"`+ocSession+`"}`)
	waitFor(t, "the turn to start", func() bool {
		s, _ := hub.State("%5")
		return s.Current != nil && s.Current.Status == agentevents.TurnRunning
	})
	if handled, err := events.interrupt("%5"); !handled || err != nil || service.interrupts != 1 {
		t.Fatalf("running interrupt = (%v, %v), %d calls", handled, err, service.interrupts)
	}
	if handled, _ := events.interrupt("%9"); handled {
		t.Fatal("a Worker nobody follows was handled")
	}
}

func TestOpencodePermissionIsAnsweredThroughTheServiceOnlyOnce(t *testing.T) {
	service := newFakeOpencode()
	_, hub := startOpencode(t, service)
	service.frames <- frame("permission.asked", `{"id":"per_1","sessionID":"`+ocSession+`","action":"shell","resources":["rm -rf build"]}`)
	waitFor(t, "the approval", func() bool { s, _ := hub.State("%5"); return len(s.Approvals) == 1 })
	if err := hub.Decide("%5", "per_1", "once", time.Now()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the reply", func() bool { service.mu.Lock(); defer service.mu.Unlock(); return len(service.replies) == 1 })
	if service.replies[0] != ocSession+"|per_1|once" {
		t.Fatalf("reply = %q", service.replies[0])
	}
	// The service's own replied event for the same request adds nothing.
	service.frames <- frame("permission.replied", `{"sessionID":"`+ocSession+`","requestID":"per_1","reply":"once"}`)
	time.Sleep(50 * time.Millisecond)
	if err := hub.Decide("%5", "per_1", "once", time.Now()); err == nil {
		t.Fatal("an answered approval was still pending")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.replies) != 1 {
		t.Fatalf("replies = %v", service.replies)
	}
}

func TestOpencodeReconnectSettlesATurnThatEndedWhileNoStreamWasOpen(t *testing.T) {
	service := newFakeOpencode()
	service.outcome = opencodeapi.Outcome{Status: "failed", ErrorType: "provider.rate-limit", ErrorText: "slow down"}
	hub := agentevents.NewHub()
	hub.Publish(agentevents.Event{WorkerID: "%5", Kind: agentevents.KindTurnStarted, At: time.Now(), TurnID: "evt_old", Status: agentevents.TurnRunning})
	service.permissions = []opencodeapi.Permission{{ID: "per_late", SessionID: ocSession, Action: "edit", Resources: []string{"main.go"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := newOpencodeEvents(ctx, hub, func(*classifier.Worker) string { return work.WorkerProviderOpenCode })
	events.open = func(context.Context) (opencodeService, error) { return service, nil }
	events.Observe(watcher.SessionEvent{Type: "worker_discovered", WorkerID: "%5", Worker: &classifier.Worker{ID: "%5", Command: "opencode --session " + ocSession}})
	waitFor(t, "the turn to settle", func() bool {
		s, _ := hub.State("%5")
		return s.Current != nil && s.Current.Status == agentevents.TurnFailed
	})
	s, _ := hub.State("%5")
	if s.Current.ID != "evt_old" || s.Current.Error == nil || s.Current.Error.Class != agentevents.ErrorRateLimit {
		t.Fatalf("turn = %+v", s.Current)
	}
	if len(s.Approvals) != 1 || s.Approvals[0].ID != "per_late" || s.Approvals[0].Kind != "file_change" {
		t.Fatalf("approvals = %+v", s.Approvals)
	}
}

func TestOpencodeDropsTheServiceConnectionWhenNoWorkerIsFollowed(t *testing.T) {
	service := newFakeOpencode()
	events, _ := startOpencode(t, service)
	events.Observe(watcher.SessionEvent{Type: "worker_removed", WorkerID: "%5"})
	waitFor(t, "the connection to be released", func() bool { return events.current() == nil })
}

func TestFirstHandledOffersEachFollowerInTurn(t *testing.T) {
	var order []string
	a := func(string, string, string, func() error) (bool, error) {
		order = append(order, "a")
		return false, nil
	}
	b := func(string, string, string, func() error) (bool, error) { order = append(order, "b"); return true, nil }
	c := func(string, string, string, func() error) (bool, error) { order = append(order, "c"); return true, nil }
	if handled, _ := firstHandled(a, b, c)("%1", "", "", nil); !handled || fmt.Sprint(order) != "[a b]" {
		t.Fatalf("handled %v after %v", handled, order)
	}
	if handled, _ := firstHandled(a)("%1", "", "", nil); handled {
		t.Fatal("no follower took the Worker, yet it was handled")
	}
}
