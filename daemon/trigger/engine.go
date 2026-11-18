package trigger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is what a Trigger appends to its Work.
type Event struct {
	WorkID      string
	Kind        string
	DedupeKey   string
	PayloadRef  string
	Summary     string
	DetailsJSON string
	Actionable  bool
}

// Recorded is the appended event; Actionable is false when the Work could
// not take a wake (it already has a typed wait or an owner).
type Recorded struct {
	ID         string
	Actionable bool
}

// Sink is the Work store the Trigger reports into (Brain's, in the daemon).
type Sink interface {
	Append(Event) (Recorded, error)
	// EventHandled says whether Brain handled the event; found is false
	// when the Work no longer has it.
	EventHandled(workID, eventID string) (handled, found bool, err error)
}

// Engine runs every Trigger's checks on one timer.
type Engine struct {
	store *Store
	sink  Sink
	run   RunFunc
	now   func() time.Time
	sem   chan struct{}
	wake  chan struct{}

	mu      sync.Mutex
	running map[string]bool
	// noticeTried paces notice retries of paused Triggers (they run no
	// checks to retry on).
	noticeTried map[string]time.Time
}

// MaxConcurrentChecks bounds checks running at once across all Triggers.
const MaxConcurrentChecks = 2

func NewEngine(store *Store, sink Sink, run RunFunc) *Engine {
	if run == nil {
		run = RunCheck
	}
	return &Engine{
		store: store, sink: sink, run: run, now: time.Now,
		sem: make(chan struct{}, MaxConcurrentChecks), wake: make(chan struct{}, 1),
		running: map[string]bool{}, noticeTried: map[string]time.Time{},
	}
}

// retryPausedNotice retries a paused Trigger's held notice at most once a
// minute, so the runaway notice reaches Brain once the Work is free.
func (e *Engine) retryPausedNotice(id string, now time.Time) {
	e.mu.Lock()
	if last, ok := e.noticeTried[id]; ok && now.Sub(last) < time.Minute {
		e.mu.Unlock()
		return
	}
	e.noticeTried[id] = now
	e.mu.Unlock()
	item, err := e.store.Get(id)
	if err != nil || item.HeldNotice == nil {
		return
	}
	next := item
	e.retryNotice(&next)
	_, _ = e.store.Update(id, func(current *Trigger) (bool, bool, error) {
		current.HeldNotice = next.HeldNotice
		return true, false, nil
	})
}

// SetClock replaces the engine's clock (tests).
func (e *Engine) SetClock(now func() time.Time) {
	e.now = now
	e.store.now = now
}

func (e *Engine) Store() *Store { return e.store }

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run checks each Trigger when it is due until ctx ends. It sleeps on one
// timer armed for the earliest due check, and looks again when a Trigger
// changes; a one-minute safety net covers a clock jump or sleep.
func (e *Engine) Run(ctx context.Context) {
	for {
		now := e.now()
		next := now.Add(time.Minute)
		for _, item := range e.store.List() {
			if item.Paused && item.HeldNotice != nil {
				e.retryPausedNotice(item.ID, now)
			}
			due := item.NextCheckAt()
			if due.IsZero() || e.isRunning(item.ID) {
				continue
			}
			if !due.After(now) {
				e.start(ctx, item.ID)
				continue
			}
			if due.Before(next) {
				next = due
			}
		}
		timer := time.NewTimer(max(next.Sub(now), 50*time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-e.store.Changed():
			timer.Stop()
		case <-e.wake:
			timer.Stop()
		}
	}
}

func (e *Engine) isRunning(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[id]
}

func (e *Engine) claim(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[id] {
		return false
	}
	e.running[id] = true
	return true
}

func (e *Engine) release(id string) {
	e.mu.Lock()
	delete(e.running, id)
	e.mu.Unlock()
	e.poke()
}

func (e *Engine) start(ctx context.Context, id string) {
	if !e.claim(id) {
		return
	}
	go func() {
		defer e.release(id)
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-e.sem }()
		if _, err := e.checkOnce(ctx, id); err != nil && !errors.Is(err, ErrNotFound) && ctx.Err() == nil {
			log.Printf("WARN trigger %s: %v", id, err)
		}
	}()
}

var ErrBusy = errors.New("a check of this Trigger is already running")

// CheckNow runs one check at once and returns what it did.
func (e *Engine) CheckNow(ctx context.Context, id string) (Trigger, Result, error) {
	item, err := e.store.Get(id)
	if err != nil {
		return Trigger{}, Result{}, err
	}
	if item.Paused {
		return item, Result{}, fmt.Errorf("%w: the Trigger is paused; resume it first", ErrInvalid)
	}
	if !e.claim(item.ID) {
		return item, Result{}, ErrBusy
	}
	defer e.release(item.ID)
	select {
	case e.sem <- struct{}{}:
	case <-ctx.Done():
		return item, Result{}, ctx.Err()
	}
	defer func() { <-e.sem }()
	return e.checkOnceResult(ctx, item.ID)
}

func (e *Engine) checkOnce(ctx context.Context, id string) (Trigger, error) {
	item, _, err := e.checkOnceResult(ctx, id)
	return item, err
}

func (e *Engine) checkOnceResult(ctx context.Context, id string) (Trigger, Result, error) {
	item, err := e.store.Get(id)
	if err != nil {
		return Trigger{}, Result{}, err
	}
	if item.Paused {
		return item, Result{}, nil
	}
	result := e.run(ctx, item)
	if ctx.Err() != nil {
		// The daemon is stopping: a check it cut short says nothing.
		return item, result, ctx.Err()
	}
	at := e.now().UTC()
	next := item
	e.apply(&next, result, at)
	saved, err := e.store.Update(id, func(current *Trigger) (bool, bool, error) {
		mergeRuntime(current, next)
		return true, false, nil
	})
	return saved, result, err
}

// mergeRuntime copies what a check changed onto the current Trigger, which
// the person may have edited (paused, retitled) while the check ran.
func mergeRuntime(current *Trigger, next Trigger) {
	current.LastCheckedAt = next.LastCheckedAt
	current.LastOutputHash = next.LastOutputHash
	current.Failures = next.Failures
	current.FailingSince = next.FailingSince
	current.LastFailure = next.LastFailure
	current.Failing = next.Failing
	current.FailingNotifiedAt = next.FailingNotifiedAt
	current.GapFrom, current.GapTo = next.GapFrom, next.GapTo
	current.FireSeq = next.FireSeq
	current.Firings = next.Firings
	current.Pending = next.Pending
	current.Wakes = next.Wakes
	current.NoticeSeq = next.NoticeSeq
	current.HeldNotice = next.HeldNotice
	if next.Paused && next.PauseReason == PauseRunaway {
		current.Paused, current.PauseReason = true, PauseRunaway
	}
}

// apply records one check's result on t, appending the events it causes.
// A check that prints nothing new changes only t's own row.
func (e *Engine) apply(t *Trigger, result Result, at time.Time) {
	last := t.LastCheckedAt
	if t.ResumedAt != nil && (last == nil || t.ResumedAt.After(*last)) {
		last = t.ResumedAt
	}
	if last != nil && at.Sub(*last) > GapFactor*t.Interval() {
		from := *last
		t.GapFrom, t.GapTo = &from, &at
	}
	t.LastCheckedAt = &at
	var kept []time.Time
	for _, wake := range t.Wakes {
		if at.Sub(wake) < time.Hour {
			kept = append(kept, wake)
		}
	}
	t.Wakes = kept
	e.refreshLatest(t, at)
	e.retryNotice(t)

	if result.Failed() {
		if t.Failures == 0 || t.FailingSince == nil {
			since := at
			t.FailingSince = &since
		}
		t.Failures++
		t.LastFailure = clip(result.Failure(), MaxStderrBytes+256)
		if t.Failures >= FailAfterChecks && at.Sub(*t.FailingSince) >= FailAfter &&
			(t.FailingNotifiedAt == nil || at.Sub(*t.FailingNotifiedAt) >= FailReminder) {
			t.Failing = true
			t.FailingNotifiedAt = &at
			payload := e.writeFile(t.ID, "failure.txt", fmt.Sprintf("# %s: %d failed checks in a row since %s\n# check: %s\n%s\n",
				at.Format(time.RFC3339), t.Failures, t.FailingSince.Format(time.RFC3339), t.Check, t.LastFailure))
			e.notice(t, KindFailing, true, payload,
				fmt.Sprintf("Trigger %q keeps failing (%d checks in a row): %s", t.Title, t.Failures, failureLine(t.LastFailure)))
		}
		e.flush(t, at)
		return
	}
	if t.Failing {
		e.notice(t, KindRecovered, false, "", fmt.Sprintf("Trigger %q checks again.", t.Title))
	}
	t.Failures, t.FailingSince, t.LastFailure, t.Failing, t.FailingNotifiedAt = 0, nil, "", false, nil

	output := strings.TrimRight(result.Stdout, " \t\r\n")
	if strings.TrimSpace(output) == "" {
		output = ""
	}
	if output != "" && result.Truncated {
		output += fmt.Sprintf("\n[output cut at %d KiB]", MaxOutputBytes/1024)
	}
	hash := ""
	if output != "" {
		sum := sha256.Sum256([]byte(output))
		hash = hex.EncodeToString(sum[:])
	}
	changed := hash != "" && hash != t.LastOutputHash
	t.LastOutputHash = hash
	if changed {
		e.gather(t, Output{At: at, Text: output})
	}
	e.flush(t, at)
}

// refreshLatest asks the Work whether Brain handled the newest firing.
func (e *Engine) refreshLatest(t *Trigger, at time.Time) bool {
	latest := t.LatestFiring()
	if !latest.open() || latest.EventID == "" || e.sink == nil {
		return false
	}
	handled, found, err := e.sink.EventHandled(t.WorkID, latest.EventID)
	switch {
	case err != nil:
		return false
	case !found:
		latest.Lost = true
	case handled:
		when := at
		latest.HandledAt = &when
	default:
		return false
	}
	return true
}

// gather adds a changed output to the next wake. A held firing (it has not
// reached Brain, so nobody read it) takes it; otherwise it waits in Pending
// for the firing Brain holds now, so nothing it already read changes under
// it and nothing gathered meanwhile is lost.
func (e *Engine) gather(t *Trigger, output Output) {
	if latest := t.LatestFiring(); latest.open() && latest.EventID == "" {
		e.appendPayload(t.ID, latest, []Output{output})
		return
	}
	if t.Pending == nil {
		t.Pending = &Pending{Since: output.At}
	}
	t.Pending.Outputs = append(t.Pending.Outputs, output)
	for size := pendingSize(t.Pending); size > MaxPayloadBytes && len(t.Pending.Outputs) > 1; size = pendingSize(t.Pending) {
		t.Pending.Outputs = t.Pending.Outputs[1:]
	}
}

func pendingSize(p *Pending) int {
	size := 0
	for _, output := range p.Outputs {
		size += len(output.Text) + 40
	}
	return size
}

// flush wakes Brain when there is something to wake it for: a held firing
// is retried, and pending outputs become a firing once the previous one is
// handled.
func (e *Engine) flush(t *Trigger, at time.Time) {
	latest := t.LatestFiring()
	if latest.open() && latest.EventID == "" {
		e.deliver(t, latest, at)
		return
	}
	if t.Pending == nil || len(t.Pending.Outputs) == 0 || latest.open() {
		return
	}
	if len(t.Wakes) >= WakesPerHour {
		t.Paused, t.PauseReason = true, PauseRunaway
		e.notice(t, KindPaused, true, "",
			fmt.Sprintf("Trigger %q paused itself: it woke Brain %d times within an hour. Its latest output waits until it is resumed (mewla trigger resume -id %s).", t.Title, WakesPerHour, t.ID))
		return
	}
	t.FireSeq++
	firing := Firing{Seq: t.FireSeq, At: at}
	firing.Payload = filepath.Join(e.store.Dir(t.ID), "fires", strconv.Itoa(firing.Seq)+".txt")
	e.appendPayload(t.ID, &firing, t.Pending.Outputs)
	t.Pending = nil
	t.Firings = append(t.Firings, firing)
	e.prune(t)
	e.deliver(t, &t.Firings[len(t.Firings)-1], at)
}

func (e *Engine) deliver(t *Trigger, firing *Firing, at time.Time) {
	if e.sink == nil {
		return
	}
	key := fmt.Sprintf("trigger:%s:fire:%d", t.ID, firing.Seq)
	if firing.Tries > 0 {
		key += ":try:" + strconv.Itoa(firing.Tries)
	}
	firing.Tries++
	preview := ""
	if raw, err := os.ReadFile(firing.Payload); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "# ") {
				preview = firstLine(line)
				break
			}
		}
	}
	summary := fmt.Sprintf("Trigger %q fired: %s", t.Title, preview)
	if firing.Outputs > 1 {
		summary += fmt.Sprintf(" (%d changes)", firing.Outputs)
	}
	details, _ := json.Marshal(map[string]any{
		"trigger_id": t.ID, "title": t.Title, "firing": firing.Seq, "outputs": firing.Outputs,
		"payload": firing.Payload, "instruction": t.Instruction, "check": t.Check,
	})
	recorded, err := e.sink.Append(Event{
		WorkID: t.WorkID, Kind: KindFired, DedupeKey: key, PayloadRef: firing.Payload,
		Summary: summary, DetailsJSON: string(details), Actionable: true,
	})
	if err != nil {
		log.Printf("WARN trigger %s: firing %d did not reach its Work: %v", t.ID, firing.Seq, err)
		return
	}
	if !recorded.Actionable {
		// The Work cannot take a wake right now; the next check tries again.
		return
	}
	firing.EventID = recorded.ID
	t.Wakes = append(t.Wakes, at)
}

// notice tells Brain about the watching itself: failing and paused are
// actionable and retried until they reach Brain; recovered is a record.
func (e *Engine) notice(t *Trigger, kind string, actionable bool, payload, summary string) {
	if !actionable {
		e.sendNotice(t, Notice{Kind: kind, Seq: t.NoticeSeq, Payload: payload, Summary: summary}, false)
		return
	}
	t.NoticeSeq++
	held := Notice{Kind: kind, Seq: t.NoticeSeq, Payload: payload, Summary: summary}
	t.HeldNotice = &held
	e.retryNotice(t)
}

func (e *Engine) retryNotice(t *Trigger) {
	if t.HeldNotice == nil || t.LatestFiring().open() && t.LatestFiring().EventID != "" {
		// The firing Brain holds is the Work's Review; wait for it.
		return
	}
	if e.sendNotice(t, *t.HeldNotice, true) {
		t.HeldNotice = nil
		return
	}
	t.HeldNotice.Tries++
}

func (e *Engine) sendNotice(t *Trigger, notice Notice, actionable bool) bool {
	if e.sink == nil {
		return false
	}
	key := fmt.Sprintf("trigger:%s:%s:%d", t.ID, strings.TrimPrefix(notice.Kind, "trigger."), notice.Seq)
	if notice.Tries > 0 {
		key += ":try:" + strconv.Itoa(notice.Tries)
	}
	details, _ := json.Marshal(map[string]any{"trigger_id": t.ID, "title": t.Title, "check": t.Check, "failure": t.LastFailure})
	recorded, err := e.sink.Append(Event{
		WorkID: t.WorkID, Kind: notice.Kind, DedupeKey: key, PayloadRef: notice.Payload,
		Summary: notice.Summary, DetailsJSON: string(details), Actionable: actionable,
	})
	if err != nil {
		log.Printf("WARN trigger %s: %s notice did not reach its Work: %v", t.ID, notice.Kind, err)
		return false
	}
	return recorded.Actionable
}

// appendPayload adds outputs to firing's payload file, up to MaxPayloadBytes.
func (e *Engine) appendPayload(id string, firing *Firing, outputs []Output) {
	if err := os.MkdirAll(filepath.Dir(firing.Payload), 0o700); err != nil {
		log.Printf("WARN trigger %s: payload directory: %v", id, err)
	}
	file, err := os.OpenFile(firing.Payload, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("WARN trigger %s: payload: %v", id, err)
		return
	}
	defer file.Close()
	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	for _, output := range outputs {
		block := fmt.Sprintf("# checked %s\n%s\n", output.At.Format(time.RFC3339), output.Text)
		if size+int64(len(block)) > MaxPayloadBytes {
			if size < MaxPayloadBytes {
				note := fmt.Sprintf("# more changes left out: the payload is capped at %d KiB\n", MaxPayloadBytes/1024)
				_, _ = file.WriteString(note)
				size += int64(len(note))
			}
			firing.Outputs++
			continue
		}
		if _, err := file.WriteString(block); err == nil {
			size += int64(len(block))
		}
		firing.Outputs++
	}
}

func (e *Engine) writeFile(id, name, body string) string {
	path := filepath.Join(e.store.Dir(id), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return ""
	}
	return path
}

// prune keeps the newest KeepFirings firings and deletes older payloads.
func (e *Engine) prune(t *Trigger) {
	if len(t.Firings) <= KeepFirings {
		return
	}
	drop := t.Firings[:len(t.Firings)-KeepFirings]
	fires := filepath.Join(e.store.Dir(t.ID), "fires") + string(filepath.Separator)
	for _, firing := range drop {
		if strings.HasPrefix(firing.Payload, fires) {
			_ = os.Remove(firing.Payload)
		}
	}
	t.Firings = append([]Firing(nil), t.Firings[len(t.Firings)-KeepFirings:]...)
}

// Pause stops checking until Resume.
func (e *Engine) Pause(id string) (Trigger, error) {
	return e.store.Update(id, func(t *Trigger) (bool, bool, error) {
		if t.Paused && t.PauseReason == "" {
			return false, false, nil
		}
		t.Paused, t.PauseReason = true, ""
		return true, true, nil
	})
}

// Resume checks again at once; a runaway's hour starts over.
func (e *Engine) Resume(id string) (Trigger, error) {
	return e.store.Update(id, func(t *Trigger) (bool, bool, error) {
		if !t.Paused {
			return false, false, nil
		}
		now := e.now().UTC()
		t.Paused, t.PauseReason, t.ResumedAt, t.Wakes = false, "", &now, nil
		return true, true, nil
	})
}

// Views are the Triggers as the person sees them now, with whether Brain
// handled each newest firing read fresh from the Work.
func (e *Engine) Views() []View {
	items := e.store.List()
	views := make([]View, 0, len(items))
	for _, item := range items {
		views = append(views, e.view(item))
	}
	return views
}

func (e *Engine) View(id string) (View, error) {
	item, err := e.store.Get(id)
	if err != nil {
		return View{}, err
	}
	return e.view(item), nil
}

func (e *Engine) view(item Trigger) View {
	now := e.now().UTC()
	if e.refreshLatest(&item, now) {
		latest := *item.LatestFiring()
		if saved, err := e.store.Update(item.ID, func(current *Trigger) (bool, bool, error) {
			if current := current.LatestFiring(); current != nil && current.Seq == latest.Seq {
				current.HandledAt, current.Lost = latest.HandledAt, latest.Lost
				return true, false, nil
			}
			return false, false, nil
		}); err == nil {
			item = saved
		}
	}
	return ViewOf(item, now)
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}
