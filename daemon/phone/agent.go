package phone

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Agent actions, in AgentAction.Action.
const (
	ActScreen  = "screen"
	ActApps    = "apps"
	ActLaunch  = "launch"
	ActTap     = "tap"
	ActType    = "type"
	ActSwipe   = "swipe"
	ActKey     = "key"
	ActWait    = "wait"
	ActHandoff = "handoff"
	ActSteps   = "steps"
)

// AgentAction is one thing an agent asks of the phone, in the words of the tools it has.
type AgentAction struct {
	Action string `json:"action"`
	// App is a label or package, for launch.
	App string `json:"app,omitempty"`
	// Element is an id from the latest screen, for tap, type and swipe.
	Element int `json:"element,omitempty"`
	// Match names the element for tap, type and swipe by its text, description or hint on the
	// screen as it is now, waiting a few seconds for it to appear; Element wins when both are set.
	Match string `json:"match,omitempty"`
	X     int    `json:"x,omitempty"`
	Y     int    `json:"y,omitempty"`
	Long  bool   `json:"long,omitempty"`
	// Text is typed into the focused field (or the Element, which is tapped first).
	Text    string `json:"text,omitempty"`
	Replace bool   `json:"replace,omitempty"`
	Submit  bool   `json:"submit,omitempty"`
	// Direction is the way the finger moves: up, down, left or right, by Amount of the screen
	// (or of the Element), 0.5 when 0. Or a stroke from X1,Y1 to X2,Y2.
	Direction string  `json:"direction,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
	X1        int     `json:"x1,omitempty"`
	Y1        int     `json:"y1,omitempty"`
	X2        int     `json:"x2,omitempty"`
	Y2        int     `json:"y2,omitempty"`
	// Key is back, enter, delete, tab or search.
	Key string `json:"key,omitempty"`
	// Seconds to wait (up to 10), or to look for For, a text on the screen.
	Seconds float64 `json:"seconds,omitempty"`
	For     string  `json:"for,omitempty"`
	// Reason is the one short line a handoff shows the person.
	Reason string `json:"reason,omitempty"`
	// Image asks for a picture of the screen with the answer.
	Image bool `json:"image,omitempty"`
	// Steps are a batch's actions, run in order by ActSteps; only the last one's screen is returned.
	Steps []AgentAction `json:"steps,omitempty"`
}

// AgentResult is what the agent sees: the screen as text, and a JPEG when it is needed.
type AgentResult struct {
	Text  string `json:"text,omitempty"`
	Image []byte `json:"image,omitempty"`
}

const (
	// actionBudget bounds an action that takes a few ops, each of which the phone answers quickly.
	actionBudget = 2 * time.Minute
	maxWaitSecs  = 10
	pollEvery    = 250 * time.Millisecond
	appsFresh    = 10 * time.Second
	maxApps      = 300
	// maxSteps bounds a batch; stepsBudget is how long one may take in all.
	maxSteps    = 12
	stepsBudget = 3 * time.Minute
	// matchWait is how long a step that names an element by its text waits for it to appear.
	matchWait = 4 * time.Second
)

// Budget is the longest the daemon lets an action take: every layer that waits for it (the
// control socket call, the agent's tool timeout) is longer than this.
func (a AgentAction) Budget() time.Duration {
	switch a.Action {
	case ActSteps:
		return stepsBudget + actionBudget
	case ActHandoff:
		return HandoffDefault + handoffGrace + actionBudget
	case ActWait:
		return actionBudget + time.Duration(min(max(a.Seconds, 0), maxWaitSecs)*float64(time.Second))
	}
	return actionBudget
}

// Agent turns what an agent asks into the phone's ops, and the phone's answers into text.
type Agent struct {
	mgr   *Manager
	audit *Audit
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu       sync.Mutex
	gen      uint64
	elements *Elements
	apps     []App
	appsAt   time.Time
}

// NewAgent serves agents through mgr. The audit may be nil.
func NewAgent(mgr *Manager, audit *Audit) *Agent {
	return &Agent{mgr: mgr, audit: audit, now: time.Now, sleep: func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// Release gives the phone's display back and ends the agent's use of it, as when Phone use is
// turned off.
func (a *Agent) Release() { a.mgr.Release() }

// session returns the ids of the connection gen; a new connection starts them afresh.
func (a *Agent) session(gen uint64) *Elements {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.elements == nil || a.gen != gen {
		a.gen, a.elements, a.apps, a.appsAt = gen, NewElements(), nil, time.Time{}
	}
	return a.elements
}

type worker struct{ id, name string }

// Act performs one action for a Worker and returns the screen it leaves, as text. A refusal
// (the phone is not ready, the person is in control or stopped it, another agent has it) is an
// *Error; so is a failure of the action.
func (a *Agent) Act(ctx context.Context, id, name string, act AgentAction) (AgentResult, error) {
	if strings.TrimSpace(id) == "" {
		return AgentResult{}, errors.New("phone use needs the Worker it acts for")
	}
	w := worker{id, name}
	var t trace
	res, err := a.run(ctx, w, act, &t)
	a.audit.Record(id, act, t, err)
	return res, err
}

func (a *Agent) run(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	switch act.Action {
	case ActScreen:
		return a.screen(ctx, w, act.Image)
	case ActApps:
		return a.listApps(ctx, w)
	case ActLaunch:
		return a.launch(ctx, w, act, t)
	case ActTap:
		return a.tap(ctx, w, act, t)
	case ActType:
		return a.typeText(ctx, w, act, t)
	case ActSwipe:
		return a.swipe(ctx, w, act, t)
	case ActKey:
		return a.key(ctx, w, act, t)
	case ActWait:
		return a.wait(ctx, w, act)
	case ActHandoff:
		return a.handoff(ctx, w, act)
	case ActSteps:
		return a.runSteps(ctx, w, act)
	}
	return AgentResult{}, fmt.Errorf("unknown phone action %q", act.Action)
}

func (a *Agent) do(ctx context.Context, w worker, op string, args any, image bool) (Reply, error) {
	return a.mgr.Do(ctx, w.id, w.name, op, args, image)
}

func (a *Agent) screen(ctx context.Context, w worker, image bool) (AgentResult, error) {
	reply, err := a.do(ctx, w, OpScreen, ScreenArgs{}, image)
	return a.finish(ctx, w, reply, err, "")
}

// finish turns the phone's answer to an acting op into what the agent sees. When the screen has
// nothing the agent can name, or the phone's own screen is off, it carries a picture.
func (a *Agent) finish(ctx context.Context, w worker, reply Reply, err error, prefix string) (AgentResult, error) {
	res, needPicture, err := a.compose(reply, err, prefix)
	if err != nil || !needPicture || len(res.Image) > 0 {
		return res, err
	}
	again, err := a.do(ctx, w, OpScreen, ScreenArgs{}, true)
	next, _, nextErr := a.compose(again, err, prefix)
	if nextErr != nil {
		// The action itself worked; do not make the agent repeat it for want of a picture.
		res.Text += "\n(A picture of the screen could not be taken: " + nextErr.Error() + ")"
		return res, nil
	}
	return next, nil
}

const screenOffText = "The phone's own screen is off, so the elements cannot be read; here is a picture of the agent's screen instead."

// compose renders a reply. needPicture is set when the text alone does not show the screen.
func (a *Agent) compose(reply Reply, err error, prefix string) (res AgentResult, needPicture bool, _ error) {
	var perr *Error
	if err != nil && !(errors.As(err, &perr) && perr.Code == CodeScreenOff) {
		return AgentResult{}, false, err
	}
	res.Image = decodeImage(reply.Image)
	var lines []string
	if prefix != "" {
		lines = append(lines, prefix)
	}
	if reply.Note != "" {
		lines = append(lines, reply.Note)
	}
	switch {
	case err != nil:
		// The screen is off: a picture and one line, not a failure.
		lines = append(lines, screenOffText)
		needPicture = len(res.Image) == 0
	case reply.Screen == nil:
		lines = append(lines, "The screen could not be read.")
		needPicture = true
	default:
		view := a.session(reply.Gen).Observe(reply.Screen)
		lines = append(lines, view.Render())
		needPicture = len(view.Elements) == 0
	}
	res.Text = strings.Join(lines, "\n")
	return res, needPicture, nil
}

func decodeImage(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// resolve finds the element an id names on the latest reading.
func (a *Agent) resolve(ctx context.Context, id int, t *trace) (Element, error) {
	if err := a.mgr.CheckWait(ctx); err != nil {
		return Element{}, err
	}
	el, err := a.session(a.mgr.Generation()).Resolve(id)
	if err != nil {
		return Element{}, err
	}
	t.element = &el
	return el, nil
}

func (a *Agent) tap(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	x, y := act.X, act.Y
	switch {
	case act.Element > 0 || act.Match != "":
		el, err := a.target(ctx, w, act, t)
		if err != nil {
			return AgentResult{}, err
		}
		x, y = el.X, el.Y
	case x > 0 || y > 0:
		if err := a.onScreen(x, y); err != nil {
			return AgentResult{}, err
		}
	default:
		return AgentResult{}, errors.New("tap needs an element id from the screen, or x and y")
	}
	t.point(x, y)
	reply, err := a.do(ctx, w, OpTap, TapArgs{X: x, Y: y, Long: act.Long}, act.Image)
	return a.finish(ctx, w, reply, err, "")
}

// displaySize is the agent's display in pixels as far as known, 0 x 0 before the phone said.
func (a *Agent) displaySize() (w, h int) {
	if st := a.mgr.Status(); st.W > 0 && st.H > 0 {
		return st.W, st.H
	}
	a.mu.Lock()
	el := a.elements
	a.mu.Unlock()
	if el == nil {
		return 0, 0
	}
	return el.Size()
}

// onScreen refuses a point outside the display, once the display's size is known.
func (a *Agent) onScreen(x, y int) error {
	if w, h := a.displaySize(); w > 0 && h > 0 && (x < 0 || y < 0 || x >= w || y >= h) {
		return fmt.Errorf("(%d,%d) is outside the %dx%d screen", x, y, w, h)
	}
	return nil
}

func (a *Agent) typeText(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	if act.Text == "" && !act.Replace {
		return AgentResult{}, errors.New("type needs text (with replace true, empty text clears the field)")
	}
	if act.Element > 0 || act.Match != "" {
		el, err := a.target(ctx, w, act, t)
		if err != nil {
			return AgentResult{}, err
		}
		t.point(el.X, el.Y)
		// Pressing the field gives it focus; the text goes to the focused field.
		if _, err := a.do(ctx, w, OpTap, TapArgs{X: el.X, Y: el.Y}, false); err != nil {
			return AgentResult{}, err
		}
	}
	reply, err := a.do(ctx, w, OpText, TextArgs{Value: act.Text, Replace: act.Replace}, act.Image && !act.Submit)
	if err != nil || !act.Submit {
		return a.finish(ctx, w, reply, err, "")
	}
	reply, err = a.do(ctx, w, OpKey, KeyArgs{Name: "enter"}, act.Image)
	return a.finish(ctx, w, reply, err, "")
}

var keyNames = map[string]string{
	"back": "back", "enter": "enter", "return": "enter", "delete": "delete", "backspace": "delete", "tab": "tab", "search": "search",
}

func (a *Agent) key(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	name, ok := keyNames[strings.ToLower(strings.TrimSpace(act.Key))]
	if !ok {
		return AgentResult{}, fmt.Errorf("unknown key %q: use back, enter, delete, tab or search", act.Key)
	}
	t.key = name
	reply, err := a.do(ctx, w, OpKey, KeyArgs{Name: name}, act.Image)
	return a.finish(ctx, w, reply, err, "")
}

func (a *Agent) swipe(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	var args SwipeArgs
	if act.Direction == "" {
		if act.X1 == 0 && act.Y1 == 0 && act.X2 == 0 && act.Y2 == 0 {
			return AgentResult{}, errors.New("swipe needs a direction (up, down, left, right) or x1, y1, x2, y2")
		}
		args = SwipeArgs{X1: act.X1, Y1: act.Y1, X2: act.X2, Y2: act.Y2}
		t.point(args.X1, args.Y1)
		t.x2, t.y2 = intp(args.X2), intp(args.Y2)
	} else {
		// The region the finger moves in: the element's, else the whole display.
		var left, top, right, bottom int
		if act.Element > 0 {
			el, err := a.resolve(ctx, act.Element, t)
			if err != nil {
				return AgentResult{}, err
			}
			left, top, right, bottom = el.Bounds[0], el.Bounds[1], el.Bounds[2], el.Bounds[3]
		} else {
			if err := a.mgr.CheckWait(ctx); err != nil {
				return AgentResult{}, err
			}
			dw, dh := a.displaySize()
			if dw == 0 || dh == 0 {
				// The display's size comes with a reading.
				reply, err := a.do(ctx, w, OpScreen, ScreenArgs{}, false)
				if err != nil {
					return AgentResult{}, err
				}
				if reply.Screen != nil {
					a.session(reply.Gen).Observe(reply.Screen)
				}
				dw, dh = a.displaySize()
			}
			if dw == 0 || dh == 0 {
				return AgentResult{}, errors.New("the phone did not report its screen size; use x1, y1, x2, y2")
			}
			left, top, right, bottom = 0, 0, dw, dh
		}
		var err error
		if args, err = strokeFor(act.Direction, act.Amount, left, top, right, bottom); err != nil {
			return AgentResult{}, err
		}
		t.point(args.X1, args.Y1)
		t.x2, t.y2 = intp(args.X2), intp(args.Y2)
	}
	reply, err := a.do(ctx, w, OpSwipe, args, act.Image)
	return a.finish(ctx, w, reply, err, "")
}

// strokeFor is the finger's stroke through the middle of a region, moving in direction by amount
// of the region's length that way. A stroke is kept off the region's edges, where the system has
// gestures of its own.
func strokeFor(direction string, amount float64, left, top, right, bottom int) (SwipeArgs, error) {
	if amount <= 0 {
		amount = 0.5
	}
	amount = min(max(amount, 0.1), 0.8)
	cx, cy := (left+right)/2, (top+bottom)/2
	half := func(extent int) int { return int(float64(extent) * amount / 2) }
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up":
		return SwipeArgs{X1: cx, Y1: cy + half(bottom-top), X2: cx, Y2: cy - half(bottom-top)}, nil
	case "down":
		return SwipeArgs{X1: cx, Y1: cy - half(bottom-top), X2: cx, Y2: cy + half(bottom-top)}, nil
	case "left":
		return SwipeArgs{X1: cx + half(right-left), Y1: cy, X2: cx - half(right-left), Y2: cy}, nil
	case "right":
		return SwipeArgs{X1: cx - half(right-left), Y1: cy, X2: cx + half(right-left), Y2: cy}, nil
	}
	return SwipeArgs{}, fmt.Errorf("unknown direction %q: use up, down, left or right (the way the finger moves)", direction)
}

func (a *Agent) wait(ctx context.Context, w worker, act AgentAction) (AgentResult, error) {
	res, _, err := a.waitFor(ctx, w, act)
	return res, err
}

// waitFor is wait, and says whether the text it was asked to wait for appeared (true when it was
// asked for none).
func (a *Agent) waitFor(ctx context.Context, w worker, act AgentAction) (AgentResult, bool, error) {
	secs := min(max(act.Seconds, 0), maxWaitSecs)
	if strings.TrimSpace(act.For) == "" {
		reply, err := a.do(ctx, w, OpWait, WaitArgs{MS: int(secs * 1000)}, act.Image)
		res, err := a.finish(ctx, w, reply, err, "")
		return res, true, err
	}
	if act.Seconds <= 0 {
		secs = 5
	}
	deadline := a.now().Add(time.Duration(secs * float64(time.Second)))
	for {
		reply, err := a.do(ctx, w, OpScreen, ScreenArgs{}, false)
		if err == nil && reply.Screen != nil {
			if el, found := a.session(reply.Gen).Observe(reply.Screen).Find(act.For); found {
				res, err := a.finish(ctx, w, reply, nil, fmt.Sprintf("Found %q at [%d].", act.For, el.ID))
				return res, true, err
			}
		}
		if err != nil || !a.now().Before(deadline) {
			prefix := fmt.Sprintf("%q did not appear within %g seconds.", act.For, secs)
			res, err := a.finish(ctx, w, reply, err, prefix)
			return res, false, err
		}
		if err := a.sleep(ctx, pollEvery); err != nil {
			return AgentResult{}, false, err
		}
	}
}

const maxReason = 200

func (a *Agent) handoff(ctx context.Context, w worker, act AgentAction) (AgentResult, error) {
	reason := cleanLabel(act.Reason)
	if reason == "" {
		return AgentResult{}, errors.New("handoff needs a reason: one short line telling the person what to do")
	}
	if r := []rune(reason); len(r) > maxReason {
		reason = string(r[:maxReason]) + "…"
	}
	reply, err := a.do(ctx, w, OpHandoff, HandoffArgs{Reason: reason}, false)
	if errors.Is(err, ErrTimeout) {
		return AgentResult{}, &Error{CodeTimeout, "The person has not finished within the time given. Report that you are still waiting for them; do not hand off again at once."}
	}
	if err != nil {
		return AgentResult{}, err
	}
	if reply.Screen == nil {
		// The person's word arrived as an event; look at what they left.
		note := reply.Note
		reply, err = a.do(ctx, w, OpScreen, ScreenArgs{}, false)
		if reply.Note == "" {
			reply.Note = note
		}
	}
	return a.finish(ctx, w, reply, err, "The person says they are done.")
}

func (a *Agent) listApps(ctx context.Context, w worker) (AgentResult, error) {
	apps, err := a.appList(ctx, w, true)
	if err != nil {
		return AgentResult{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d apps on the phone (launch one by its name or package):\n", len(apps))
	for i, app := range apps {
		if i == maxApps {
			fmt.Fprintf(&b, "… %d more not shown\n", len(apps)-maxApps)
			break
		}
		fmt.Fprintf(&b, "%s — %s\n", app.Label, app.Key())
	}
	return AgentResult{Text: strings.TrimRight(b.String(), "\n")}, nil
}

// appList is the phone's launchable apps, kept for the connection. With refresh, a list read
// longer ago than appsFresh is read again.
func (a *Agent) appList(ctx context.Context, w worker, refresh bool) ([]App, error) {
	a.mu.Lock()
	cached, at, gen := a.apps, a.appsAt, a.gen
	a.mu.Unlock()
	if cached != nil && gen == a.mgr.Generation() && (!refresh || a.now().Sub(at) < appsFresh) {
		return cached, nil
	}
	reply, err := a.do(ctx, w, OpApps, nil, false)
	if err != nil {
		return nil, err
	}
	apps := append([]App(nil), reply.Apps...)
	sort.SliceStable(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Label) < strings.ToLower(apps[j].Label)
	})
	a.session(reply.Gen)
	a.mu.Lock()
	a.apps, a.appsAt = apps, a.now()
	a.mu.Unlock()
	return apps, nil
}

func (a *Agent) launch(ctx context.Context, w worker, act AgentAction, t *trace) (AgentResult, error) {
	query := strings.TrimSpace(act.App)
	if query == "" {
		return AgentResult{}, errors.New("launch needs an app: its name, or its package from phone_apps")
	}
	apps, err := a.appList(ctx, w, false)
	if err != nil {
		return AgentResult{}, err
	}
	app, err := ResolveApp(apps, query)
	if errors.As(err, new(*noApp)) {
		// Installed since the list was read?
		if apps, err = a.appList(ctx, w, true); err != nil {
			return AgentResult{}, err
		}
		app, err = ResolveApp(apps, query)
	}
	if err != nil {
		return AgentResult{}, err
	}
	t.app = app.Key()
	reply, err := a.do(ctx, w, OpLaunch, LaunchArgs{Package: app.Package, User: app.User}, act.Image)
	if e := (*Error)(nil); errors.As(err, &e) && e.Code == CodeNotInstalled {
		return AgentResult{}, &Error{CodeNotInstalled, fmt.Sprintf("%s (%s) is not installed on the phone.", app.Label, app.Key())}
	}
	return a.finish(ctx, w, reply, err, "")
}

// noApp is ResolveApp's answer when nothing matches.
type noApp struct{ query string }

func (e *noApp) Error() string {
	return fmt.Sprintf("no app on the phone matches %q; phone_apps lists them", e.query)
}

// ResolveApp picks the app a name means: a package (or package@user) or a label that matches
// exactly, else a label that starts with it, else one that contains it. A bare package names the
// person's own app, and a clone only when there is no own app by that package; a clone is named
// by package@user or by its label. Several matches at the best level are an error that lists
// them, so the agent says which.
func ResolveApp(apps []App, query string) (App, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	tiers := [4][]App{}
	var clonesByPackage []App
	for _, app := range apps {
		label, pkg, key := strings.ToLower(app.Label), strings.ToLower(app.Package), strings.ToLower(app.Key())
		switch {
		case key == q:
			tiers[0] = append(tiers[0], app)
		case pkg == q:
			clonesByPackage = append(clonesByPackage, app)
		case label == q:
			tiers[1] = append(tiers[1], app)
		case strings.HasPrefix(label, q):
			tiers[2] = append(tiers[2], app)
		case strings.Contains(label, q) || strings.Contains(key, q):
			tiers[3] = append(tiers[3], app)
		}
	}
	if len(tiers[0]) == 0 {
		tiers[0] = clonesByPackage
	}
	for _, found := range tiers {
		switch len(found) {
		case 0:
			continue
		case 1:
			return found[0], nil
		}
		names := make([]string, 0, len(found))
		for i, app := range found {
			if i == 8 {
				names = append(names, fmt.Sprintf("and %d more", len(found)-8))
				break
			}
			names = append(names, fmt.Sprintf("%s (%s)", app.Label, app.Key()))
		}
		return App{}, fmt.Errorf("%q matches several apps: %s. Launch one by its package.", query, strings.Join(names, ", "))
	}
	if strings.Contains(query, ".") && !strings.ContainsAny(query, " \t") {
		// A package that is not in the launcher list may still open.
		return App{Package: strings.TrimSpace(query), Label: strings.TrimSpace(query)}, nil
	}
	return App{}, &noApp{query}
}
