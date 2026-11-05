// Package iphone is how Mewla's agents use the person's iPhone: through iPhone
// Mirroring on the Mac the daemon runs on. The iPhone stays locked in the
// person's pocket or on the desk; its screen is a window on the Mac, which the
// screen package moves off the person's screen and clicks and types into from
// behind. This package turns that window into a phone the agent tools know
// (phone.Device): a reading of the screen as numbered text elements, and
// taps, swipes, text, keys, app launches and handoffs.
//
// The mirror shows pixels only. The reading is the text on the picture, read
// on the Mac (Vision), with its place; what has no text (an icon) is tapped by
// a point on the picture.
package iphone

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/jpeg"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/phone"
	xdraw "golang.org/x/image/draw"
)

// Line is one line of text on the mirror's picture, in its pixels.
type Line struct {
	Text string
	Box  image.Rectangle
}

// Info is iPhone Mirroring as it stands, read without touching it.
type Info struct {
	// Supported is false on a computer that has no iPhone Mirroring to drive.
	Supported bool
	Running   bool
	Window    bool
	// Front says the person has it in front.
	Front bool
	// Worker is the agent the screen service says works in it.
	Worker string
}

// Mirror is iPhone Mirroring, as the screen service drives it for a Worker.
// Points are pixels of the picture Shot returns.
type Mirror interface {
	Info(ctx context.Context) Info
	Shot(ctx context.Context, worker string) (*image.RGBA, error)
	Read(ctx context.Context, img *image.RGBA) ([]Line, error)
	Tap(ctx context.Context, worker string, x, y int, hold time.Duration) error
	Swipe(ctx context.Context, worker string, x1, y1, x2, y2 int) error
	Type(ctx context.Context, worker, text string) error
	// Key is return, delete, tab, escape, select_all, home, app_switcher or spotlight.
	Key(ctx context.Context, worker, name string) error
	Release(worker string)
	// Stop ends the Worker's session on the Mac's screen, as Stop in Screen does.
	Stop(worker string)
}

// Keys an agent names, and what the mirror calls them.
var Profile = phone.Profile{
	Keys: map[string]string{
		"enter": "return", "return": "return", "delete": "delete", "backspace": "delete", "tab": "tab",
		"escape": "escape", "home": "home", "app_switcher": "app_switcher", "recents": "app_switcher", "spotlight": "spotlight", "search": "spotlight",
	},
	KeyList:   "home, app_switcher, spotlight, enter, delete, tab or escape (an iPhone has no back key: tap the app's back button, usually at the top left)",
	NoAppList: true,
}

// AuditFile is the iPhone's audit log in the state directory.
const AuditFile = "iphone-agent-audit.jsonl"

// Options tune a Phone; the zero value is what the daemon runs.
type Options struct {
	// Idle is how long a Worker keeps the iPhone without acting; DefaultIdle.
	Idle time.Duration
	// Settle bounds the wait for the screen to stop changing after an action.
	Settle time.Duration
	// Sleep and Now are time, replaced in tests.
	Sleep func(ctx context.Context, d time.Duration) error
	Now   func() time.Time
	// Notify tells the person a Worker needs them on the iPhone, for a handoff.
	Notify func(worker, workerName, reason string)
}

const (
	DefaultIdle   = 60 * time.Second
	defaultSettle = 2500 * time.Millisecond
	settleEvery   = 250 * time.Millisecond
	// agentLongEdge is the long edge of the screen as the agent sees it.
	agentLongEdge = 1000
	// handoffPoll is how often a handoff looks whether the person is done.
	handoffPoll = 3 * time.Second
	holdLong    = 800 * time.Millisecond
)

// Phone is the iPhone as agents use it. It is one device: one op at a time,
// and one Worker at a time.
type Phone struct {
	mirror Mirror
	opts   Options
	sem    chan struct{}

	mu      sync.Mutex
	gen     uint64
	active  *phone.Active
	lastAct time.Time
	// scale is picture pixels per screen pixel of the latest reading; w, h
	// the screen's size.
	scale float64
	w, h  int
	// handoff is closed when the person says a handoff is done.
	handoff chan struct{}
	// stopped are the Workers the person stopped, refused until the switch
	// goes off; cancel ends the op in flight, for worker.
	stopped map[string]bool
	cancel  context.CancelCauseFunc
	running string
	// picture is the latest reading's JPEG, at the screen's size, for Watch.
	picture   []byte
	pictureAt time.Time
}

var _ phone.Device = (*Phone)(nil)

// New drives the iPhone through mirror.
func New(mirror Mirror, opts Options) *Phone {
	if opts.Idle == 0 {
		opts.Idle = DefaultIdle
	}
	if opts.Settle == 0 {
		opts.Settle = defaultSettle
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Sleep == nil {
		opts.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return &Phone{mirror: mirror, opts: opts, sem: make(chan struct{}, 1), gen: 1, scale: 1}
}

// Refusals and failures, with the codes the phone tools know.
var (
	ErrUnsupported = &phone.Error{Code: phone.CodeNotConnected, Message: "This computer can't use an iPhone: iPhone use works through iPhone Mirroring on a Mac (macOS 15 or later). Tell the person, and stop."}
	ErrInUse       = &phone.Error{Code: phone.CodePersonInControl, Message: "The person is using their iPhone: iPhone Mirroring pauses while the iPhone is unlocked. Stop and tell them; do not retry in a loop. It works again once they lock it."}
	ErrStopped     = &phone.Error{Code: phone.CodeStopped, Message: "The person stopped you on the iPhone. Stop and report; do not retry. Agents can use it again after the person turns iPhone use off and on."}
)

// mirrorState is what the mirror's window shows besides the iPhone's screen.
type mirrorState int

const (
	stateScreen       mirrorState = iota // the iPhone's screen
	stateInUse                           // the person unlocked the iPhone
	stateMirror                          // maybe the mirror's own screen: sign-in, Try Again
)

// The mirror's own words for its states, English and Chinese, lower case. Its
// own screens hold a few lines only, which is what tells them from an app
// that happens to show one of these words.
var stateWords = []struct {
	state mirrorState
	words []string
}{
	{stateInUse, []string{"iphone in use", "iphone is in use", "iphone 使用中", "iphone使用中", "正在使用 iphone", "正在使用iphone", "iphone 正在使用", "iphone正在使用"}},
	// Words an iPhone app may show as well: never a refusal, only a note.
	{stateMirror, []string{"touch id", "enter your password", "输入密码", "触控 id", "iphone mirroring", "iphone 镜像", "try again", "重试", "再试一次", "unable to connect", "无法连接", "not nearby", "不在附近", "disconnected", "已断开"}},
}

const stateMaxLines = 8

func classify(lines []Line) (mirrorState, string) {
	if len(lines) == 0 || len(lines) > stateMaxLines {
		return stateScreen, ""
	}
	var all []string
	for _, l := range lines {
		all = append(all, strings.ToLower(strings.TrimSpace(l.Text)))
	}
	joined := strings.Join(all, " · ")
	for _, s := range stateWords {
		for _, w := range s.words {
			if strings.Contains(joined, w) {
				return s.state, joined
			}
		}
	}
	return stateScreen, ""
}

// Generation numbers the iPhone's readings: a release starts them afresh.
func (p *Phone) Generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen
}

// CheckWait refuses at once on a computer with no iPhone Mirroring; the
// mirror is opened by the action itself.
func (p *Phone) CheckWait(ctx context.Context) error {
	if !p.mirror.Info(ctx).Supported {
		return ErrUnsupported
	}
	return nil
}

// Status is the iPhone as the agent tools need it: its screen's size, and
// who is using it.
func (p *Phone) Status() phone.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := phone.Status{Connected: true, State: phone.StateReady, Model: "iPhone", W: p.w, H: p.h}
	if a := p.activeLocked(); a != nil {
		active := *a
		status.Active = &active
	}
	return status
}

// Active is the Worker using the iPhone, if one is.
func (p *Phone) Active() *phone.Active {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a := p.activeLocked(); a != nil {
		active := *a
		return &active
	}
	return nil
}

func (p *Phone) activeLocked() *phone.Active {
	if p.active == nil || p.opts.Now().Sub(p.lastAct) > p.opts.Idle {
		return nil
	}
	return p.active
}

// Release ends every agent's use of the iPhone, as when iPhone use is
// turned off; agents the person stopped may use it again once it is on.
func (p *Phone) Release() {
	p.mu.Lock()
	active := p.active
	p.active = nil
	p.gen++
	p.stopped = nil
	cancel := p.cancel
	p.picture = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel(ErrStopped)
	}
	if active != nil {
		p.mirror.Release(active.Worker)
	}
}

// Stop is the person's Stop for the agent using the iPhone.
func (p *Phone) Stop() {
	p.mu.Lock()
	worker := ""
	if p.active != nil {
		worker = p.active.Worker
	}
	p.mu.Unlock()
	if worker != "" {
		p.StopWorker(worker)
	}
}

// StopWorker is the person stopping one Worker: what it is doing ends at
// once, a handoff it waits on included, and it is refused from then on.
func (p *Phone) StopWorker(worker string) {
	p.mu.Lock()
	if p.stopped == nil {
		p.stopped = map[string]bool{}
	}
	again := p.stopped[worker]
	p.stopped[worker] = true
	var cancel context.CancelCauseFunc
	if p.running == worker {
		cancel = p.cancel
	}
	held := p.active != nil && p.active.Worker == worker
	if held {
		p.active = nil
	}
	p.mu.Unlock()
	if cancel != nil {
		cancel(ErrStopped)
	}
	if held {
		p.mirror.Release(worker)
	}
	if !again {
		// The screen's Stop tells this Phone too (StopWorker again), which then ends here.
		p.mirror.Stop(worker)
	}
}

// Picture is the latest reading's JPEG, for watching the agent without
// another capture.
func (p *Phone) Picture() ([]byte, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.picture, p.pictureAt
}

// HandoffDone is the person saying they did what a handoff asked.
func (p *Phone) HandoffDone() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handoff == nil {
		return false
	}
	close(p.handoff)
	p.handoff = nil
	return true
}

// claim makes worker the one using the iPhone, or refuses with who is.
func (p *Phone) claim(worker, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.opts.Now()
	if a := p.activeLocked(); a != nil && a.Worker != worker {
		who := a.Name
		if who == "" {
			who = a.Worker
		}
		return &phone.Error{Code: phone.CodeBusy, Message: fmt.Sprintf("Another agent (%s) is using the iPhone. Wait until it is done, or work on something else.", who)}
	}
	if p.active == nil || p.active.Worker != worker || p.activeLocked() == nil {
		if p.active != nil && p.active.Worker != worker {
			// The one before went quiet: its hold on the mirror goes.
			old := p.active.Worker
			go p.mirror.Release(old)
		}
		p.active = &phone.Active{Worker: worker, Name: name, Since: now}
	}
	p.lastAct = now
	return nil
}

// Do performs one op for a Worker and returns the screen it leaves.
func (p *Phone) Do(ctx context.Context, worker, workerName, op string, args any, image bool) (phone.Reply, error) {
	if op == phone.OpRelease {
		if worker != "" {
			p.mirror.Release(worker)
		}
		return phone.Reply{Gen: p.Generation()}, nil
	}
	if op == phone.OpApps {
		return phone.Reply{}, errors.New("an iPhone cannot list its apps: launch one by its name")
	}
	if err := p.CheckWait(ctx); err != nil {
		return phone.Reply{}, err
	}
	p.mu.Lock()
	stopped := p.stopped[worker]
	p.mu.Unlock()
	if stopped {
		return phone.Reply{}, ErrStopped
	}
	if err := p.claim(worker, workerName); err != nil {
		return phone.Reply{}, err
	}
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return phone.Reply{}, &phone.Error{Code: phone.CodeTimeout, Message: "Another action on the iPhone did not finish in time; read the screen again."}
	}
	defer func() { <-p.sem }()
	defer func() {
		p.mu.Lock()
		p.lastAct = p.opts.Now()
		p.mu.Unlock()
	}()
	opCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	p.mu.Lock()
	if p.stopped[worker] {
		p.mu.Unlock()
		return phone.Reply{}, ErrStopped
	}
	p.cancel, p.running = cancel, worker
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.running == worker {
			p.cancel, p.running = nil, ""
		}
		p.mu.Unlock()
	}()
	note, err := p.act(opCtx, worker, op, args)
	if err == nil {
		var reply phone.Reply
		reply, err = p.read(opCtx, worker, image, note)
		if err == nil {
			return reply, nil
		}
	}
	if errors.Is(context.Cause(opCtx), ErrStopped) {
		return phone.Reply{}, ErrStopped
	}
	return phone.Reply{}, err
}

// act performs an op's input, and settles the screen after it.
func (p *Phone) act(ctx context.Context, worker, op string, args any) (string, error) {
	switch op {
	case phone.OpScreen:
		return "", nil
	case phone.OpTap:
		a, _ := args.(phone.TapArgs)
		x, y := p.toPicture(a.X, a.Y)
		hold := time.Duration(0)
		if a.Long {
			hold = holdLong
		}
		if err := p.mirror.Tap(ctx, worker, x, y, hold); err != nil {
			return "", err
		}
	case phone.OpSwipe:
		a, _ := args.(phone.SwipeArgs)
		x1, y1 := p.toPicture(a.X1, a.Y1)
		x2, y2 := p.toPicture(a.X2, a.Y2)
		if err := p.mirror.Swipe(ctx, worker, x1, y1, x2, y2); err != nil {
			return "", err
		}
	case phone.OpText:
		a, _ := args.(phone.TextArgs)
		if a.Replace {
			if err := p.mirror.Key(ctx, worker, "select_all"); err != nil {
				return "", err
			}
			if err := p.mirror.Key(ctx, worker, "delete"); err != nil {
				return "", err
			}
		}
		if err := p.mirror.Type(ctx, worker, a.Value); err != nil {
			return "", err
		}
	case phone.OpKey:
		a, _ := args.(phone.KeyArgs)
		name := a.Name
		if mapped, ok := Profile.Keys[name]; ok {
			// The agent's submit asks for Android's enter.
			name = mapped
		}
		if err := p.mirror.Key(ctx, worker, name); err != nil {
			return "", err
		}
	case phone.OpWait:
		a, _ := args.(phone.WaitArgs)
		if a.MS > 0 {
			return "", p.opts.Sleep(ctx, time.Duration(a.MS)*time.Millisecond)
		}
	case phone.OpLaunch:
		a, _ := args.(phone.LaunchArgs)
		return p.launch(ctx, worker, a.Package)
	case phone.OpHandoff:
		a, _ := args.(phone.HandoffArgs)
		return p.handoffWait(ctx, worker, a)
	default:
		return "", fmt.Errorf("unknown iPhone op %q", op)
	}
	return "", p.settle(ctx, worker)
}

// launch opens an app the way a person does on an iPhone with a keyboard:
// Spotlight, its name, Return.
func (p *Phone) launch(ctx context.Context, worker, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("launch needs an app's name")
	}
	if err := p.mirror.Key(ctx, worker, "home"); err != nil {
		return "", err
	}
	if err := p.opts.Sleep(ctx, 500*time.Millisecond); err != nil {
		return "", err
	}
	if err := p.mirror.Key(ctx, worker, "spotlight"); err != nil {
		return "", err
	}
	if err := p.opts.Sleep(ctx, 700*time.Millisecond); err != nil {
		return "", err
	}
	if err := p.mirror.Type(ctx, worker, name); err != nil {
		return "", err
	}
	// Spotlight finds the app as the name arrives.
	if err := p.opts.Sleep(ctx, 900*time.Millisecond); err != nil {
		return "", err
	}
	if err := p.mirror.Key(ctx, worker, "return"); err != nil {
		return "", err
	}
	if err := p.opts.Sleep(ctx, 1200*time.Millisecond); err != nil {
		return "", err
	}
	return fmt.Sprintf("Opened %q through Spotlight; check the screen is that app (Spotlight opens its best match).", name), p.settle(ctx, worker)
}

// handoffWait asks the person to do something on the iPhone and waits until
// they say they are done, or until they unlocked the iPhone and locked it
// again, which is how they do anything on it.
func (p *Phone) handoffWait(ctx context.Context, worker string, a phone.HandoffArgs) (string, error) {
	p.mu.Lock()
	name := ""
	if p.active != nil && p.active.Worker == worker {
		name = p.active.Name
	}
	p.mu.Unlock()
	done := make(chan struct{})
	p.mu.Lock()
	if p.handoff != nil {
		close(p.handoff)
	}
	p.handoff = done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.handoff == done {
			p.handoff = nil
		}
		p.mu.Unlock()
	}()
	if p.opts.Notify != nil {
		p.opts.Notify(worker, name, a.Reason)
	}
	timeout := phone.HandoffDefault
	if a.TimeoutMS > 0 {
		timeout = time.Duration(a.TimeoutMS) * time.Millisecond
	}
	deadline := p.opts.Now().Add(timeout)
	sawInUse := false
	for {
		select {
		case <-done:
			return "The person tapped Done.", nil
		default:
		}
		if !p.opts.Now().Before(deadline) {
			return "", phone.ErrTimeout
		}
		if err := p.opts.Sleep(ctx, handoffPoll); err != nil {
			return "", err
		}
		p.mu.Lock()
		p.lastAct = p.opts.Now()
		p.mu.Unlock()
		img, err := p.mirror.Shot(ctx, worker)
		if err != nil {
			continue
		}
		lines, err := p.mirror.Read(ctx, img)
		if err != nil {
			continue
		}
		state, _ := classify(lines)
		if state == stateInUse {
			sawInUse = true
			continue
		}
		if sawInUse {
			return "The person unlocked the iPhone and locked it again.", nil
		}
	}
}

// settle waits until two pictures in a row are the same, up to Settle.
func (p *Phone) settle(ctx context.Context, worker string) error {
	deadline := p.opts.Now().Add(p.opts.Settle)
	var last uint64
	for first := true; ; first = false {
		if err := p.opts.Sleep(ctx, settleEvery); err != nil {
			return err
		}
		img, err := p.mirror.Shot(ctx, worker)
		if err != nil {
			return err
		}
		sum := fingerprint(img)
		if !first && sum == last {
			return nil
		}
		last = sum
		if !p.opts.Now().Before(deadline) {
			return nil
		}
	}
}

// fingerprint hashes a coarse sample of a picture, so a blinking caret or a
// clock that ticks over is seen and the settle ends at Settle.
func fingerprint(img *image.RGBA) uint64 {
	h := fnv.New64a()
	b := img.Rect
	step := max(1, b.Dx()/64)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			i := img.PixOffset(x, y)
			h.Write(img.Pix[i : i+3])
		}
	}
	return h.Sum64()
}

func (p *Phone) toPicture(x, y int) (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return int(float64(x)*p.scale + 0.5), int(float64(y)*p.scale + 0.5)
}

// read takes the screen as the agent gets it: the text on it as elements,
// and a picture when asked.
func (p *Phone) read(ctx context.Context, worker string, withImage bool, note string) (phone.Reply, error) {
	img, err := p.mirror.Shot(ctx, worker)
	if err != nil {
		return phone.Reply{}, err
	}
	lines, err := p.mirror.Read(ctx, img)
	if err != nil {
		return phone.Reply{}, err
	}
	switch state, words := classify(lines); state {
	case stateInUse:
		return phone.Reply{}, ErrInUse
	case stateMirror:
		extra := fmt.Sprintf("This may be iPhone Mirroring's own screen on the Mac rather than the iPhone's (it says: %s). If it offers Try Again or Connect, tap it (the iPhone must be locked and near the Mac); if it asks for Touch ID or a password, never type it: hand off to the person.", words)
		if note == "" {
			note = extra
		} else {
			note += " " + extra
		}
	}
	scale := max(1, float64(max(img.Rect.Dx(), img.Rect.Dy()))/agentLongEdge)
	screen := ToScreen(lines, img.Rect.Dx(), img.Rect.Dy(), scale)
	p.mu.Lock()
	p.scale, p.w, p.h = scale, screen.W, screen.H
	gen := p.gen
	p.mu.Unlock()
	reply := phone.Reply{Gen: gen}
	reply.OK = true
	reply.Screen = screen
	reply.Note = note
	picture := encode(img, screen.W, screen.H)
	p.mu.Lock()
	p.picture, p.pictureAt = picture, p.opts.Now()
	p.mu.Unlock()
	if withImage || len(screen.Windows) == 0 {
		reply.Image = base64.StdEncoding.EncodeToString(picture)
	}
	return reply, nil
}

func encode(img *image.RGBA, w, h int) []byte {
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	xdraw.ApproxBiLinear.Scale(dst, dst.Rect, img, img.Rect, xdraw.Src, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 70}); err != nil {
		return nil
	}
	return out.Bytes()
}
