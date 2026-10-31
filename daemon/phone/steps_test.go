package phone

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// quickClock makes a step's wait for an element take no real time.
func quickClock(a *Agent) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	a.sleep = func(context.Context, time.Duration) error { return nil }
}

func TestAFlowRunsInOneCallAndShowsOnlyTheLastScreen(t *testing.T) {
	home := appScreen(node("ImageButton", "search", "", "Search", tap1, [4]int{600, 100, 700, 200}), node("TextView", "", "Inbox", "", plain, [4]int{0, 300, 500, 400}))
	box := appScreen(node("EditText", "q", "", "", field, [4]int{100, 100, 900, 200}), node("TextView", "", "Recent searches", "", plain, [4]int{0, 300, 500, 400}))
	box.Windows[0].Nodes[0].H = "Search for anything"
	results := appScreen(node("TextView", "title", "Sylvan Park", "", tap1, [4]int{0, 300, 1080, 500}), node("TextView", "title", "Sylvan Cafe", "", tap1, [4]int{0, 500, 1080, 700}))
	opened := appScreen(node("TextView", "", "Sylvan Park: the page", "", plain, [4]int{0, 0, 500, 100}))
	p := &scriptedPhone{screen: home, after: map[string]*Screen{OpTap: box, OpText: results}}
	a, _, conn := newAgent(t, StateReady, p)
	quickClock(a)

	// Tapping the first result is the last step: it is the only screen the answer shows.
	p.after[OpKey] = opened
	res := act(t, a, AgentAction{Action: ActSteps, Steps: []AgentAction{
		{Action: ActTap, Match: "Search"},
		{Action: ActType, Text: "Sylvan", Match: "Search for anything"},
		{Action: ActKey, Key: "enter"},
	}})
	if strings.Contains(res.Text, "Recent searches") || strings.Contains(res.Text, "Inbox") || !strings.Contains(res.Text, "Sylvan Park: the page") {
		t.Fatalf("only the last screen is shown:\n%s", res.Text)
	}
	for _, want := range []string{"1. tap \"Search\": done", "2. type 6 characters: done", "3. key enter: done"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("missing %q:\n%s", want, res.Text)
		}
	}
	// The first tap needed a reading to find "Search" on; the field was found on the screen the tap left.
	if got := strings.Join(conn.ops(), ","); got != "screen,tap,tap,text,key" {
		t.Fatalf("ops %s", got)
	}
	if first := args[TapArgs](t, conn.requests()[1]); first != (TapArgs{X: 650, Y: 150}) {
		t.Fatalf("tapped %+v", first)
	}
}

func TestAStepThatCannotBeDoneEndsTheBatchAndShowsTheScreen(t *testing.T) {
	home := appScreen(node("TextView", "", "Inbox", "", plain, [4]int{0, 300, 500, 400}))
	p := &scriptedPhone{screen: home}
	a, _, conn := newAgent(t, StateReady, p)
	quickClock(a)

	res := act(t, a, AgentAction{Action: ActSteps, Steps: []AgentAction{
		{Action: ActKey, Key: "back"},
		{Action: ActTap, Match: "Nowhere"},
		{Action: ActKey, Key: "enter"},
	}})
	if !strings.Contains(res.Text, "Step 2 (tap) failed") || !strings.Contains(res.Text, "1 step(s) after it did not run") || !strings.Contains(res.Text, "Inbox") {
		t.Fatalf("the failure names the step and shows the screen:\n%s", res.Text)
	}
	for _, op := range conn.ops() {
		if op == OpTap {
			t.Fatalf("nothing was tapped: %v", conn.ops())
		}
	}
	if keys := strings.Count(strings.Join(conn.ops(), ","), "key"); keys != 1 {
		t.Fatalf("the step after the failure ran: %v", conn.ops())
	}
}

func TestWaitingForTextThatNeverComesFailsTheBatch(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "Loading", "", plain, [4]int{0, 0, 100, 100}))}
	a, _, _ := newAgent(t, StateReady, p)
	quickClock(a)
	res := act(t, a, AgentAction{Action: ActSteps, Steps: []AgentAction{
		{Action: ActWait, For: "Results", Seconds: 2},
		{Action: ActKey, Key: "enter"},
	}})
	if !strings.Contains(res.Text, "Step 1 (wait) failed") || !strings.Contains(res.Text, `"Results" did not appear`) {
		t.Fatalf("%s", res.Text)
	}
}

func TestARefusalInsideABatchIsAnErrorThatNamesTheStep(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}))}
	a, _, _ := newAgent(t, StateReady, p)
	p.result = map[string]*Result{OpKey: {OK: false, Code: CodePersonInControl, Error: "The person is using the agent's screen."}}
	_, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActSteps, Steps: []AgentAction{{Action: ActKey, Key: "back"}}})
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != CodePersonInControl || !strings.Contains(err.Error(), "step 1 (key)") {
		t.Fatalf("%v", err)
	}
}

func TestABatchHoldsOnlyActionsThatActOnTheScreen(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}))}
	a, _, conn := newAgent(t, StateReady, p)
	for _, bad := range [][]AgentAction{
		nil,
		{{Action: ActHandoff, Reason: "x"}},
		{{Action: ActSteps, Steps: []AgentAction{{Action: ActKey, Key: "back"}}}},
		make([]AgentAction, maxSteps+1),
	} {
		if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActSteps, Steps: bad}); err == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
	if len(conn.requests()) != 0 {
		t.Fatal("a refused batch reached the phone")
	}
}

func TestMatchPrefersAnExactTextThenWhatCanBePressed(t *testing.T) {
	e := NewElements()
	e.Observe(appScreen(
		node("TextView", "", "Search history", "", plain, [4]int{0, 0, 100, 50}),
		node("Button", "", "Search", "", tap1, [4]int{0, 60, 100, 110}),
		node("TextView", "", "Open", "", tap1, [4]int{0, 120, 100, 170}),
		node("TextView", "", "Open now", "", tap1, [4]int{0, 180, 100, 230}),
		node("TextView", "", "Reopen", "", plain, [4]int{0, 240, 100, 290}),
	))
	if el, found, err := e.Match("search"); err != nil || !found || el.Text != "Search" {
		t.Fatalf("exact beats part: %+v %v %v", el, found, err)
	}
	if el, found, err := e.Match("reopen"); err != nil || !found || el.Text != "Reopen" {
		t.Fatalf("%+v %v %v", el, found, err)
	}
	if _, found, err := e.Match("ope"); !found || err == nil || !strings.Contains(err.Error(), "matches several") {
		t.Fatalf("an ambiguous match is said so: %v %v", found, err)
	}
	if _, found, err := e.Match("zzz"); found || err != nil {
		t.Fatalf("%v %v", found, err)
	}
}
