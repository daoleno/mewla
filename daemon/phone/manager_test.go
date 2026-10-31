package phone

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if err != nil {
		return "other: " + err.Error()
	}
	return ""
}

func tap(m *Manager, worker string) (Reply, error) {
	return m.Do(context.Background(), worker, "Writer", OpTap, TapArgs{X: 5, Y: 6}, false)
}

func TestTheManagerRefusesWhatThePhoneCannotDo(t *testing.T) {
	m := NewManager(Options{Reconnect: -1})
	if _, err := tap(m, "w1"); code(err) != CodeNotConnected {
		t.Fatalf("no phone: %v", err)
	}
	if st := m.Status(); st.Connected || st.State != StateOffline {
		t.Fatalf("status without a phone: %+v", st)
	}

	for _, c := range []struct {
		state, want string
	}{
		{StateSetup, CodeNotReady},
		{StatePersonInCharge, CodePersonInControl},
		{StateStopped, CodeStopped},
		{"something new", CodeNotReady},
	} {
		_, conn := connect(t, m, c.state, okResult(nil))
		_, err := tap(m, "w1")
		if code(err) != c.want {
			t.Fatalf("state %q: want %s, got %v", c.state, c.want, err)
		}
		if len(conn.requests()) != 0 {
			t.Fatalf("state %q: a request reached the phone: %v", c.state, conn.ops())
		}
		if !strings.Contains(err.Error(), "Stop") && !strings.Contains(err.Error(), "stop") {
			t.Fatalf("the refusal does not tell the agent to stop: %v", err)
		}
	}

	_, conn := connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}
	if ops := conn.ops(); len(ops) != 1 || ops[0] != OpTap {
		t.Fatalf("ops %v", ops)
	}
	if req := conn.requests()[0]; req.Worker != "w1" || req.WorkerName != "Writer" || req.ID == "" || req.Type != TypeRequest {
		t.Fatalf("request %+v", req)
	}
}

func TestTheStatesThePhoneReportsGateActions(t *testing.T) {
	m := NewManager(Options{})
	link, _ := connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}

	sendFrame(t, link, State{Type: TypeState, State: StatePersonInCharge, W: 1080, H: 2400})
	if _, err := tap(m, "w1"); code(err) != CodePersonInControl {
		t.Fatalf("person in control: %v", err)
	}
	if st := m.Status(); st.State != StatePersonInCharge || st.W != 1080 || st.H != 2400 || st.Model != "Pixel 8" || st.SDK != 35 {
		t.Fatalf("status %+v", st)
	}
	// Ready again: the phone says whose hands the screen is in.
	sendFrame(t, link, State{Type: TypeState, State: StateReady})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("handed back by state: %v", err)
	}

	// A stop is the person's and holds until they give it back, whatever else the phone reports.
	sendFrame(t, link, State{Type: TypeState, State: StateStopped})
	sendFrame(t, link, State{Type: TypeState, State: StateReady})
	if _, err := tap(m, "w1"); code(err) != CodeStopped {
		t.Fatalf("stop was forgotten: %v", err)
	}
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventGiveBack})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("after give_back: %v", err)
	}

	// Taking over ends the agent's use at once, and give_back returns it.
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventTakeOver})
	if _, err := tap(m, "w1"); code(err) != CodePersonInControl {
		t.Fatalf("take_over: %v", err)
	}
	if st := m.Status(); st.Active != nil || st.State != StatePersonInCharge {
		t.Fatalf("status after take_over %+v", st)
	}
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventGiveBack})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("after give_back: %v", err)
	}
	if err := link.Receive([]byte("not json")); err == nil {
		t.Fatal("a frame that is not JSON was accepted")
	}
	if err := link.Receive([]byte(`{"type":"from-the-future"}`)); err != nil {
		t.Fatalf("an unknown frame type must be ignored: %v", err)
	}
}

func TestAResultTheAgentCannotUseCarriesItsCode(t *testing.T) {
	m := NewManager(Options{})
	var res *Result
	connect(t, m, StateReady, func(Request) *Result { return res })
	for _, c := range []struct {
		res  Result
		want string
		text string
	}{
		{Result{Code: CodePersonInControl}, CodePersonInControl, "do not retry"},
		{Result{Code: CodeStopped, Error: "You pressed Stop."}, CodeStopped, "You pressed Stop. Stop and report"},
		{Result{Code: CodeNotInstalled}, CodeNotInstalled, "not installed"},
		{Result{Code: CodeInUse}, CodeInUse, "using that app"},
		{Result{Code: CodeNotReady}, CodeNotReady, "isn't ready"},
		{Result{Error: "boom"}, CodeFailed, "boom"},
	} {
		r := c.res
		res = &r
		_, err := tap(m, "w1")
		if code(err) != c.want || !strings.Contains(err.Error(), c.text) {
			t.Fatalf("%+v: %v", c.res, err)
		}
	}
	// A screen-off answer still carries its picture next to the error.
	res = &Result{Code: CodeScreenOff, Image: "aGk="}
	reply, err := tap(m, "w1")
	if code(err) != CodeScreenOff || reply.Image != "aGk=" {
		t.Fatalf("screen_off: %+v %v", reply, err)
	}
}

func TestStoppingEndsTheRequestInFlight(t *testing.T) {
	m := NewManager(Options{})
	var slow atomic.Bool
	slow.Store(true)
	link, conn := connect(t, m, StateReady, func(r Request) *Result {
		if r.Op == OpTap && slow.Load() {
			return nil // the phone is slow
		}
		return &Result{OK: true}
	})
	done := make(chan error, 1)
	go func() {
		_, err := tap(m, "w1")
		done <- err
	}()
	eventually(t, "the tap to be sent", func() bool { return len(conn.ops()) == 1 })
	if st := m.Status(); st.Active == nil || st.Active.Worker != "w1" || st.Active.Name != "Writer" || st.Active.Since.IsZero() {
		t.Fatalf("active agent %+v", st.Active)
	}

	m.Stop()
	select {
	case err := <-done:
		if code(err) != CodeStopped {
			t.Fatalf("the in-flight request ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not end the request in flight")
	}
	if st := m.Status(); st.State != StateStopped || st.Active != nil {
		t.Fatalf("status after stop %+v", st)
	}
	if _, err := tap(m, "w1"); code(err) != CodeStopped {
		t.Fatalf("an action after stop: %v", err)
	}
	// The phone is told to free the agent's display.
	eventually(t, "release", func() bool { return strings.Join(conn.ops(), ",") == "tap,release" })

	// The person's own stop, from the phone, is the same.
	slow.Store(false)
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventGiveBack})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("after give_back: %v", err)
	}
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventStop})
	if _, err := tap(m, "w1"); code(err) != CodeStopped {
		t.Fatalf("stop event: %v", err)
	}
}

func TestANewConnectionReplacesTheOldAndSaysWhy(t *testing.T) {
	m := NewManager(Options{})
	first, a := connect(t, m, StateReady, func(Request) *Result { return nil })
	done := make(chan error, 1)
	go func() {
		_, err := tap(m, "w1")
		done <- err
	}()
	eventually(t, "the tap to reach the first phone", func() bool { return len(a.ops()) == 1 })

	second := Hello{Type: TypeHello, Version: Version, Model: "Pixel 9", SDK: 36, State: StateReady}
	b := &fakeConn{handle: okResult(nil)}
	link, err := m.Attach(second, b)
	if err != nil {
		t.Fatal(err)
	}
	b.link = link
	if closes := a.closes(); len(closes) != 1 || closes[0].code != CloseReplaced || closes[0].reason == "" {
		t.Fatalf("the old connection was told %v", closes)
	}
	select {
	case err := <-done:
		if code(err) != CodeLost {
			t.Fatalf("the old request ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the old request was left waiting")
	}
	if st := m.Status(); st.Model != "Pixel 9" || !st.Connected {
		t.Fatalf("status %+v", st)
	}
	// Whatever the old connection says now is nobody's business.
	sendFrame(t, first, State{Type: TypeState, State: StateSetup})
	sendFrame(t, first, Event{Type: TypeEvent, Kind: EventStop})
	first.Detach()
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("the new phone: %v", err)
	}
	if len(b.ops()) != 1 {
		t.Fatalf("ops on the new phone %v", b.ops())
	}
	if m.Generation() != 2 {
		t.Fatalf("generation %d", m.Generation())
	}
	// A phone that speaks another protocol is turned away and the current one kept.
	c := &fakeConn{}
	if _, err := m.Attach(Hello{Type: TypeHello, Version: Version + 1}, c); err == nil {
		t.Fatal("a protocol this daemon does not know was accepted")
	}
	if len(b.closes()) != 0 || m.Generation() != 2 {
		t.Fatal("the current connection was disturbed by a refused one")
	}
	link.Detach()
	if st := m.Status(); st.Connected || st.State != StateOffline {
		t.Fatalf("after detach %+v", st)
	}
}

func TestADisconnectEndsTheRequestInFlight(t *testing.T) {
	m := NewManager(Options{})
	link, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
	done := make(chan error, 1)
	go func() {
		_, err := tap(m, "w1")
		done <- err
	}()
	eventually(t, "the tap", func() bool { return len(conn.ops()) == 1 })
	link.Detach()
	if err := <-done; code(err) != CodeLost {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestOneAgentUsesThePhoneAtATime(t *testing.T) {
	m := NewManager(Options{})
	connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := tap(m, "w2"); code(err) != CodeBusy || !strings.Contains(err.Error(), "Writer") && !strings.Contains(err.Error(), "w1") {
		t.Fatalf("a second agent: %v", err)
	}
}

func TestRequestsAreSentOneAtATime(t *testing.T) {
	m := NewManager(Options{})
	link, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := tap(m, "w1"); err != nil {
				t.Error(err)
			}
		}()
	}
	eventually(t, "the first request", func() bool { return len(conn.requests()) == 1 })
	time.Sleep(60 * time.Millisecond)
	if n := len(conn.requests()); n != 1 {
		t.Fatalf("%d requests in flight at once", n)
	}
	answer := func(id string) { sendFrame(t, link, Result{Type: TypeResult, ID: id, OK: true}) }
	answer("not-a-request") // late or stray answers change nothing
	answer(conn.requests()[0].ID)
	eventually(t, "the second request", func() bool { return len(conn.requests()) == 2 })
	answer(conn.requests()[1].ID)
	wg.Wait()
}

func TestAnAgentThatGoesQuietGivesTheDisplayBack(t *testing.T) {
	clock := newClock()
	m := NewManager(Options{Clock: clock})
	_, conn := connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}
	since := m.Status().Active.Since
	clock.Advance(30 * time.Second)
	if _, err := tap(m, "w1"); err != nil { // an action restarts the minute
		t.Fatal(err)
	}
	clock.Advance(59 * time.Second)
	if st := m.Status(); st.Active == nil || !st.Active.Since.Equal(since) {
		t.Fatalf("the agent was dropped early or its start moved: %+v", st.Active)
	}
	if ops := conn.ops(); len(ops) != 2 {
		t.Fatalf("released early: %v", ops)
	}
	clock.Advance(time.Second)
	eventually(t, "release", func() bool { return strings.Join(conn.ops(), ",") == "tap,tap,release" })
	if st := m.Status(); st.Active != nil {
		t.Fatalf("still active %+v", st.Active)
	}
	// Another agent may have the phone now.
	if _, err := tap(m, "w2"); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); st.Active == nil || st.Active.Worker != "w2" {
		t.Fatalf("active %+v", st.Active)
	}
}

func TestAWorkerThatEndsGivesTheDisplayBackAtOnce(t *testing.T) {
	clock := newClock()
	m := NewManager(Options{Clock: clock})
	running := true
	var asked []string
	var mu sync.Mutex
	m.SetAlive(func(worker string) bool {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, worker)
		return running
	})
	_, conn := connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}
	// A running Worker keeps the phone well past the check, and long before the idle wait is over.
	clock.Advance(DefaultAliveEvery)
	clock.Advance(DefaultAliveEvery)
	if st := m.Status(); st.Active == nil || len(conn.ops()) != 1 {
		t.Fatalf("a running Worker lost the phone: %+v %v", st.Active, conn.ops())
	}
	mu.Lock()
	running = false
	mu.Unlock()
	// One miss is not enough: it may be a moment of the Worker changing state.
	clock.Advance(DefaultAliveEvery)
	if st := m.Status(); st.Active == nil {
		t.Fatalf("released on a single miss")
	}
	clock.Advance(DefaultAliveEvery)
	eventually(t, "release", func() bool { return strings.Join(conn.ops(), ",") == "tap,release" })
	if st := m.Status(); st.Active != nil {
		t.Fatalf("still active %+v", st.Active)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, w := range asked {
		if w != "w1" {
			t.Fatalf("asked about %q", w)
		}
	}
}

func TestAReleaseOwedWhileThePhoneIsInSetupIsPaidWhenItIsReadyAgain(t *testing.T) {
	clock := newClock()
	m := NewManager(Options{Clock: clock})
	m.SetAlive(func(string) bool { return false })
	link, conn := connect(t, m, StateReady, okResult(nil))
	if _, err := tap(m, "w1"); err != nil {
		t.Fatal(err)
	}
	// The phone drops into setup, and the Worker ends meanwhile: nothing can be sent now.
	sendFrame(t, link, State{Type: TypeState, State: StateSetup, Problem: "Wireless debugging is off"})
	clock.Advance(DefaultAliveEvery)
	clock.Advance(DefaultAliveEvery)
	eventually(t, "worker dropped", func() bool { return m.Status().Active == nil })
	if ops := conn.ops(); strings.Join(ops, ",") != "tap" {
		t.Fatalf("a phone in setup was sent %v", ops)
	}
	// Ready again: the display that was owed goes back, so the App stops showing a dead Worker.
	sendFrame(t, link, State{Type: TypeState, State: StateReady})
	eventually(t, "owed release", func() bool { return strings.Join(conn.ops(), ",") == "tap,release" })
}

func TestTheDisplayIsNotReleasedFromUnderTheAgentOrThePerson(t *testing.T) {
	clock := newClock()
	m := NewManager(Options{Clock: clock})
	link, conn := connect(t, m, StateReady, func(r Request) *Result {
		if r.Op == OpHandoff {
			return nil
		}
		return &Result{OK: true}
	})
	done := make(chan error, 1)
	go func() {
		_, err := m.Do(context.Background(), "w1", "Writer", OpHandoff, HandoffArgs{Reason: "Sign in"}, false)
		done <- err
	}()
	eventually(t, "the handoff", func() bool { return len(conn.ops()) == 1 })
	clock.Advance(10 * time.Minute) // the person is taking their time; the agent is not idle
	time.Sleep(30 * time.Millisecond)
	if len(conn.ops()) != 1 {
		t.Fatalf("released during a handoff: %v", conn.ops())
	}
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventHandedOff, ID: conn.requests()[0].ID})
	if err := <-done; err != nil {
		t.Fatalf("handoff: %v", err)
	}
}

func TestAHandoffEndsWhenThePersonSaysTheyAreDone(t *testing.T) {
	for _, how := range []string{"event", "result"} {
		m := NewManager(Options{})
		link, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
		type answer struct {
			reply Reply
			err   error
		}
		done := make(chan answer, 1)
		go func() {
			reply, err := m.Do(context.Background(), "w1", "Writer", OpHandoff, HandoffArgs{Reason: "Sign in"}, false)
			done <- answer{reply, err}
		}()
		eventually(t, "the handoff", func() bool { return len(conn.ops()) == 1 })
		id := conn.requests()[0].ID
		// An event for another request does nothing.
		sendFrame(t, link, Event{Type: TypeEvent, Kind: EventHandedOff, ID: "someone-else"})
		select {
		case <-done:
			t.Fatal("a stranger's handed_off ended the handoff")
		case <-time.After(30 * time.Millisecond):
		}
		if how == "event" {
			sendFrame(t, link, Event{Type: TypeEvent, Kind: EventHandedOff, ID: id})
		} else {
			sendFrame(t, link, Result{Type: TypeResult, ID: id, OK: true, Screen: &Screen{W: 10, H: 10}})
		}
		a := <-done
		if a.err != nil || !a.reply.OK {
			t.Fatalf("%s: %+v %v", how, a.reply, a.err)
		}
		if how == "result" && a.reply.Screen == nil {
			t.Fatal("the result's screen was lost")
		}
	}
}

// The phone reports person_in_control as soon as it asks the person for help, and again as ready
// when they are done. Neither may end the handoff that caused it.
func TestAHandoffSurvivesThePhoneSayingThePersonIsInCharge(t *testing.T) {
	m := NewManager(Options{})
	link, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
	type answer struct {
		reply Reply
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		reply, err := m.Do(context.Background(), "w1", "Writer", OpHandoff, HandoffArgs{Reason: "Sign in"}, false)
		done <- answer{reply, err}
	}()
	eventually(t, "the handoff", func() bool { return len(conn.ops()) == 1 })
	id := conn.requests()[0].ID
	sendFrame(t, link, State{Type: TypeState, State: StatePersonInCharge, W: 720, H: 1600})
	select {
	case a := <-done:
		t.Fatalf("the phone's own state ended the handoff: %+v %v", a.reply, a.err)
	case <-time.After(50 * time.Millisecond):
	}
	// While they work, the screen is theirs: another agent is told so.
	if _, err := tap(m, "w2"); code(err) != CodeBusy && code(err) != CodePersonInControl {
		t.Fatalf("another agent during a handoff: %v", err)
	}
	// The phone's Done is the event first and its own "ready" frame after: the agent's very next
	// action, which comes in between, must already have the phone.
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventHandedOff, ID: id})
	if a := <-done; a.err != nil || !a.reply.OK {
		t.Fatalf("handoff: %+v %v", a.reply, a.err)
	}
	conn.mu.Lock()
	conn.handle = func(Request) *Result { return &Result{OK: true} }
	conn.mu.Unlock()
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("the agent's next action right after Done: %v", err)
	}
	sendFrame(t, link, State{Type: TypeState, State: StateReady, W: 720, H: 1600})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("after the phone's own ready frame: %v", err)
	}
}

func TestTimeoutsFollowTheOp(t *testing.T) {
	m := NewManager(Options{Timeout: 40 * time.Millisecond})
	_, conn := connect(t, m, StateReady, func(r Request) *Result {
		if r.Op == OpTap {
			return nil
		}
		return &Result{OK: true}
	})
	start := time.Now()
	if _, err := tap(m, "w1"); code(err) != CodeTimeout {
		t.Fatalf("a silent phone: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timed out late")
	}
	eventually(t, "release after a timeout", func() bool { return strings.Join(conn.ops(), ",") == "tap,release" })

	// A wait is given its own length on top.
	if got := m.timeoutFor(OpWait, []byte(`{"ms":5000}`)); got != 5*time.Second+40*time.Millisecond {
		t.Fatalf("wait timeout %v", got)
	}
	if got := m.timeoutFor(OpHandoff, []byte(`{"reason":"x"}`)); got != HandoffDefault+handoffGrace {
		t.Fatalf("handoff default %v", got)
	}
	if got := m.timeoutFor(OpHandoff, []byte(`{"reason":"x","timeout_ms":2000}`)); got != 2*time.Second+handoffGrace {
		t.Fatalf("handoff timeout %v", got)
	}
	if got := m.timeoutFor(OpScreen, nil); got != 40*time.Millisecond {
		t.Fatalf("screen timeout %v", got)
	}
	// The caller's own deadline is the same story told to the agent.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	m2 := NewManager(Options{})
	connect(t, m2, StateReady, func(Request) *Result { return nil })
	if _, err := m2.Do(ctx, "w1", "", OpTap, TapArgs{}, false); code(err) != CodeTimeout {
		t.Fatalf("caller deadline: %v", err)
	}
}

func TestTheDaemonsReleaseNeverTakesThePersonsScreen(t *testing.T) {
	m := NewManager(Options{})
	_, conn := connect(t, m, StatePersonInCharge, okResult(nil))
	m.Release()
	time.Sleep(50 * time.Millisecond)
	if len(conn.requests()) != 0 {
		t.Fatalf("released the person's screen: %v", conn.ops())
	}
}

func TestAnAgentThatCannotHaveThePhoneIsToldAtOnceNotAfterAHandoff(t *testing.T) {
	m := NewManager(Options{})
	link, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
	done := make(chan error, 1)
	go func() {
		_, err := m.Do(context.Background(), "w1", "Writer", OpHandoff, HandoffArgs{Reason: "Sign in"}, false)
		done <- err
	}()
	eventually(t, "the handoff", func() bool { return len(conn.ops()) == 1 })

	start := time.Now()
	if _, err := tap(m, "w2"); code(err) != CodeBusy || !strings.Contains(err.Error(), "Writer") {
		t.Fatalf("another agent during a handoff: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the refusal waited %v behind the handoff", time.Since(start))
	}
	// The same agent's next action does wait its turn, and a caller that gives up gets an answer.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.Do(ctx, "w1", "Writer", OpTap, TapArgs{}, false); code(err) != CodeTimeout {
		t.Fatalf("waiting for the turn: %v", err)
	}
	// The person stops it while the handoff waits: the next agent is refused for that reason.
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventStop})
	if err := <-done; code(err) != CodeStopped {
		t.Fatalf("handoff: %v", err)
	}
	if _, err := tap(m, "w2"); code(err) != CodeStopped {
		t.Fatalf("after stop: %v", err)
	}
}

func TestAnActionWaitsForThePhoneToReconnect(t *testing.T) {
	m := NewManager(Options{Reconnect: 2 * time.Second})
	link, _ := connect(t, m, StateReady, okResult(nil))
	link.Detach()

	done := make(chan error, 1)
	go func() {
		_, err := tap(m, "w1")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the action did not wait for the phone: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	_, conn := connect(t, m, StateReady, okResult(nil))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("after the phone came back: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the action never ran after the phone came back")
	}
	if ops := conn.ops(); len(ops) != 1 || ops[0] != OpTap {
		t.Fatalf("ops %v", ops)
	}
}

func TestAnActionGivesUpWhenThePhoneStaysAway(t *testing.T) {
	m := NewManager(Options{Reconnect: 80 * time.Millisecond})
	start := time.Now()
	if _, err := tap(m, "w1"); code(err) != CodeNotConnected {
		t.Fatalf("no phone: %v", err)
	}
	if d := time.Since(start); d < 70*time.Millisecond || d > time.Second {
		t.Fatalf("waited %v", d)
	}
	// The daemon's own release never waits for a phone.
	start = time.Now()
	if _, err := m.Do(context.Background(), "", "", OpRelease, nil, false); err != nil || time.Since(start) > 50*time.Millisecond {
		t.Fatalf("release: %v after %v", err, time.Since(start))
	}
}

func TestAPhoneInSetupSaysWhatToDoAndWhyWhenItKnows(t *testing.T) {
	m := NewManager(Options{})
	link, conn := connect(t, m, StateSetup, okResult(nil))
	_, err := tap(m, "w1")
	if code(err) != CodeNotReady || !strings.Contains(err.Error(), "open Mewla → Phone") || strings.Contains(err.Error(), "(") {
		t.Fatalf("no reason known: %v", err)
	}
	// An agent waits and tries again for a short while before it gives up, since the phone is
	// often ready within a minute of the person opening Mewla.
	if !strings.Contains(err.Error(), "try again") || strings.Contains(err.Error(), "Tell them, and stop") {
		t.Fatalf("not-ready advice: %v", err)
	}
	sendFrame(t, link, State{Type: TypeState, State: StateSetup, Problem: "Wireless debugging is off"})
	_, err = tap(m, "w1")
	if code(err) != CodeNotReady || !strings.Contains(err.Error(), "open Mewla → Phone") || !strings.Contains(err.Error(), "(Wireless debugging is off)") {
		t.Fatalf("with the phone's reason: %v", err)
	}
	if len(conn.ops()) != 0 {
		t.Fatalf("a phone in setup was sent %v", conn.ops())
	}
	// Ready again: nothing is refused, and the old reason is gone.
	sendFrame(t, link, State{Type: TypeState, State: StateReady})
	if _, err := tap(m, "w1"); err != nil {
		t.Fatalf("after it became ready: %v", err)
	}
}
