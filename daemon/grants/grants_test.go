package grants

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

type fakeBackend struct {
	mu          sync.Mutex
	states      map[string]State
	opened      []string
	checks      int
	openErr     error
	unsupported map[string]bool
}

func (f *fakeBackend) Supported(permission string) bool { return !f.unsupported[permission] }

func (f *fakeBackend) State(_ context.Context, permission string) State {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.states[permission]
}
func (f *fakeBackend) set(permission string, state State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.states == nil {
		f.states = map[string]State{}
	}
	f.states[permission] = state
}
func (f *fakeBackend) OpenSettings(_ context.Context, permission string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, permission)
	return f.openErr
}
func (f *fakeBackend) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

// fakeCards is the timeline's cards in memory.
type fakeCards struct {
	mu      sync.Mutex
	items   []brain.TimelineItem
	told    []string
	tellErr error
}

func (f *fakeCards) GrantRequests() ([]brain.TimelineItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]brain.TimelineItem(nil), f.items...), nil
}
func (f *fakeCards) UpdateGrantRequest(id string, change func(*brain.TimelineItem) error) (brain.TimelineItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			next := f.items[i]
			if err := change(&next); err != nil {
				return f.items[i], err
			}
			f.items[i] = next
			return next, nil
		}
	}
	return brain.TimelineItem{}, brain.ErrGrantRequestNotFound
}
func (f *fakeCards) AskGrant(permission, reason string, now time.Time) (brain.TimelineItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item := brain.TimelineItem{ID: "grant:" + permission + ":" + strconv.Itoa(len(f.items)), Kind: "grant_request", Role: "assistant", Status: brain.GrantAsked, Body: reason, GrantPermission: permission, CreatedAt: now}
	f.items = append(f.items, item)
	return item, nil
}
func (f *fakeCards) NoteGrantAnswered(card brain.TimelineItem, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.told = append(f.told, card.ID+": "+line)
	return f.tellErr
}
func (f *fakeCards) status(id string) string {
	cards, _ := f.GrantRequests()
	for _, card := range cards {
		if card.ID == id {
			return card.Status
		}
	}
	return ""
}
func (f *fakeCards) count() int { cards, _ := f.GrantRequests(); return len(cards) }
func (f *fakeCards) tellings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.told...)
}

type fakeRestarter struct {
	mu         sync.Mutex
	quiet      bool
	inPlace    int
	process    int
	inPlaceErr error
}

func (f *fakeRestarter) Quiet() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.quiet }
func (f *fakeRestarter) InPlace() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inPlace++
	return f.inPlaceErr
}
func (f *fakeRestarter) Process() error { f.mu.Lock(); defer f.mu.Unlock(); f.process++; return nil }
func (f *fakeRestarter) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inPlace, f.process
}

type rig struct {
	backend *fakeBackend
	cards   *fakeCards
	restart *fakeRestarter
	m       *Manager
	clock   time.Time
	later   []func()
}

func newRig() *rig {
	r := &rig{backend: &fakeBackend{}, cards: &fakeCards{}, restart: &fakeRestarter{quiet: true}, clock: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	r.m = New(r.backend, r.cards, r.restart)
	r.m.Where = "this Mac"
	r.m.now = func() time.Time { return r.clock }
	r.m.afterFunc = func(_ time.Duration, f func()) { r.later = append(r.later, f) }
	return r
}

func TestNeedAsksOnceAndNotForAGrantThatCounts(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	if _, err := r.m.Need(ctx, "location", "x"); !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("err %v", err)
	}
	got, err := r.m.Need(ctx, ScreenRecording, "A Worker needs to see the screen.")
	if err != nil || got.Outcome != OutcomeAsked || got.Card == nil || got.Card.Status != brain.GrantAsked {
		t.Fatalf("%+v %v", got, err)
	}
	again, _ := r.m.Need(ctx, ScreenRecording, "Another reason.")
	if again.Outcome != OutcomeAlreadyAsked || r.cards.count() != 1 {
		t.Fatalf("%+v, %d cards", again, r.cards.count())
	}
	// Another permission is its own card; one that already counts has none.
	other, _ := r.m.Need(ctx, Accessibility, "To click.")
	r.backend.set(Chrome, Granted)
	granted, _ := r.m.Need(ctx, Chrome, "Browser.")
	if other.Outcome != OutcomeAsked || granted.Outcome != OutcomeAlreadyGranted || granted.Card != nil || r.cards.count() != 2 {
		t.Fatalf("%+v %+v, %d cards", other, granted, r.cards.count())
	}
	// Even an open card for a grant that counts now says granted, and makes no second.
	r.backend.set(ScreenRecording, Granted)
	if res, _ := r.m.Need(ctx, ScreenRecording, "x"); res.Outcome != OutcomeAlreadyGranted || r.cards.count() != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestNeedRefusesAGrantThisComputerDoesNotHave(t *testing.T) {
	r := newRig()
	r.backend.unsupported = map[string]bool{ScreenRecording: true}
	if _, err := r.m.Need(context.Background(), ScreenRecording, "To see."); !errors.Is(err, ErrUnsupported) || r.cards.count() != 0 {
		t.Fatalf("err %v, %d cards", err, r.cards.count())
	}
}

func TestRuntimeAsksOncePerGrantAndNotAfterNotNow(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := r.m.ask(ctx, ScreenRecording, "A Worker needs to see and use the screen.", true); err != nil {
			t.Fatal(err)
		}
	}
	if r.cards.count() != 1 {
		t.Fatalf("%d cards for three spawns", r.cards.count())
	}
	// Not now: a spawn within the window does not ask again; an explicit need does; after the window the runtime may.
	id := r.cards.items[0].ID
	if _, err := r.m.Act(ctx, id, ActionNotNow); err != nil {
		t.Fatal(err)
	}
	r.clock = r.clock.Add(time.Hour)
	if res, _ := r.m.ask(ctx, ScreenRecording, "again", true); res.Outcome != OutcomeAlreadyAsked || r.cards.count() != 1 {
		t.Fatalf("asked again an hour after Not now: %+v", res)
	}
	if res, _ := r.m.Need(ctx, ScreenRecording, "The person asked for it."); res.Outcome != OutcomeAsked || r.cards.count() != 2 {
		t.Fatalf("an explicit need was refused: %+v", res)
	}
	r.m.Act(ctx, r.cards.items[1].ID, ActionNotNow)
	r.clock = r.clock.Add(7 * time.Hour)
	if res, _ := r.m.ask(ctx, ScreenRecording, "later", true); res.Outcome != OutcomeAsked || r.cards.count() != 3 {
		t.Fatalf("not asked after the window: %+v", res)
	}
	// Runtime is asynchronous and never blocks: it returns at once.
	done := make(chan struct{})
	go func() { r.m.Runtime(Chrome, "A Worker needs your Chrome."); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Runtime blocked")
	}
	r.m.Runtime("nonsense", "x")
}

func TestActionsOpenSettingsAndNotNow(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	res, _ := r.m.Need(ctx, Accessibility, "A Worker needs to click and type.")
	id := res.Card.ID

	event, err := r.m.Act(ctx, id, ActionOpenSettings)
	if err != nil || event.Status != "opened" || event.GrantPermission != Accessibility || event.Source != "grant_request" || event.Body != "A Worker needs to click and type." {
		t.Fatalf("%+v %v", event, err)
	}
	if len(r.backend.opened) != 1 || r.backend.opened[0] != Accessibility {
		t.Fatalf("opened %v", r.backend.opened)
	}
	// Opening again keeps it opened.
	if event, _ = r.m.Act(ctx, id, ActionOpenSettings); event.Status != "opened" {
		t.Fatalf("%+v", event)
	}
	// Restart is only for a card that needs it.
	var conflict *ConflictError
	if _, err := r.m.Act(ctx, id, ActionRestart); !errors.As(err, &conflict) {
		t.Fatalf("restart on an opened card: %v", err)
	}
	if _, err := r.m.Act(ctx, id, "sing"); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("err %v", err)
	}
	if _, err := r.m.Act(ctx, "grant:nope", ActionNotNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	// Not now seals it, and Brain is told once.
	event, err = r.m.Act(ctx, id, ActionNotNow)
	if err != nil || event.Status != "declined" {
		t.Fatalf("%+v %v", event, err)
	}
	if told := r.cards.tellings(); len(told) != 1 || told[0] != id+": The person chose Not now for Accessibility: stop asking for this task." {
		t.Fatalf("told %q", told)
	}
	for _, action := range []string{ActionNotNow, ActionOpenSettings, ActionRestart} {
		if _, err := r.m.Act(ctx, id, action); !errors.As(err, &conflict) {
			t.Fatalf("%s on a sealed card: %v", action, err)
		}
	}
	if len(r.cards.tellings()) != 1 {
		t.Fatalf("told again: %q", r.cards.tellings())
	}
	// A settings pane that cannot open leaves the card as it was.
	res, _ = r.m.Need(ctx, ScreenRecording, "To see.")
	r.backend.openErr = errors.New("no pane")
	if _, err := r.m.Act(ctx, res.Card.ID, ActionOpenSettings); err == nil || r.cards.status(res.Card.ID) != "asked" {
		t.Fatalf("err %v, status %s", err, r.cards.status(res.Card.ID))
	}
}

// runOnce is one pass of Run over the open cards.
func (r *rig) pass() {
	for _, card := range r.m.open() {
		r.m.check(context.Background(), card)
	}
}

func TestGrantedSealsAndTellsBrainOnce(t *testing.T) {
	r := newRig()
	res, _ := r.m.Need(context.Background(), ScreenRecording, "A Worker needs to see and use the screen.")
	r.pass()
	if r.cards.status(res.Card.ID) != "asked" || len(r.cards.tellings()) != 0 {
		t.Fatalf("status %s", r.cards.status(res.Card.ID))
	}
	r.backend.set(ScreenRecording, Granted)
	r.pass()
	r.pass()
	if r.cards.status(res.Card.ID) != "granted" {
		t.Fatalf("status %s", r.cards.status(res.Card.ID))
	}
	want := res.Card.ID + ": Screen recording is now allowed on this Mac. You can start the Worker again with the screen."
	if told := r.cards.tellings(); len(told) != 1 || told[0] != want {
		t.Fatalf("told %q", told)
	}
	if card, _ := r.cards.GrantRequests(); card[0].GrantSealedAt == nil {
		t.Fatal("no sealed time")
	}
	// Sealed: not looked at again.
	before := r.backend.checkCount()
	r.pass()
	if r.backend.checkCount() != before {
		t.Fatal("a sealed card is still checked")
	}
}

func TestStaleAccessibilityTriesARestartInPlaceThenOffersTheRestart(t *testing.T) {
	r := newRig()
	res, _ := r.m.Need(context.Background(), Accessibility, "A Worker needs to click and type.")
	id := res.Card.ID
	r.backend.set(Accessibility, Stale)

	// Busy: it waits for a quiet moment, the card stays as it is.
	r.restart.quiet = false
	r.pass()
	if in, _ := r.restart.counts(); in != 0 || r.cards.status(id) != "asked" {
		t.Fatalf("restarted while busy: %d %s", in, r.cards.status(id))
	}
	// Quiet: once, in place, and nothing else happens in the card meanwhile.
	r.restart.quiet = true
	r.pass()
	r.pass()
	if in, _ := r.restart.counts(); in != 1 || r.cards.status(id) != "asked" {
		t.Fatalf("in place %d, status %s", in, r.cards.status(id))
	}
	if card, _ := r.cards.GrantRequests(); !card[0].GrantRestartTried {
		t.Fatal("the attempt is not kept")
	}
	// The process came back (a new manager) and the grant still does not count: the card offers the restart.
	next := New(r.backend, r.cards, r.restart)
	next.now = r.m.now
	next.afterFunc = r.m.afterFunc
	for _, card := range next.open() {
		next.check(context.Background(), card)
	}
	if r.cards.status(id) != "restart" {
		t.Fatalf("status %s", r.cards.status(id))
	}
	if in, _ := r.restart.counts(); in != 1 {
		t.Fatalf("a second attempt: %d", in)
	}
	// Its one button restarts the process, after the answer is out.
	event, err := r.m.Act(context.Background(), id, ActionRestart)
	if err != nil || event.Status != "restart" {
		t.Fatalf("%+v %v", event, err)
	}
	if _, proc := r.restart.counts(); proc != 0 {
		t.Fatal("restarted before the answer went out")
	}
	for _, f := range r.later {
		f()
	}
	if _, proc := r.restart.counts(); proc != 1 {
		t.Fatalf("process restarts: %d", proc)
	}
	// And once it counts, the card seals with no more steps.
	r.backend.set(Accessibility, Granted)
	next.check(context.Background(), brain.TimelineItem{ID: id, GrantPermission: Accessibility})
	if r.cards.status(id) != "granted" || len(r.cards.tellings()) != 1 {
		t.Fatalf("status %s, told %q", r.cards.status(id), r.cards.tellings())
	}
}

func TestStaleAccessibilityThatCountsAfterTheRestartNeverShowsTheButton(t *testing.T) {
	r := newRig()
	res, _ := r.m.Need(context.Background(), Accessibility, "To click.")
	r.backend.set(Accessibility, Stale)
	r.pass()
	// The restart worked: the new process sees it granted.
	r.backend.set(Accessibility, Granted)
	next := New(r.backend, r.cards, r.restart)
	next.now = r.m.now
	for _, card := range next.open() {
		next.check(context.Background(), card)
	}
	if r.cards.status(res.Card.ID) != "granted" {
		t.Fatalf("status %s", r.cards.status(res.Card.ID))
	}
}

func TestStaleFallsBackToTheRestartWhenInPlaceFailsOrNoQuietMomentComes(t *testing.T) {
	r := newRig()
	res, _ := r.m.Need(context.Background(), Accessibility, "To click.")
	r.backend.set(Accessibility, Stale)
	r.restart.inPlaceErr = errors.New("cannot exec")
	r.pass()
	if r.cards.status(res.Card.ID) != "restart" {
		t.Fatalf("status %s", r.cards.status(res.Card.ID))
	}

	r = newRig()
	res, _ = r.m.Need(context.Background(), Accessibility, "To click.")
	r.backend.set(Accessibility, Stale)
	r.restart.quiet = false
	r.pass()
	r.clock = r.clock.Add(r.m.QuietWait)
	r.pass()
	if in, _ := r.restart.counts(); in != 0 || r.cards.status(res.Card.ID) != "restart" {
		t.Fatalf("in place %d, status %s", in, r.cards.status(res.Card.ID))
	}
}

// Run polls only while a card is open, and a new card wakes it.
func TestRunPollsOnlyWhileACardIsOpen(t *testing.T) {
	r := newRig()
	r.m.Poll = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.m.Run(ctx); close(done) }()
	time.Sleep(60 * time.Millisecond)
	if n := r.backend.checkCount(); n != 0 {
		t.Fatalf("%d grant checks with no open card", n)
	}
	res, _ := r.m.Need(ctx, ScreenRecording, "To see.")
	// Need itself checks once; the loop then follows the card.
	r.backend.set(ScreenRecording, Granted)
	deadline := time.Now().Add(2 * time.Second)
	for r.cards.status(res.Card.ID) != "granted" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.cards.status(res.Card.ID) != "granted" {
		t.Fatal("the loop did not seal the card")
	}
	time.Sleep(40 * time.Millisecond)
	settled := r.backend.checkCount()
	time.Sleep(60 * time.Millisecond)
	if r.backend.checkCount() != settled {
		t.Fatal("still polling with every card sealed")
	}
	cancel()
	<-done
}
