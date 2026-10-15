package screen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
)

// One person or one agent controls the screen at a time, and a person always
// comes first: a viewer that connects ends an agent's control, and an agent
// is refused while a person is in control.
var (
	ErrPersonControlling = errors.New("A person is using this computer's screen; try again when they are done")
	ErrAgentStopped      = errors.New("A person took over this computer's screen; the agent's control ended")
	ErrScreenReplaced    = errors.New("Another device took over this screen")
	ErrScreenLocked      = errors.New("This computer's screen is locked; agents can use it again once it's unlocked")
	ErrDisplayAsleep     = errors.New("This computer's display went to sleep or off, so the agent lost its view; try again and Mewla wakes it (a closed lid with no external display stays off)")
	ErrPersonAtComputer  = errors.New("Someone is using this computer's keyboard or pointer, so the agent paused; wait about 10 seconds and try again")
)

// An agent pauses while someone uses the computer itself: until personPause
// after their last input. Input counts as theirs when it came personMargin
// after the agent's own last event, which the system may count as input too.
var (
	personPause  = 10 * time.Second
	personMargin = 300 * time.Millisecond
)

// AgentLongEdge is the long edge of agent screenshots. Agents give
// coordinates on the screenshot; Mewla converts them to the display.
const AgentLongEdge = 1280

const agentIdle = 60 * time.Second

var (
	agentSettle      = 400 * time.Millisecond
	agentFirstFrame  = 5 * time.Second
	agentMaxWait     = 30 * time.Second
	agentAuditMaxAge = 30 * 24 * time.Hour
)

// Control is one holder of the screen.
type Control struct {
	agent  bool
	cancel context.CancelCauseFunc
}

// TakeControl gives the screen to a person's viewer. It ends any agent's
// control and replaces an older viewer, whose cancel gets the reason.
func (s *Service) TakeControl(cancel context.CancelCauseFunc) *Control {
	s.control.Lock()
	defer s.control.Unlock()
	if s.current != nil {
		if s.current.agent {
			s.current.cancel(ErrAgentStopped)
		} else {
			s.current.cancel(ErrScreenReplaced)
		}
	}
	s.current = &Control{cancel: cancel}
	return s.current
}

// ReleaseControl ends a holder's control if it still has it.
func (s *Service) ReleaseControl(c *Control) {
	s.control.Lock()
	defer s.control.Unlock()
	if s.current == c {
		s.current = nil
	}
}

// AgentAction is one computer-use action, shaped like the computer tool
// models already know. Coordinates are screenshot pixels.
type AgentAction struct {
	Action          string  `json:"action"`
	Coordinate      []int   `json:"coordinate,omitempty"`
	StartCoordinate []int   `json:"start_coordinate,omitempty"`
	Text            string  `json:"text,omitempty"`
	ScrollDirection string  `json:"scroll_direction,omitempty"`
	ScrollAmount    int     `json:"scroll_amount,omitempty"`
	Duration        float64 `json:"duration,omitempty"`
	// App names the app an app action is for: its name or pid.
	App string `json:"app,omitempty"`
}

// AgentResult is what the agent sees: a JPEG screenshot and/or a short line
// of text.
type AgentResult struct {
	Text   string `json:"text,omitempty"`
	Image  []byte `json:"image,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// AgentState is what the app shows while an agent uses the screen.
type AgentState struct {
	Worker string    `json:"worker"`
	Name   string    `json:"name,omitempty"`
	Since  time.Time `json:"since"`
}

// SetAgentGate is the app's switch for agents on this computer. Turned off,
// even from another process, it ends the agent using the screen within
// seconds.
func (s *Service) SetAgentGate(enabled func() bool) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.agentGate = enabled
}

// agentBlocked says why an agent may no longer use the screen, if it may not.
func (s *Service) agentBlocked() error {
	if cfg, err := LoadConfig(s.stateDir); err == nil && cfg.Disabled {
		return ErrDisabled
	}
	s.agentMu.Lock()
	gate := s.agentGate
	s.agentMu.Unlock()
	if gate != nil && !gate() {
		return errors.New("Agents were turned off in the app")
	}
	return nil
}

// SetWorkerNames lets the app show a Worker's name instead of its id.
func (s *Service) SetWorkerNames(name func(id string) string) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.workerName = name
}

type agentSession struct {
	worker  string
	label   string
	since   time.Time
	session Session
	control *Control
	cancel  context.CancelCauseFunc
	ctx     context.Context

	mu       sync.Mutex
	latest   *image.RGBA
	seq      uint64
	changed  chan struct{} // closed when latest changes, for watchers
	frameErr error
	ready    chan struct{}
	lastUse  time.Time
	inflight int
	lastAct  time.Time
	pointer  image.Point
	buttons  uint8
	// presence reports the person at the computer; lastPost is when this
	// agent's own last event went out.
	presence    func() Presence
	lastPost    time.Time
	pausedUntil time.Time
	// readElements reads the focused window, when the backend can.
	readElements func(ctx context.Context, limit int) (ElementTree, error)
	scale        float64 // framebuffer pixels per screenshot pixel

	// appMu serializes reading apps and acting on their elements: tree is
	// the agent's latest read, whose ids its element actions name, and the
	// handles it holds are released only under appMu.
	appMu   sync.Mutex
	apps    appReader
	tree    *ElementTree
	treeApp App
}

// Agent reports the agent using the screen, if any.
func (s *Service) Agent() (AgentState, bool) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agent == nil || s.agent.ctx.Err() != nil {
		return AgentState{}, false
	}
	state := AgentState{Worker: s.agent.worker, Since: s.agent.since}
	if s.workerName != nil {
		state.Name = s.workerName(s.agent.worker)
	}
	return state, true
}

// StopAgent ends the agent's control, from the app.
func (s *Service) StopAgent() {
	s.agentMu.Lock()
	agent := s.agent
	s.agentMu.Unlock()
	if agent != nil {
		agent.cancel(errors.New("You stopped the agent from the app"))
	}
}

// AgentReady says whether agents can use the screen now, and why not.
func (s *Service) AgentReady(ctx context.Context) (Capability, error) {
	capability := s.backend.Capability(ctx)
	if !capability.Supported {
		return capability, ErrUnsupported
	}
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		return capability, err
	}
	if cfg.Disabled {
		return capability, ErrDisabled
	}
	if !capability.Ready() {
		return capability, ErrPermissions
	}
	if s.presence().Locked {
		return capability, ErrScreenLocked
	}
	return capability, nil
}

func (s *Service) presence() Presence {
	if reporter, ok := s.backend.(presenceReporter); ok {
		return reporter.Presence()
	}
	return Presence{}
}

// OpenAgentSettings shows the settings page for a permission agents need.
// The app asks through its own authenticated route; no Screen approval of a
// device is involved, since no device views the screen.
func (s *Service) OpenAgentSettings(ctx context.Context, permission string) error {
	return s.backend.OpenSettings(ctx, permission)
}

// Act performs one action for a Worker. Every call passes the same gates as
// a person's viewer and leaves an audit line without typed text.
func (s *Service) Act(ctx context.Context, worker string, action AgentAction) (AgentResult, error) {
	if strings.TrimSpace(worker) == "" {
		return AgentResult{}, errors.New("computer use needs the Worker it acts for")
	}
	if _, err := s.AgentReady(ctx); err != nil {
		return AgentResult{}, err
	}
	agent, err := s.agentFor(worker)
	if err != nil {
		return AgentResult{}, err
	}
	if agent.personActive() {
		agent.letGo()
		s.audit(worker, action, ErrPersonAtComputer)
		return AgentResult{}, ErrPersonAtComputer
	}
	agent.mu.Lock()
	agent.inflight++
	agent.mu.Unlock()
	result, err := agent.act(ctx, action)
	if errors.Is(err, ErrCaptureStopped) {
		err = ErrDisplayAsleep
	}
	agent.mu.Lock()
	agent.inflight--
	agent.lastUse = time.Now()
	agent.mu.Unlock()
	s.audit(worker, action, err)
	return result, err
}

func (s *Service) agentFor(worker string) (*agentSession, error) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agent != nil && s.agent.ctx.Err() == nil {
		if s.agent.worker != worker {
			return nil, fmt.Errorf("Another agent (%s) is using this computer's screen", s.agent.worker)
		}
		return s.agent, nil
	}
	// What an agent posted last is not a person's input for the next
	// session either, as when a display that slept ends one.
	lastPost := s.agentPosted
	if old := s.agent; old != nil {
		old.mu.Lock()
		if old.lastPost.After(lastPost) {
			lastPost = old.lastPost
		}
		old.mu.Unlock()
	}
	s.control.Lock()
	if s.current != nil && !s.current.agent {
		s.control.Unlock()
		return nil, ErrPersonControlling
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	control := &Control{agent: true, cancel: cancel}
	s.current = control
	s.control.Unlock()

	session, err := s.backend.Start(ctx)
	if err != nil {
		cancel(err)
		s.ReleaseControl(control)
		return nil, err
	}
	label := worker
	if s.workerName != nil {
		if name := s.workerName(worker); name != "" {
			label = name
		}
	}
	agent := &agentSession{
		worker: worker, label: label, since: time.Now(), session: session, control: control, presence: s.presence, lastPost: lastPost,
		cancel: cancel, ctx: ctx, ready: make(chan struct{}), changed: make(chan struct{}), lastUse: time.Now(),
	}
	if reader, ok := s.backend.(elementReader); ok {
		agent.readElements = reader.Elements
	}
	if reader, ok := s.backend.(appReader); ok {
		agent.apps = reader
	}
	if holder, ok := s.backend.(displayHolder); ok {
		holder.HoldDisplay(true)
	}
	s.agent = agent
	go agent.capture()
	go s.watchAgent(agent)
	Notify("Mewla", "An agent ("+label+") is using this computer. Open Screen in the app to watch or stop it.")
	return agent, nil
}

// watchAgent ends an idle agent's control and cleans up when it ends.
func (s *Service) watchAgent(agent *agentSession) {
	idleAfter := s.agentIdle
	if idleAfter == 0 {
		idleAfter = agentIdle
	}
	// Screen or the switch may be turned off by another process (the mewla
	// command), so look often.
	ticker := time.NewTicker(min(idleAfter/4, 2*time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-agent.ctx.Done():
		case <-ticker.C:
			if err := s.agentBlocked(); err != nil {
				agent.cancel(err)
				break
			}
			agent.mu.Lock()
			idle := agent.inflight == 0 && time.Since(agent.lastUse) > idleAfter
			agent.mu.Unlock()
			if !idle {
				continue
			}
			agent.cancel(errors.New("the agent stopped using the screen"))
		}
		break
	}
	// An action in flight finishes letting go of its keys first, and a button
	// the agent left down between actions comes up.
	agent.mu.Lock()
	agent.letGoLocked()
	_ = agent.session.Close()
	posted := agent.lastPost
	agent.mu.Unlock()
	agent.releaseTree()
	s.ReleaseControl(agent.control)
	s.agentMu.Lock()
	if posted.After(s.agentPosted) {
		s.agentPosted = posted
	}
	if s.agent == agent {
		s.agent = nil
		if holder, ok := s.backend.(displayHolder); ok {
			holder.HoldDisplay(false)
		}
	}
	s.agentMu.Unlock()
	Notify("Mewla", "The agent ("+agent.label+") stopped using this computer.")
}

// elements lists what the focused window offers, placed on the screenshot.
func (a *agentSession) elements(ctx context.Context) (AgentResult, error) {
	if a.readElements == nil {
		return AgentResult{}, ErrNoElements
	}
	tree, err := a.readElements(ctx, elementsRead)
	if err != nil {
		return AgentResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentResult{Text: formatElements(tree, a.latest.Rect, a.toAgent)}, nil
}

// listApps lists the apps an agent can read and act on.
func (a *agentSession) listApps(ctx context.Context) (AgentResult, error) {
	if a.apps == nil {
		return AgentResult{}, ErrNoApps
	}
	apps, err := a.apps.Apps(ctx)
	if err != nil {
		return AgentResult{}, err
	}
	return AgentResult{Text: formatApps(apps)}, nil
}

// appState reads one app, in front or not, and gives its elements ids. The
// previous read's ids end here.
func (a *agentSession) appState(ctx context.Context, name string) (AgentResult, error) {
	if a.apps == nil {
		return AgentResult{}, ErrNoApps
	}
	apps, err := a.apps.Apps(ctx)
	if err != nil {
		return AgentResult{}, err
	}
	app, err := resolveApp(apps, name)
	if err != nil {
		return AgentResult{}, err
	}
	a.appMu.Lock()
	defer a.appMu.Unlock()
	tree, err := a.apps.ReadApp(ctx, app.PID, elementsRead)
	if err != nil {
		return AgentResult{}, err
	}
	if a.tree != nil {
		a.apps.Release(*a.tree)
	}
	a.tree, a.treeApp = &tree, app
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentResult{Text: formatAppTree(app, tree, a.latest.Rect, a.toAgent)}, nil
}

// releaseTree lets go of the latest read's elements, when the agent ends.
func (a *agentSession) releaseTree() {
	a.appMu.Lock()
	defer a.appMu.Unlock()
	if a.tree != nil {
		a.apps.Release(*a.tree)
		a.tree = nil
	}
}

// personActive says someone used the computer's own keyboard or pointer in
// the last personPause, after this agent's last event.
func (a *agentSession) personActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.personActiveLocked()
}

func (a *agentSession) personActiveLocked() bool {
	if a.presence == nil {
		return false
	}
	// Once seen, the pause holds for its full time: the agent's own events
	// after it, such as letting go of a button, must not end it early.
	if last := a.presence().LastInput; !last.IsZero() && last.After(a.lastPost.Add(personMargin)) {
		if until := last.Add(personPause); until.After(a.pausedUntil) {
			a.pausedUntil = until
		}
	}
	return time.Now().Before(a.pausedUntil)
}

// letGo releases a button the agent left down between actions, so a paused
// or stopped agent never holds one under the person's hand.
func (a *agentSession) letGo() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.letGoLocked()
}

func (a *agentSession) letGoLocked() {
	if a.buttons != 0 {
		_ = a.session.Pointer(a.pointer.X, a.pointer.Y, 0)
		a.buttons = 0
		a.lastPost = time.Now()
	}
}

// capture keeps the newest frame so a screenshot never waits for a change.
func (a *agentSession) capture() {
	var seq uint64
	first := true
	for {
		frame, err := a.session.Frame(a.ctx, seq)
		a.mu.Lock()
		if err != nil {
			// A dead capture ends the agent's control at once, so its next
			// action starts a new one, which wakes the display.
			if a.ctx.Err() == nil {
				a.cancel(err)
			}
			a.frameErr = err
			if first {
				close(a.ready)
			}
			close(a.changed)
			a.mu.Unlock()
			return
		}
		seq = frame.Seq
		if a.latest == nil || a.latest.Rect != frame.Image.Rect {
			a.latest = image.NewRGBA(frame.Image.Rect)
		}
		copy(a.latest.Pix, frame.Image.Pix)
		a.seq++
		close(a.changed)
		a.changed = make(chan struct{})
		if first {
			close(a.ready)
			first = false
		}
		a.mu.Unlock()
	}
}

func (a *agentSession) act(ctx context.Context, action AgentAction) (AgentResult, error) {
	select {
	case <-a.ready:
	case <-time.After(agentFirstFrame):
		return AgentResult{}, errors.New("the screen did not answer in time")
	case <-ctx.Done():
		return AgentResult{}, ctx.Err()
	}
	if err := context.Cause(a.ctx); err != nil {
		return AgentResult{}, err
	}
	a.mu.Lock()
	a.lastUse = time.Now()
	if a.frameErr != nil {
		err := a.frameErr
		a.mu.Unlock()
		return AgentResult{}, err
	}
	// The display can change size (another monitor, a new resolution).
	bounds := a.latest.Rect
	a.scale = max(1, float64(max(bounds.Dx(), bounds.Dy()))/AgentLongEdge)
	a.mu.Unlock()

	switch action.Action {
	case "screenshot":
		return a.screenshot(ctx)
	case "elements":
		return a.elements(ctx)
	case "apps":
		return a.listApps(ctx)
	case "app_state":
		return a.appState(ctx, action.App)
	case "cursor_position":
		a.mu.Lock()
		x, y := a.toAgent(a.pointer)
		a.mu.Unlock()
		return AgentResult{Text: fmt.Sprintf("X=%d,Y=%d", x, y)}, nil
	case "wait":
		d := time.Duration(action.Duration * float64(time.Second))
		if d <= 0 || d > agentMaxWait {
			return AgentResult{}, fmt.Errorf("wait takes a duration between 0 and %d seconds", int(agentMaxWait/time.Second))
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return AgentResult{}, ctx.Err()
		case <-a.ctx.Done():
			return AgentResult{}, context.Cause(a.ctx)
		}
		return a.screenshot(ctx)
	}
	if err := a.input(action); err != nil {
		return AgentResult{}, err
	}
	a.mu.Lock()
	a.lastAct = time.Now()
	a.mu.Unlock()
	return AgentResult{Text: "done; take a screenshot to see the result"}, nil
}

func (a *agentSession) input(action AgentAction) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A person comes first, whether they take over from the app or use the
	// computer itself: every event first checks that the agent still has the
	// screen and nobody else is typing or pointing, and an interrupted action
	// lets go of what it holds, so no key or button stays pressed.
	var held []uint32
	var interrupted error
	stopped := func() error {
		if a.ctx.Err() != nil {
			interrupted = context.Cause(a.ctx)
		} else if a.personActiveLocked() {
			interrupted = ErrPersonAtComputer
		}
		return interrupted
	}
	defer func() {
		if interrupted == nil {
			return
		}
		for i := len(held) - 1; i >= 0; i-- {
			_ = a.session.Key(held[i], false)
			a.lastPost = time.Now()
		}
		a.letGoLocked()
	}()
	press := func(keysym uint32, down bool) error {
		if err := stopped(); err != nil {
			return err
		}
		err := a.session.Key(keysym, down)
		a.lastPost = time.Now()
		if err != nil {
			return err
		}
		if down {
			held = append(held, keysym)
		} else {
			for i := len(held) - 1; i >= 0; i-- {
				if held[i] == keysym {
					held = append(held[:i], held[i+1:]...)
					break
				}
			}
		}
		return nil
	}
	at := func(c []int) (image.Point, error) {
		if len(c) != 2 {
			return image.Point{}, fmt.Errorf("%s needs coordinate [x, y] on the screenshot", action.Action)
		}
		p := image.Pt(int(float64(c[0])*a.scale+0.5), int(float64(c[1])*a.scale+0.5))
		if !p.In(a.latest.Rect) {
			w, h := a.agentSize()
			return image.Point{}, fmt.Errorf("[%d, %d] is outside the %dx%d screenshot", c[0], c[1], w, h)
		}
		return p, nil
	}
	point := a.pointer
	if action.Coordinate != nil {
		p, err := at(action.Coordinate)
		if err != nil {
			return err
		}
		point = p
	}
	pointer := func(p image.Point, buttons uint8) error {
		if err := stopped(); err != nil {
			return err
		}
		a.pointer, a.buttons = p, buttons
		err := a.session.Pointer(p.X, p.Y, buttons)
		a.lastPost = time.Now()
		return err
	}
	clicks := func(mask uint8, n int) error {
		if err := pointer(point, a.buttons&^mask); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := pointer(point, a.buttons|mask); err != nil {
				return err
			}
			if err := pointer(point, a.buttons&^mask); err != nil {
				return err
			}
		}
		return nil
	}
	switch action.Action {
	case "mouse_move":
		if action.Coordinate == nil {
			return errors.New("mouse_move needs coordinate [x, y]")
		}
		return pointer(point, a.buttons)
	case "left_click":
		return clicks(1, 1)
	case "right_click":
		return clicks(4, 1)
	case "middle_click":
		return clicks(2, 1)
	case "double_click":
		return clicks(1, 2)
	case "triple_click":
		return clicks(1, 3)
	case "left_mouse_down":
		return pointer(point, a.buttons|1)
	case "left_mouse_up":
		return pointer(point, a.buttons&^1)
	case "left_click_drag":
		start, err := at(action.StartCoordinate)
		if err != nil {
			return errors.New("left_click_drag needs start_coordinate and coordinate")
		}
		if action.Coordinate == nil {
			return errors.New("left_click_drag needs start_coordinate and coordinate")
		}
		if err := pointer(start, 0); err != nil {
			return err
		}
		if err := pointer(start, 1); err != nil {
			return err
		}
		const steps = 8
		for i := 1; i <= steps; i++ {
			p := image.Pt(start.X+(point.X-start.X)*i/steps, start.Y+(point.Y-start.Y)*i/steps)
			if err := pointer(p, 1); err != nil {
				return err
			}
		}
		return pointer(point, 0)
	case "scroll":
		masks := map[string]uint8{"up": 8, "down": 16, "left": 32, "right": 64}
		mask, ok := masks[action.ScrollDirection]
		if !ok {
			return errors.New("scroll needs scroll_direction up, down, left or right")
		}
		amount := action.ScrollAmount
		if amount <= 0 {
			amount = 3
		}
		if amount > 50 {
			amount = 50
		}
		for i := 0; i < amount; i++ {
			if err := pointer(point, a.buttons|mask); err != nil {
				return err
			}
			if err := pointer(point, a.buttons&^mask); err != nil {
				return err
			}
		}
		return nil
	case "type":
		if action.Text == "" {
			return errors.New("type needs text")
		}
		for _, r := range action.Text {
			keysym := runeKeysym(r)
			if err := press(keysym, true); err != nil {
				return err
			}
			if err := press(keysym, false); err != nil {
				return err
			}
		}
		return nil
	case "key", "hold_key":
		keys, err := parseCombo(action.Text)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := press(k, true); err != nil {
				return err
			}
		}
		if action.Action == "hold_key" {
			d := time.Duration(action.Duration * float64(time.Second))
			if d <= 0 || d > 10*time.Second {
				d = time.Second
			}
			// Held without a.mu released, so cleanup comes first on a stop;
			// a person's input ends the hold as well.
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for deadline := time.Now().Add(d); time.Now().Before(deadline); {
				select {
				case <-tick.C:
				case <-a.ctx.Done():
				}
				if err := stopped(); err != nil {
					return err
				}
			}
		}
		for i := len(keys) - 1; i >= 0; i-- {
			if err := press(keys[i], false); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown computer action %q", action.Action)
	}
}

// screenshot waits for the screen to settle after the last action, then
// returns the newest frame scaled to the agent's coordinates.
func (a *agentSession) screenshot(ctx context.Context) (AgentResult, error) {
	a.mu.Lock()
	settle := agentSettle - time.Since(a.lastAct)
	a.mu.Unlock()
	if settle > 0 {
		select {
		case <-time.After(settle):
		case <-ctx.Done():
			return AgentResult{}, ctx.Err()
		}
	}
	a.mu.Lock()
	src := a.latest
	w, h := a.agentSize()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Rect, src, src.Rect, xdraw.Src, nil)
	a.mu.Unlock()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 70}); err != nil {
		return AgentResult{}, err
	}
	return AgentResult{
		Image: out.Bytes(), Width: w, Height: h,
		Text: fmt.Sprintf("Screenshot %dx%d. Give coordinates on this image.", w, h),
	}, nil
}

// agentSize is the screenshot size; callers hold a.mu.
func (a *agentSession) agentSize() (int, int) {
	b := a.latest.Rect
	return int(float64(b.Dx())/a.scale + 0.5), int(float64(b.Dy())/a.scale + 0.5)
}

// toAgent converts a framebuffer point to screenshot pixels; callers hold a.mu.
func (a *agentSession) toAgent(p image.Point) (int, int) {
	return int(float64(p.X)/a.scale + 0.5), int(float64(p.Y)/a.scale + 0.5)
}

// runeKeysym is the X11 keysym of a typed character. The input layer types
// anything that is not a key on the keyboard as text, so every script works.
func runeKeysym(r rune) uint32 {
	switch r {
	case '\n', '\r':
		return 0xff0d
	case '\t':
		return 0xff09
	}
	if r >= 0x20 && r <= 0x7e {
		return uint32(r)
	}
	return 0x01000000 + uint32(r)
}

var comboKeys = map[string]uint32{
	"cmd": 0xffeb, "command": 0xffeb, "super": 0xffeb, "meta": 0xffeb, "win": 0xffeb,
	"shift": 0xffe1, "ctrl": 0xffe3, "control": 0xffe3, "alt": 0xffe9, "option": 0xffe9, "opt": 0xffe9,
	"return": 0xff0d, "enter": 0xff0d, "tab": 0xff09, "escape": 0xff1b, "esc": 0xff1b,
	"backspace": 0xff08, "delete": 0xffff, "space": 0x20, "home": 0xff50, "end": 0xff57,
	"left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"page_up": 0xff55, "pageup": 0xff55, "page_down": 0xff56, "pagedown": 0xff56,
	"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1, "f5": 0xffc2, "f6": 0xffc3,
	"f7": 0xffc4, "f8": 0xffc5, "f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
	"plus": '+', "minus": '-',
}

// parseCombo reads xdotool-style combos such as "cmd+shift+4" or "Return".
func parseCombo(combo string) ([]uint32, error) {
	combo = strings.TrimSpace(combo)
	if combo == "" {
		return nil, errors.New("key needs text, such as \"cmd+c\" or \"Return\"")
	}
	var keys []uint32
	for _, part := range strings.Split(combo, "+") {
		name := strings.ToLower(strings.TrimSpace(part))
		if k, ok := comboKeys[strings.ReplaceAll(name, " ", "_")]; ok {
			keys = append(keys, k)
			continue
		}
		runes := []rune(strings.TrimSpace(part))
		if len(runes) == 1 {
			r := runes[0]
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A' // a combo names the key; shift is its own part
			}
			keys = append(keys, runeKeysym(r))
			continue
		}
		return nil, fmt.Errorf("unknown key %q in %q", part, combo)
	}
	return keys, nil
}

type auditLine struct {
	Time       time.Time `json:"time"`
	Worker     string    `json:"worker"`
	Action     string    `json:"action"`
	Coordinate []int     `json:"coordinate,omitempty"`
	Start      []int     `json:"start_coordinate,omitempty"`
	App        string    `json:"app,omitempty"`
	Keys       string    `json:"keys,omitempty"`
	TextLength int       `json:"text_length,omitempty"`
	TextHash   string    `json:"text_sha256,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// audit records what an agent did, never what it typed or saw.
func (s *Service) audit(worker string, action AgentAction, actErr error) {
	line := auditLine{Time: time.Now().UTC(), Worker: worker, Action: action.Action, Coordinate: action.Coordinate, Start: action.StartCoordinate, App: action.App}
	switch action.Action {
	case "type":
		sum := sha256.Sum256([]byte(action.Text))
		line.TextLength, line.TextHash = len([]rune(action.Text)), hex.EncodeToString(sum[:8])
	case "key", "hold_key":
		line.Keys = action.Text
	}
	if actErr != nil {
		line.Error = actErr.Error()
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	path := filepath.Join(s.stateDir, "screen-agent-audit.jsonl")
	if !s.auditPruned {
		s.auditPruned = true
		pruneAudit(path, time.Now().Add(-agentAuditMaxAge))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

func pruneAudit(path string, before time.Time) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var entry auditLine
		if len(line) == 0 || json.Unmarshal(line, &entry) != nil || entry.Time.Before(before) {
			continue
		}
		kept = append(append(kept, line...), '\n')
	}
	_ = os.WriteFile(path, kept, 0o600)
}
