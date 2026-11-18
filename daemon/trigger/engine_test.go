package trigger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu        sync.Mutex
	events    []Event
	ids       []string
	handled   map[string]bool
	missing   map[string]bool
	demote    bool
	open      string
	appendErr error
}

func (s *fakeSink) Append(event Event) (Recorded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appendErr != nil {
		return Recorded{}, s.appendErr
	}
	for index, existing := range s.events {
		if existing.WorkID == event.WorkID && existing.DedupeKey == event.DedupeKey {
			return Recorded{ID: s.ids[index], Actionable: existing.Actionable}, nil
		}
	}
	if s.demote {
		event.Actionable = false
	}
	id := fmt.Sprintf("ev-%d", len(s.events)+1)
	// A Work holds one Review at a time: an actionable event opens one only
	// when the previous one is handled.
	if event.Actionable && s.open != "" && !s.handled[s.open] && !s.missing[s.open] {
		event.Actionable = false
	}
	if event.Actionable {
		s.open = id
	}
	s.events = append(s.events, event)
	s.ids = append(s.ids, id)
	return Recorded{ID: id, Actionable: event.Actionable}, nil
}

func (s *fakeSink) EventHandled(_, id string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.missing[id] {
		return false, false, nil
	}
	return s.handled[id], true, nil
}

func (s *fakeSink) handle(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handled == nil {
		s.handled = map[string]bool{}
	}
	s.handled[id] = true
}

// handleAll is Brain handling everything raised so far.
func (s *fakeSink) handleAll() {
	for _, id := range s.ids {
		s.handle(id)
	}
}

func (s *fakeSink) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kinds []string
	for _, event := range s.events {
		mark := ""
		if event.Actionable {
			mark = "!"
		}
		kinds = append(kinds, event.Kind+mark)
	}
	return kinds
}

func (s *fakeSink) wakes() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	for _, event := range s.events {
		if event.Kind == KindFired && event.Actionable {
			out = append(out, event)
		}
	}
	return out
}

type harness struct {
	t      *testing.T
	store  *Store
	sink   *fakeSink
	engine *Engine
	now    time.Time
	next   Result
	runs   int
	id     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := &harness{t: t, store: store, sink: &fakeSink{}, now: time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)}
	h.engine = NewEngine(store, h.sink, func(context.Context, Trigger) Result {
		h.runs++
		return h.next
	})
	h.engine.SetClock(func() time.Time { return h.now })
	item, err := store.Create(Trigger{Title: "PR loop", Check: "./poll.sh", IntervalSeconds: 600, WorkID: "work-1"})
	if err != nil {
		t.Fatal(err)
	}
	h.id = item.ID
	return h
}

// check advances the clock one interval and runs a check printing out.
func (h *harness) check(out string) Trigger {
	h.t.Helper()
	return h.result(Result{Stdout: out})
}

func (h *harness) result(result Result) Trigger {
	h.t.Helper()
	h.now = h.now.Add(10 * time.Minute)
	h.next = result
	item, _, err := h.engine.CheckNow(context.Background(), h.id)
	if err != nil {
		h.t.Fatalf("check: %v", err)
	}
	return item
}

func (h *harness) payload(event Event) string {
	h.t.Helper()
	raw, err := os.ReadFile(event.PayloadRef)
	if err != nil {
		h.t.Fatalf("payload %s: %v", event.PayloadRef, err)
	}
	return string(raw)
}

func TestSilentChecksAppendNothing(t *testing.T) {
	h := newHarness(t)
	for range 20 {
		h.check("")
		h.check("  \n")
	}
	if kinds := h.sink.kinds(); len(kinds) != 0 {
		t.Fatalf("silent checks appended %v", kinds)
	}
	item, _ := h.store.Get(h.id)
	if item.LastCheckedAt == nil || len(item.Firings) != 0 || item.Revision != 1 {
		t.Fatalf("silent checks: %+v", item)
	}
}

func TestPollShapeFiresOncePerNewOutput(t *testing.T) {
	h := newHarness(t)
	h.check("NEW perpetuo#1@aaaa\n")
	wakes := h.sink.wakes()
	if len(wakes) != 1 || !strings.Contains(h.payload(wakes[0]), "NEW perpetuo#1@aaaa") {
		t.Fatalf("first NEW: %v", h.sink.kinds())
	}
	if !strings.Contains(wakes[0].Summary, "NEW perpetuo#1@aaaa") || wakes[0].WorkID != "work-1" {
		t.Fatalf("event: %+v", wakes[0])
	}
	h.check("")
	h.check("")
	if len(h.sink.wakes()) != 1 {
		t.Fatalf("quiet checks woke Brain: %v", h.sink.kinds())
	}
	h.sink.handle(h.sink.ids[0])
	h.check("NEW perpetuo#2@bbbb\n")
	wakes = h.sink.wakes()
	if len(wakes) != 2 || !strings.Contains(h.payload(wakes[1]), "NEW perpetuo#2@bbbb") || strings.Contains(h.payload(wakes[1]), "#1@") {
		t.Fatalf("second NEW: %d wakes", len(wakes))
	}
}

func TestEdgeIsAgainstThePreviousCheck(t *testing.T) {
	h := newHarness(t)
	h.check("CI red on main")
	h.sink.handle(h.sink.ids[0])
	h.check("CI red on main")
	if len(h.sink.wakes()) != 1 {
		t.Fatal("an unchanged output fired again")
	}
	h.check("")
	h.check("CI red on main")
	if len(h.sink.wakes()) != 2 {
		t.Fatalf("a condition that came back did not fire: %v", h.sink.kinds())
	}
}

func TestOutputsWhileUnhandledCoalesceIntoOneWake(t *testing.T) {
	h := newHarness(t)
	h.check("NEW a")
	h.check("NEW b")
	h.check("NEW c")
	if len(h.sink.wakes()) != 1 {
		t.Fatalf("outputs while unhandled woke Brain again: %v", h.sink.kinds())
	}
	// What Brain already holds does not change under it.
	if first := h.payload(h.sink.wakes()[0]); strings.Contains(first, "NEW b") {
		t.Fatalf("the delivered payload changed: %q", first)
	}
	item, _ := h.store.Get(h.id)
	if item.Pending == nil || len(item.Pending.Outputs) != 2 {
		t.Fatalf("pending: %+v", item.Pending)
	}
	h.sink.handle(h.sink.ids[0])
	h.check("")
	wakes := h.sink.wakes()
	if len(wakes) != 2 {
		t.Fatalf("handled firing did not release the gathered outputs: %v", h.sink.kinds())
	}
	payload := h.payload(wakes[1])
	if !strings.Contains(payload, "NEW b") || !strings.Contains(payload, "NEW c") || !strings.Contains(wakes[1].Summary, "2 changes") {
		t.Fatalf("coalesced payload %q summary %q", payload, wakes[1].Summary)
	}
}

func TestHeldFiringGathersAndRetries(t *testing.T) {
	h := newHarness(t)
	h.sink.demote = true
	h.check("NEW a")
	h.check("NEW b")
	if len(h.sink.wakes()) != 0 {
		t.Fatal("a demoted event counted as a wake")
	}
	item, _ := h.store.Get(h.id)
	if len(item.Firings) != 1 || item.Firings[0].EventID != "" || item.Firings[0].Outputs != 2 {
		t.Fatalf("held firing: %+v", item.Firings)
	}
	h.sink.demote = false
	h.check("NEW b")
	wakes := h.sink.wakes()
	if len(wakes) != 1 || !strings.Contains(wakes[0].DedupeKey, ":try:") {
		t.Fatalf("held firing was not retried: %v", h.sink.kinds())
	}
	payload := h.payload(wakes[0])
	if !strings.Contains(payload, "NEW a") || !strings.Contains(payload, "NEW b") {
		t.Fatalf("held payload %q", payload)
	}
	if view := ViewOf(mustGet(t, h.store, h.id), h.now); !strings.Contains(view.Line, "with Brain") {
		t.Fatalf("line %q", view.Line)
	}
}

func TestSinkErrorHoldsTheFiring(t *testing.T) {
	h := newHarness(t)
	h.sink.appendErr = errors.New("brain down")
	h.check("NEW a")
	view := ViewOf(mustGet(t, h.store, h.id), h.now)
	if !strings.Contains(view.Line, "waiting to reach Brain") {
		t.Fatalf("line %q", view.Line)
	}
	h.sink.appendErr = nil
	h.check("")
	if len(h.sink.wakes()) != 1 {
		t.Fatalf("held firing not delivered after the Work came back: %v", h.sink.kinds())
	}
}

func TestFailingIsReportedOnceThenRecovers(t *testing.T) {
	h := newHarness(t)
	fail := Result{Exit: 1, StderrTail: "gh: HTTP 502"}
	h.result(fail)
	h.result(fail)
	h.result(fail)
	if len(h.sink.kinds()) != 0 {
		t.Fatalf("failing before 30 minutes: %v", h.sink.kinds())
	}
	item := h.result(fail) // 30 minutes after the first failure
	if !item.Failing {
		t.Fatalf("not failing after 4 failures over 30 min: %+v", item)
	}
	if got := h.sink.kinds(); len(got) != 1 || got[0] != KindFailing+"!" {
		t.Fatalf("events %v", got)
	}
	if !strings.Contains(h.sink.events[0].Summary, "gh: HTTP 502") {
		t.Fatalf("summary %q", h.sink.events[0].Summary)
	}
	if raw, err := os.ReadFile(h.sink.events[0].PayloadRef); err != nil || !strings.Contains(string(raw), "gh: HTTP 502") {
		t.Fatalf("failure payload %q %v", raw, err)
	}
	view := ViewOf(item, h.now)
	if view.State != StateFailing || view.Attention != AttentionFailing || !strings.Contains(view.Line, "failing: exit status 1") {
		t.Fatalf("view %+v", view)
	}
	paused := item
	paused.Paused = true
	if view := ViewOf(paused, h.now); view.Attention != AttentionNone {
		t.Fatalf("paused by the person, still asks: %+v", view.Attention)
	}
	h.sink.handleAll()
	for range 10 {
		h.result(fail)
	}
	if len(h.sink.kinds()) != 1 {
		t.Fatalf("reminded within a day: %v", h.sink.kinds())
	}
	h.now = h.now.Add(FailReminder)
	h.result(fail)
	if len(h.sink.kinds()) != 2 {
		t.Fatalf("no daily reminder: %v", h.sink.kinds())
	}
	h.sink.handleAll()
	item = h.check("")
	if item.Failing || item.Failures != 0 {
		t.Fatalf("not recovered: %+v", item)
	}
	if got := h.sink.kinds(); got[len(got)-1] != KindRecovered {
		t.Fatalf("recovered event %v", got)
	}
	// A failure does not move the edge: the output before it still counts.
	h.check("X")
	h.result(fail)
	h.check("X")
	if len(h.sink.wakes()) != 1 {
		t.Fatalf("a failure in between refired: %v", h.sink.kinds())
	}
}

func TestNoticeWaitsForTheOpenFiringThenReachesBrain(t *testing.T) {
	h := newHarness(t)
	h.check("NEW a") // a firing Brain has not handled holds the Work's Review
	fail := Result{Exit: 1, StderrTail: "boom"}
	for range 4 {
		h.result(fail)
	}
	item := mustGet(t, h.store, h.id)
	if !item.Failing || item.HeldNotice == nil || item.HeldNotice.Kind != KindFailing {
		t.Fatalf("failing notice not held: %+v", item.HeldNotice)
	}
	h.sink.handle(h.sink.ids[0])
	item = h.result(fail)
	if item.HeldNotice != nil {
		t.Fatalf("held notice not delivered: %+v", item.HeldNotice)
	}
	var delivered int
	for _, event := range h.sink.events {
		if event.Kind == KindFailing && event.Actionable {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("failing notices delivered %d: %v", delivered, h.sink.kinds())
	}
}

func TestRunawayPausesAndSaysSo(t *testing.T) {
	h := newHarness(t)
	for index := range WakesPerHour {
		h.now = h.now.Add(-9 * time.Minute) // six wakes inside an hour
		h.check(fmt.Sprintf("NEW %d", index))
		h.sink.handle(h.sink.ids[len(h.sink.ids)-1])
	}
	if len(h.sink.wakes()) != WakesPerHour {
		t.Fatalf("wakes %d", len(h.sink.wakes()))
	}
	h.now = h.now.Add(-9 * time.Minute)
	item := h.check("NEW 7")
	if !item.Paused || item.PauseReason != PauseRunaway {
		t.Fatalf("not paused: %+v", item)
	}
	got := h.sink.kinds()
	if got[len(got)-1] != KindPaused+"!" || len(h.sink.wakes()) != WakesPerHour {
		t.Fatalf("events %v", got)
	}
	view := ViewOf(item, h.now)
	if view.Attention != AttentionRunaway || !strings.Contains(view.Line, "paused: woke Brain 6 times in an hour") {
		t.Fatalf("view %+v", view)
	}
	if _, _, err := h.engine.CheckNow(context.Background(), h.id); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a paused Trigger checked: %v", err)
	}
	h.sink.handleAll()
	if _, err := h.engine.Resume(h.id); err != nil {
		t.Fatal(err)
	}
	h.check("NEW 7")
	wakes := h.sink.wakes()
	if len(wakes) != WakesPerHour+1 || !strings.Contains(h.payload(wakes[len(wakes)-1]), "NEW 7") {
		t.Fatalf("resume did not deliver the held output: %v", h.sink.kinds())
	}
}

func TestUnhandledFiringNeedsThePersonAfterThreeHours(t *testing.T) {
	h := newHarness(t)
	h.check("NEW a")
	view, _ := h.engine.View(h.id)
	if view.Attention != AttentionNone || !strings.Contains(view.Line, "fired just now · with Brain") {
		t.Fatalf("fresh firing %+v", view.Line)
	}
	h.now = h.now.Add(NotHandledAfter)
	view, _ = h.engine.View(h.id)
	if view.Attention != AttentionNotHandled || !strings.Contains(view.Line, "fired 3 h ago · not handled") {
		t.Fatalf("after 3 h: %q %q", view.Attention, view.Line)
	}
	h.sink.handle(h.sink.ids[0])
	view, _ = h.engine.View(h.id)
	if view.Attention != AttentionNone || !strings.Contains(view.Line, "· handled") || view.Handled == nil || !*view.Handled {
		t.Fatalf("handled: %+v", view)
	}
	if stored := mustGet(t, h.store, h.id); stored.Firings[0].HandledAt == nil {
		t.Fatal("handled state not kept")
	}
}

func TestLostEventIsNotHandled(t *testing.T) {
	h := newHarness(t)
	h.check("NEW a")
	h.sink.missing = map[string]bool{h.sink.ids[0]: true}
	view, _ := h.engine.View(h.id)
	if !strings.Contains(view.Line, "not handled") || view.Attention != AttentionNotHandled {
		t.Fatalf("line %q attention %q", view.Line, view.Attention)
	}
	h.check("NEW b")
	if len(h.sink.wakes()) != 2 {
		t.Fatal("a lost firing blocked the next one")
	}
}

func TestRetentionKeepsFiftyAndDeleteRemovesTheDirectory(t *testing.T) {
	h := newHarness(t)
	for index := range KeepFirings + 5 {
		h.check(fmt.Sprintf("NEW %d", index))
		h.sink.handle(h.sink.ids[len(h.sink.ids)-1])
		h.check("")
	}
	item := mustGet(t, h.store, h.id)
	if len(item.Firings) != KeepFirings || item.Firings[0].Seq != 6 {
		t.Fatalf("kept %d firings from %d", len(item.Firings), item.Firings[0].Seq)
	}
	files, _ := filepath.Glob(filepath.Join(h.store.Dir(h.id), "fires", "*.txt"))
	if len(files) != KeepFirings {
		t.Fatalf("%d payload files", len(files))
	}
	if _, err := os.Stat(filepath.Join(h.store.Dir(h.id), "fires", "1.txt")); !os.IsNotExist(err) {
		t.Fatalf("oldest payload kept: %v", err)
	}
	if _, err := h.store.Delete(h.id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.store.Dir(h.id)); !os.IsNotExist(err) {
		t.Fatalf("directory kept: %v", err)
	}
}

func TestGapIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.check("")
	h.now = h.now.Add(2 * time.Hour)
	item := h.check("")
	if item.GapFrom == nil || item.GapTo.Sub(*item.GapFrom) < 2*time.Hour {
		t.Fatalf("gap %+v", item)
	}
	if line := ViewOf(item, h.now).Line; !strings.Contains(line, "not checked for 2 h: Mewla was not running") {
		t.Fatalf("line %q", line)
	}
	if len(h.sink.kinds()) != 0 {
		t.Fatal("a gap fired")
	}
}

func TestStoreSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sink := &fakeSink{}
	engine := NewEngine(store, sink, func(context.Context, Trigger) Result { return Result{Stdout: "X"} })
	item, err := store.Create(Trigger{Title: "t", Check: "true", IntervalSeconds: 60, WorkID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.CheckNow(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	store, err = OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine = NewEngine(store, sink, func(context.Context, Trigger) Result { return Result{Stdout: "X"} })
	if _, _, err := engine.CheckNow(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	reopened := mustGet(t, store, item.ID)
	if len(reopened.Firings) != 1 || len(sink.wakes()) != 1 || reopened.LastOutputHash == "" {
		t.Fatalf("after reopen: %d firings, %v", len(reopened.Firings), sink.kinds())
	}
}

func TestRunChecksWhenDue(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ran := make(chan string, 4)
	engine := NewEngine(store, &fakeSink{}, func(_ context.Context, item Trigger) Result {
		ran <- item.ID
		return Result{}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)
	item, err := store.Create(Trigger{Title: "t", Check: "true", IntervalSeconds: 3600, WorkID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-ran:
		if id != item.ID {
			t.Fatalf("ran %s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a new Trigger was not checked")
	}
	select {
	case <-ran:
		t.Fatal("checked again before its interval")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestNormalize(t *testing.T) {
	for _, bad := range []Trigger{
		{Check: "x", WorkID: "w"},
		{Title: "t", WorkID: "w"},
		{Title: "t", Check: "x"},
		{Title: "t", Check: "x", Instruction: "i"},
		{Title: "t", Check: "x", WorkID: "w", IntervalSeconds: 30},
		{Title: "t", Check: "x", WorkID: "w", IntervalSeconds: 60, TimeoutSeconds: 60},
		{Title: "t", Check: "x", WorkID: "w", Cwd: "relative"},
	} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", bad)
		}
	}
	good, err := Trigger{Title: "t", Check: "x", WorkID: "w", IntervalSeconds: 60}.Normalize()
	if err != nil || good.TimeoutSeconds != 30 {
		t.Fatalf("%+v %v", good, err)
	}
	good, _ = Trigger{Title: "t", Check: "x", Instruction: "i", SourceThreadID: "th"}.Normalize()
	if good.IntervalSeconds != 600 || good.TimeoutSeconds != 60 {
		t.Fatalf("defaults %+v", good)
	}
}

func mustGet(t *testing.T, store *Store, id string) Trigger {
	t.Helper()
	item, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
