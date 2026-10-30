package phone

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedPhone is a phone whose screen the test sets.
type scriptedPhone struct {
	mu     sync.Mutex
	screen *Screen
	apps   []App
	// after changes the screen once the named op has been answered.
	after map[string]*Screen
	// result overrides the answer to an op.
	result map[string]*Result
	jpeg   string
}

func (p *scriptedPhone) set(s *Screen) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.screen = s
}

func (p *scriptedPhone) handle(r Request) *Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	if res := p.result[r.Op]; res != nil {
		c := *res
		return &c
	}
	if next := p.after[r.Op]; next != nil {
		p.screen = next
	}
	res := &Result{OK: true}
	switch r.Op {
	case OpApps:
		res.Apps = p.apps
	case OpRelease:
	default:
		res.Screen = p.screen
	}
	if r.Image {
		res.Image = p.jpeg
	}
	return res
}

func newAgent(t *testing.T, state string, p *scriptedPhone) (*Agent, *Manager, *fakeConn) {
	t.Helper()
	m := NewManager(Options{})
	_, conn := connect(t, m, state, p.handle)
	a := NewAgent(m, nil)
	return a, m, conn
}

func act(t *testing.T, a *Agent, action AgentAction) AgentResult {
	t.Helper()
	res, err := a.Act(context.Background(), "w1", "Writer", action)
	if err != nil {
		t.Fatalf("%+v: %v", action, err)
	}
	return res
}

func args[T any](t *testing.T, r Request) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.Args, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

var jpegB64 = base64.StdEncoding.EncodeToString([]byte("\xff\xd8jpeg"))

func TestTapByElementTapsItsCenterAndAnswersWithTheNewScreen(t *testing.T) {
	home := appScreen(node("Button", "ok", "OK", "", tap1, [4]int{100, 200, 300, 400}))
	next := appScreen(node("TextView", "", "Done", "", plain, [4]int{0, 0, 100, 100}))
	p := &scriptedPhone{screen: home, after: map[string]*Screen{OpTap: next}}
	a, _, conn := newAgent(t, StateReady, p)

	read := act(t, a, AgentAction{Action: ActScreen})
	if !strings.Contains(read.Text, `[1] button "OK" (200,300) tap`) || len(read.Image) != 0 {
		t.Fatalf("screen:\n%s", read.Text)
	}
	res := act(t, a, AgentAction{Action: ActTap, Element: 1, Long: true})
	reqs := conn.requests()
	if len(reqs) != 2 || reqs[1].Op != OpTap || reqs[1].Image {
		t.Fatalf("requests %+v", reqs)
	}
	if got := args[TapArgs](t, reqs[1]); got != (TapArgs{X: 200, Y: 300, Long: true}) {
		t.Fatalf("tap %+v", got)
	}
	if !strings.Contains(res.Text, `[2] text "Done"`) {
		t.Fatalf("the answer is the screen after the tap:\n%s", res.Text)
	}
	// The element is gone: refused, and nothing reaches the phone.
	if _, err := a.Act(context.Background(), "w1", "Writer", AgentAction{Action: ActTap, Element: 1}); err == nil || !strings.Contains(err.Error(), "read the screen again") {
		t.Fatalf("a gone element: %v", err)
	}
	if len(conn.requests()) != 2 {
		t.Fatal("a refused id reached the phone")
	}
	// Coordinates are for what has no element; they must be on the display.
	act(t, a, AgentAction{Action: ActTap, X: 50, Y: 60})
	if got := args[TapArgs](t, conn.requests()[2]); got != (TapArgs{X: 50, Y: 60}) {
		t.Fatalf("tap %+v", got)
	}
	for _, bad := range []AgentAction{{Action: ActTap}, {Action: ActTap, X: 5000, Y: 5}} {
		if _, err := a.Act(context.Background(), "w1", "", bad); err == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}

func TestIdsBelongToTheConnectionTheyWereReadOn(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("Button", "ok", "OK", "", tap1, [4]int{0, 0, 10, 10}))}
	a, m, _ := newAgent(t, StateReady, p)
	act(t, a, AgentAction{Action: ActScreen})

	other := &fakeConn{handle: p.handle}
	link, err := m.Attach(hello(StateReady), other)
	if err != nil {
		t.Fatal(err)
	}
	other.link = link
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActTap, Element: 1}); err == nil {
		t.Fatal("an id from the old connection was accepted on the new one")
	}
	if len(other.requests()) != 0 {
		t.Fatal("a stale id reached the new phone")
	}
	if res := act(t, a, AgentAction{Action: ActScreen}); !strings.Contains(res.Text, "[1] button") {
		t.Fatalf("ids restart with the connection:\n%s", res.Text)
	}
}

func TestTypeTapsTheFieldFirstThenSendsTheText(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("EditText", "to", "", "", field, [4]int{0, 100, 1000, 200}))}
	a, _, conn := newAgent(t, StateReady, p)
	act(t, a, AgentAction{Action: ActScreen})
	act(t, a, AgentAction{Action: ActType, Element: 1, Text: "hello", Replace: true, Submit: true, Image: true})

	ops := conn.ops()
	if strings.Join(ops, ",") != "screen,tap,text,key" {
		t.Fatalf("ops %v", ops)
	}
	reqs := conn.requests()
	if got := args[TapArgs](t, reqs[1]); got.X != 500 || got.Y != 150 {
		t.Fatalf("tap %+v", got)
	}
	if got := args[TextArgs](t, reqs[2]); got != (TextArgs{Value: "hello", Replace: true}) {
		t.Fatalf("text %+v", got)
	}
	if got := args[KeyArgs](t, reqs[3]); got.Name != "enter" {
		t.Fatalf("key %+v", got)
	}
	// The picture, when asked for, comes with the last op.
	if reqs[1].Image || reqs[2].Image || !reqs[3].Image {
		t.Fatalf("image flags %v %v %v", reqs[1].Image, reqs[2].Image, reqs[3].Image)
	}

	// Without an element the text goes to whatever is focused; empty text needs replace.
	act(t, a, AgentAction{Action: ActType, Text: "more"})
	if got := conn.requests(); got[len(got)-1].Op != OpText {
		t.Fatalf("ops %v", conn.ops())
	}
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActType}); err == nil {
		t.Fatal("typing nothing was accepted")
	}
	act(t, a, AgentAction{Action: ActType, Replace: true})
}

func TestKeysAreNamedForThePhone(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}))}
	a, _, conn := newAgent(t, StateReady, p)
	for in, want := range map[string]string{"Back": "back", "return": "enter", "backspace": "delete", "TAB": "tab", "search": "search"} {
		act(t, a, AgentAction{Action: ActKey, Key: in})
		reqs := conn.requests()
		if got := args[KeyArgs](t, reqs[len(reqs)-1]); got.Name != want {
			t.Fatalf("%s -> %s", in, got.Name)
		}
	}
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActKey, Key: "home"}); err == nil {
		t.Fatal("an unknown key was accepted")
	}
}

func TestSwipeIsTheFingersWayThroughTheMiddle(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(
		node("RecyclerView", "list", "", "", scroll, [4]int{0, 500, 1080, 1500}),
		node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}),
	)}
	a, _, conn := newAgent(t, StateReady, p)
	swipe := func(action AgentAction) SwipeArgs {
		t.Helper()
		action.Action = ActSwipe
		act(t, a, action)
		reqs := conn.requests()
		return args[SwipeArgs](t, reqs[len(reqs)-1])
	}
	// The display's size is read first when the phone has not said it.
	if got := swipe(AgentAction{Direction: "up"}); got != (SwipeArgs{X1: 540, Y1: 1800, X2: 540, Y2: 600}) {
		t.Fatalf("up %+v", got)
	}
	if ops := conn.ops(); strings.Join(ops, ",") != "screen,swipe" {
		t.Fatalf("ops %v", ops)
	}
	if got := swipe(AgentAction{Direction: "down", Amount: 0.25}); got != (SwipeArgs{X1: 540, Y1: 900, X2: 540, Y2: 1500}) {
		t.Fatalf("down %+v", got)
	}
	if got := swipe(AgentAction{Direction: "left"}); got != (SwipeArgs{X1: 810, Y1: 1200, X2: 270, Y2: 1200}) {
		t.Fatalf("left %+v", got)
	}
	if got := swipe(AgentAction{Direction: "right", Amount: 5}); got != (SwipeArgs{X1: 108, Y1: 1200, X2: 972, Y2: 1200}) {
		t.Fatalf("right, amount clamped %+v", got)
	}
	// Within an element, the stroke stays in the element.
	if got := swipe(AgentAction{Direction: "up", Element: 1}); got != (SwipeArgs{X1: 540, Y1: 1250, X2: 540, Y2: 750}) {
		t.Fatalf("up in the list %+v", got)
	}
	if got := swipe(AgentAction{X1: 1, Y1: 2, X2: 3, Y2: 4}); got != (SwipeArgs{X1: 1, Y1: 2, X2: 3, Y2: 4}) {
		t.Fatalf("explicit %+v", got)
	}
	for _, bad := range []AgentAction{{Action: ActSwipe}, {Action: ActSwipe, Direction: "sideways"}} {
		if _, err := a.Act(context.Background(), "w1", "", bad); err == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}

func TestResolvingAnAppFromWhatTheAgentSays(t *testing.T) {
	apps := []App{
		{"com.google.android.apps.maps", "Maps"},
		{"com.google.android.apps.photos", "Photos"},
		{"com.google.android.gm", "Gmail"},
		{"com.google.android.apps.googleone", "Google One"},
		{"com.google.android.googlequicksearchbox", "Google"},
		{"com.android.chrome", "Chrome"},
		{"org.example.notes", "Notes"},
		{"org.example.notes.pro", "Notes Pro"},
		{"org.other.mapview", "Map View"},
	}
	for query, want := range map[string]string{
		"Maps":                         "com.google.android.apps.maps", // exact label, though "Map View" has a prefix
		"maps":                         "com.google.android.apps.maps",
		"com.android.chrome":           "com.android.chrome",
		"Notes":                        "org.example.notes", // exact before the prefix of Notes Pro
		"notes pro":                    "org.example.notes.pro",
		"Photo":                        "com.google.android.apps.photos", // prefix
		"mail":                         "com.google.android.gm",          // contains
		"Google":                       "com.google.android.googlequicksearchbox",
		"org.not.in.the.list":          "org.not.in.the.list", // a package may open though the launcher does not list it
		"  CHROME ":                    "com.android.chrome",
		"com.google.android.apps.maps": "com.google.android.apps.maps",
	} {
		app, err := ResolveApp(apps, query)
		if err != nil || app.Package != want {
			t.Errorf("%q -> %+v %v, want %s", query, app, err, want)
		}
	}
	// Several at the best level: the agent chooses.
	_, err := ResolveApp(apps, "Map")
	if err == nil || !strings.Contains(err.Error(), "Maps (com.google.android.apps.maps)") || !strings.Contains(err.Error(), "Map View (org.other.mapview)") {
		t.Fatalf("ambiguous: %v", err)
	}
	if _, err := ResolveApp(apps, "Spotify"); err == nil || !strings.Contains(err.Error(), "phone_apps") {
		t.Fatalf("unknown: %v", err)
	}
}

func TestLaunchReadsTheAppListOnceAndAgainOnlyForAnAppItDoesNotKnow(t *testing.T) {
	p := &scriptedPhone{
		screen: appScreen(node("TextView", "", "Home", "", plain, [4]int{0, 0, 10, 10})),
		apps:   []App{{"com.android.chrome", "Chrome"}, {"com.google.android.gm", "Gmail"}},
	}
	a, _, conn := newAgent(t, StateReady, p)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }

	res := act(t, a, AgentAction{Action: ActLaunch, App: "chrome"})
	act(t, a, AgentAction{Action: ActLaunch, App: "Gmail"})
	if strings.Join(conn.ops(), ",") != "apps,launch,launch" {
		t.Fatalf("ops %v", conn.ops())
	}
	if got := args[LaunchArgs](t, conn.requests()[1]); got.Package != "com.android.chrome" {
		t.Fatalf("launch %+v", got)
	}
	if !strings.Contains(res.Text, "Home") {
		t.Fatalf("launch answers with the screen:\n%s", res.Text)
	}

	// Not in the list: asked again only when the list is old enough to be out of date.
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActLaunch, App: "Spotify"}); err == nil {
		t.Fatal("an unknown app was launched")
	}
	if n := strings.Count(strings.Join(conn.ops(), ","), "apps"); n != 1 {
		t.Fatalf("the list was read %d times within seconds", n)
	}
	p.mu.Lock()
	p.apps = append(p.apps, App{"com.spotify.music", "Spotify"})
	p.mu.Unlock()
	clock = clock.Add(time.Minute)
	act(t, a, AgentAction{Action: ActLaunch, App: "Spotify"})
	if n := strings.Count(strings.Join(conn.ops(), ","), "apps"); n != 2 {
		t.Fatalf("the list was read %d times", n)
	}

	// phone_apps lists them by name.
	list := act(t, a, AgentAction{Action: ActApps})
	if !strings.Contains(list.Text, "Chrome — com.android.chrome") || !strings.Contains(list.Text, "3 apps") {
		t.Fatalf("apps:\n%s", list.Text)
	}

	// The phone says the app is not installed after all.
	p.mu.Lock()
	p.result = map[string]*Result{OpLaunch: {Code: CodeNotInstalled}}
	p.mu.Unlock()
	_, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActLaunch, App: "Gmail"})
	if code(err) != CodeNotInstalled || !strings.Contains(err.Error(), "Gmail") {
		t.Fatalf("not installed: %v", err)
	}
}

func TestAScreenThatIsOffAnswersWithAPictureNotAnError(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(), result: map[string]*Result{OpScreen: {Code: CodeScreenOff, Image: jpegB64}}}
	a, _, conn := newAgent(t, StateReady, p)
	res := act(t, a, AgentAction{Action: ActScreen})
	if string(res.Image) != "\xff\xd8jpeg" || !strings.Contains(res.Text, "screen is off") {
		t.Fatalf("%+v", res)
	}
	if len(conn.requests()) != 1 {
		t.Fatalf("ops %v", conn.ops())
	}
}

func TestAScreenWithNothingToNameComesWithAPicture(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(), jpeg: jpegB64}
	a, _, conn := newAgent(t, StateReady, p)
	res := act(t, a, AgentAction{Action: ActTap, X: 5, Y: 5})
	if string(res.Image) != "\xff\xd8jpeg" || !strings.Contains(res.Text, "nothing readable") {
		t.Fatalf("%+v", res)
	}
	reqs := conn.requests()
	if len(reqs) != 2 || reqs[0].Op != OpTap || reqs[1].Op != OpScreen || !reqs[1].Image {
		t.Fatalf("requests %+v", reqs)
	}

	// A screen with elements has no picture unless asked.
	p.set(appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10})))
	if res := act(t, a, AgentAction{Action: ActKey, Key: "back"}); len(res.Image) != 0 {
		t.Fatal("a picture nobody asked for")
	}
	if res := act(t, a, AgentAction{Action: ActKey, Key: "back", Image: true}); len(res.Image) == 0 {
		t.Fatal("no picture when asked")
	}
	// An unreadable tree after an action is shown as such, with a picture.
	p.set(nil)
	if res := act(t, a, AgentAction{Action: ActKey, Key: "back"}); !strings.Contains(res.Text, "could not be read") || len(res.Image) == 0 {
		t.Fatalf("%+v", res)
	}
}

func TestRefusalsReachTheAgentAsErrors(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}))}
	for state, want := range map[string]string{StatePersonInCharge: CodePersonInControl, StateStopped: CodeStopped, StateSetup: CodeNotReady} {
		a, _, _ := newAgent(t, state, p)
		for _, action := range []AgentAction{{Action: ActScreen}, {Action: ActTap, X: 1, Y: 1}, {Action: ActTap, Element: 1}, {Action: ActSwipe, Direction: "up"}, {Action: ActLaunch, App: "x"}, {Action: ActHandoff, Reason: "x"}} {
			if _, err := a.Act(context.Background(), "w1", "", action); code(err) != want {
				t.Fatalf("%s %+v: %v", state, action, err)
			}
		}
	}
	a := NewAgent(NewManager(Options{}), nil)
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActTap, Element: 1}); code(err) != CodeNotConnected {
		t.Fatalf("offline: %v", err)
	}
	if _, err := a.Act(context.Background(), "", "", AgentAction{Action: ActScreen}); err == nil {
		t.Fatal("an action without a Worker")
	}
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: "teleport"}); err == nil {
		t.Fatal("an unknown action")
	}
}

func TestWaitForTextPollsTheScreen(t *testing.T) {
	loading := appScreen(node("ProgressBar", "", "", "Loading", plain, [4]int{0, 0, 10, 10}))
	loaded := appScreen(node("TextView", "", "Order shipped", "", plain, [4]int{0, 0, 10, 10}))
	p := &scriptedPhone{screen: loading}
	a, _, conn := newAgent(t, StateReady, p)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }
	polls := 0
	a.sleep = func(_ context.Context, d time.Duration) error {
		clock = clock.Add(d)
		if polls++; polls == 3 {
			p.set(loaded)
		}
		return nil
	}

	res := act(t, a, AgentAction{Action: ActWait, For: "SHIPPED", Seconds: 10})
	if !strings.Contains(res.Text, `Found "SHIPPED" at [2]`) || !strings.Contains(res.Text, "Order shipped") {
		t.Fatalf("found:\n%s", res.Text)
	}
	if strings.Join(conn.ops(), ",") != "screen,screen,screen,screen" {
		t.Fatalf("ops %v", conn.ops())
	}

	// Never there: give up when the time is up, and say so with the screen as it is.
	p.set(loading)
	polls = -1000
	before := len(conn.ops())
	res = act(t, a, AgentAction{Action: ActWait, For: "never", Seconds: 2})
	if !strings.Contains(res.Text, `"never" did not appear within 2 seconds`) || !strings.Contains(res.Text, "Loading") {
		t.Fatalf("not found:\n%s", res.Text)
	}
	if n := len(conn.ops()) - before; n != 5 { // 0, 0.5, 1, 1.5, 2 seconds
		t.Fatalf("%d polls", n)
	}

	// Plain waiting is one op for that long, at most ten seconds.
	act(t, a, AgentAction{Action: ActWait, Seconds: 3})
	act(t, a, AgentAction{Action: ActWait, Seconds: 99})
	reqs := conn.requests()
	if got := args[WaitArgs](t, reqs[len(reqs)-2]); got.MS != 3000 {
		t.Fatalf("wait %+v", got)
	}
	if got := args[WaitArgs](t, reqs[len(reqs)-1]); got.MS != 10000 {
		t.Fatalf("wait %+v", got)
	}
}

func TestAHandoffAsksThePersonThenLooksAtWhatTheyLeft(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "Signed in", "", plain, [4]int{0, 0, 10, 10}))}
	m := NewManager(Options{})
	var link *Link
	conn := &fakeConn{}
	conn.handle = func(r Request) *Result {
		if r.Op == OpHandoff {
			return nil
		}
		return p.handle(r)
	}
	link, err := m.Attach(hello(StateReady), conn)
	if err != nil {
		t.Fatal(err)
	}
	conn.link = link
	a := NewAgent(m, nil)

	type answer struct {
		res AgentResult
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := a.Act(context.Background(), "w1", "Writer", AgentAction{Action: ActHandoff, Reason: "  Sign in\nto Instagram  "})
		done <- answer{res, err}
	}()
	eventually(t, "the handoff request", func() bool { return len(conn.requests()) == 1 })
	req := conn.requests()[0]
	if got := args[HandoffArgs](t, req); got.Reason != "Sign in to Instagram" || got.TimeoutMS != 0 {
		t.Fatalf("handoff %+v", got)
	}
	sendFrame(t, link, Event{Type: TypeEvent, Kind: EventHandedOff, ID: req.ID})
	r := <-done
	if r.err != nil || !strings.Contains(r.res.Text, "The person says they are done.") || !strings.Contains(r.res.Text, "Signed in") {
		t.Fatalf("%+v %v", r.res, r.err)
	}
	if strings.Join(conn.ops(), ",") != "handoff,screen" {
		t.Fatalf("ops %v", conn.ops())
	}
	if _, err := a.Act(context.Background(), "w1", "", AgentAction{Action: ActHandoff}); err == nil {
		t.Fatal("a handoff without a reason")
	}
}

func TestEveryLayerWaitsLongerThanTheOneInsideIt(t *testing.T) {
	handoff := AgentAction{Action: ActHandoff}.Budget()
	if handoff < HandoffDefault+handoffGrace+DefaultTimeout {
		t.Fatalf("a handoff gets %v", handoff)
	}
	if wait := (AgentAction{Action: ActWait, Seconds: 10}).Budget(); wait < 10*time.Second+DefaultTimeout {
		t.Fatalf("a wait gets %v", wait)
	}
	if long := (AgentAction{Action: ActWait, Seconds: 1000}).Budget(); long > actionBudget+maxWaitSecs*time.Second {
		t.Fatalf("a wait is not capped: %v", long)
	}
	if tapBudget := (AgentAction{Action: ActTap}).Budget(); tapBudget < 3*DefaultTimeout {
		t.Fatalf("an action that may take three ops gets %v", tapBudget)
	}
}

func TestACallerThatGivesUpEndsTheAction(t *testing.T) {
	p := &scriptedPhone{screen: appScreen(node("TextView", "", "x", "", plain, [4]int{0, 0, 10, 10}))}
	m := NewManager(Options{})
	_, conn := connect(t, m, StateReady, func(Request) *Result { return nil })
	_ = p
	a := NewAgent(m, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.Act(ctx, "w1", "", AgentAction{Action: ActScreen})
		done <- err
	}()
	eventually(t, "the request", func() bool { return len(conn.requests()) == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}
