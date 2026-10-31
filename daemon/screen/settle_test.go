package screen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A virtual clock: sleeping moves time, reading costs none.
type fakeClock struct{ at time.Duration }

func (c *fakeClock) now() time.Time { return time.Unix(1000, 0).Add(c.at) }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.at += d
	return nil
}

func scripted(clock *fakeClock, prints ...AppPrint) (settler, *int) {
	reads := 0
	return settler{first: 300 * time.Millisecond, every: 100 * time.Millisecond, max: 2500 * time.Millisecond,
		now: clock.now, sleep: clock.sleep,
		print: func(context.Context) (AppPrint, error) {
			p := prints[min(reads, len(prints)-1)]
			reads++
			return p, nil
		}}, &reads
}

func TestSettleWaitsForTwoAlikeReads(t *testing.T) {
	still := AppPrint{Windows: 1, Children: 4, Title: "Notes"}
	clock := &fakeClock{}
	st, reads := scripted(clock, still)
	if settled, err := st.quiet(context.Background()); !settled || err != nil {
		t.Fatalf("a quiet app: %v %v", settled, err)
	}
	if *reads != 2 || clock.at != 400*time.Millisecond {
		t.Fatalf("a quiet app takes the first wait and one poll: %d reads, %v", *reads, clock.at)
	}

	// A sheet opens (the children change twice) and then holds still.
	clock = &fakeClock{}
	st, reads = scripted(clock, AppPrint{Windows: 1, Children: 4}, AppPrint{Windows: 1, Children: 5}, AppPrint{Windows: 1, Children: 6}, AppPrint{Windows: 1, Children: 6})
	if settled, _ := st.quiet(context.Background()); !settled || *reads != 4 || clock.at != 600*time.Millisecond {
		t.Fatalf("a changing app: settled %v after %d reads, %v", settled, *reads, clock.at)
	}
}

func TestSettleGivesUpAtItsCap(t *testing.T) {
	clock := &fakeClock{}
	n := 0
	st := settler{first: 300 * time.Millisecond, every: 100 * time.Millisecond, max: 2500 * time.Millisecond, now: clock.now, sleep: clock.sleep,
		print: func(context.Context) (AppPrint, error) { n++; return AppPrint{Children: n}, nil }}
	settled, err := st.quiet(context.Background())
	if settled || err != nil || clock.at != 2500*time.Millisecond {
		t.Fatalf("an app that never settles: %v %v after %v", settled, err, clock.at)
	}
}

func TestSettleEndsWhereItCannotLook(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	st := settler{first: 300 * time.Millisecond, every: 100 * time.Millisecond, max: 2500 * time.Millisecond, now: clock.now, sleep: clock.sleep,
		print: func(context.Context) (AppPrint, error) {
			calls++
			if calls == 2 {
				return AppPrint{}, errors.New("the app quit")
			}
			return AppPrint{Children: calls}, nil
		}}
	if settled, err := st.quiet(context.Background()); settled || err != nil || calls != 2 {
		t.Fatalf("%v %v after %d reads", settled, err, calls)
	}
}

func TestSettleEndsWhenTheWaitIsEnded(t *testing.T) {
	gone := errors.New("a person took over")
	st := settler{first: time.Second, every: time.Second, max: time.Minute, now: time.Now,
		sleep: func(context.Context, time.Duration) error { return gone },
		print: func(context.Context) (AppPrint, error) {
			t.Fatal("looked after the wait ended")
			return AppPrint{}, nil
		}}
	if _, err := st.quiet(context.Background()); !errors.Is(err, gone) {
		t.Fatalf("%v", err)
	}
}

// Short delays, so the real waits of a test stay short.
func quickSettle(t *testing.T) {
	t.Helper()
	first, every, max, recheck := settleFirst, settleEvery, settleMax, waitForRecheck
	settleFirst, settleEvery, settleMax, waitForRecheck = 20*time.Millisecond, 10*time.Millisecond, 300*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { settleFirst, settleEvery, settleMax, waitForRecheck = first, every, max, recheck })
}

// An element action answers when the app has gone quiet, not after a fixed
// moment: an app that goes on drawing is waited for, and its last row is in
// the answer. The app changes with each look at it, so no timing is involved.
func TestElementActionWaitsForTheAppToGoQuiet(t *testing.T) {
	quickSettle(t)
	s, backend, _ := newAgentService(t)
	backend.RunningApps = []FakeApp{bigApp()}
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		if app.Elements[index].Title != "Open" {
			return
		}
		first := backend.Prints + 1
		backend.OnPrint = func(app *FakeApp, call int) {
			if n := call - first; n >= 0 && n < 6 {
				app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("Late %d", n), Enabled: true, Actions: []string{"AXPress"}})
			}
		}
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	answer := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, state, `Button "Open"`), ElementAction: "press"}).Text
	if !strings.Contains(answer, `Button "Late 5"`) {
		t.Fatalf("the last row isn't in the answer:\n%s", answer)
	}
	// Six looks that differ, then one that agrees with the one before.
	if backend.Prints != 7 {
		t.Fatalf("%d fingerprints", backend.Prints)
	}
}

// wait_for watches the fingerprint and reads the app when it changes, and
// still finds what appears without changing it. The app changes at a look,
// not at a time.
func TestWaitForReadsOnChangeAndAtTheRecheck(t *testing.T) {
	quickSettle(t)
	s, backend, _ := newAgentService(t)
	backend.RunningApps = []FakeApp{bigApp()}
	onLook := func(n int, change func(app *FakeApp)) {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		at := backend.Prints + n
		backend.OnPrint = func(app *FakeApp, call int) {
			if call == at {
				change(app)
			}
		}
	}

	// A sheet appears under the window at the second look: the fingerprint
	// changes, and the app is read again.
	onLook(2, func(app *FakeApp) {
		app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXSheet", Title: "Welcome", Enabled: true})
	})
	found := act(t, s, "%7", AgentAction{Action: "wait_for", Duration: 5, Match: &ElementMatch{Role: "Sheet", Title: "Welcome"}}).Text
	if !strings.HasPrefix(found, "Found [") {
		t.Fatalf("not found:\n%s", found)
	}
	backend.mu.Lock()
	reads, prints := backend.Reads, backend.Prints
	backend.mu.Unlock()
	if reads != 2 || prints < 3 {
		t.Fatalf("expected a read, a change seen and a read; got %d reads and %d fingerprints", reads, prints)
	}

	// A button deep in the window appears: no fingerprint changes, so the
	// recheck finds it.
	backend.mu.Lock()
	backend.RunningApps[0].Elements = append(backend.RunningApps[0].Elements, Element{Depth: 2, Role: "AXGroup", Title: "Deep", Enabled: true})
	backend.mu.Unlock()
	onLook(2, func(app *FakeApp) {
		app.Elements = append(app.Elements, Element{Depth: 3, Role: "AXButton", Title: "Buried", Enabled: true})
	})
	start := time.Now()
	if got := act(t, s, "%7", AgentAction{Action: "wait_for", Duration: 5, Match: &ElementMatch{Role: "Button", Title: "Buried"}}).Text; !strings.HasPrefix(got, "Found [") {
		t.Fatalf("a change the fingerprint can't see was missed:\n%s", got)
	}
	if took := time.Since(start); took < waitForRecheck || took > 3*time.Second {
		t.Fatalf("found after %v, expected about the recheck, %v", took, waitForRecheck)
	}

	// Nothing appears: it gives up at its limit, not before.
	backend.mu.Lock()
	backend.OnPrint = nil
	backend.mu.Unlock()
	start = time.Now()
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "wait_for", Duration: 0.5, Match: &ElementMatch{Role: "Button", Title: "Never"}})
	if err == nil || !strings.Contains(err.Error(), "did not appear") || time.Since(start) < 450*time.Millisecond {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
}
