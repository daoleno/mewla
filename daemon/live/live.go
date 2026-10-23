// Package live projects what the daemon already knows (running delegated
// Workers, Brain's open questions, the current objective) into one small
// "live state" the phone shows as an ongoing notification or a widget.
//
// It invents no state: every field is read from brain.Snapshot. The Projector
// numbers and de-duplicates the projection so a phone, a push and a widget
// all agree on which state is newest.
package live

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
)

// Kinds of live state. A question that needs the person outranks running work.
const (
	KindIdle     = "idle"
	KindRunning  = "running"
	KindNeedsYou = "needs_you"
)

// Question kinds say which path answers it.
const (
	QuestionWork = "work"
	QuestionChat = "chat"
)

const (
	maxWorkers      = 3
	maxChoices      = 3
	maxChoiceRunes  = 40
	maxQuestionText = 140
	maxTextRunes    = 80
)

// DefaultDebounce coalesces a burst of snapshot changes into one state.
const DefaultDebounce = 1500 * time.Millisecond

// Worker is one running Worker line.
type Worker struct {
	Name  string `json:"name"`
	Phase string `json:"phase,omitempty"`
}

// Question is what Brain asks the person. Choices are shortened for a
// notification; the full choices stay in the unexported field so an answer
// replies with exactly what Brain offered.
type Question struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	WorkID  string   `json:"work_id,omitempty"`
	Text    string   `json:"text,omitempty"`
	Choices []string `json:"choices"`

	full []string
}

// FullChoice returns the untruncated choice at index (0-based).
func (q *Question) FullChoice(index int) (string, bool) {
	if q == nil || index < 0 || index >= len(q.full) {
		return "", false
	}
	return q.full[index], true
}

// State is the wire projection. Epoch+Seq order states across daemon
// restarts: the epoch is the boot time, the sequence counts within a boot.
type State struct {
	Kind        string    `json:"kind"`
	Epoch       int64     `json:"epoch"`
	Seq         uint64    `json:"seq"`
	RunKey      string    `json:"run_key"`
	Title       string    `json:"title"`
	Phase       string    `json:"phase"`
	StartedAt   int64     `json:"started_at"`
	Done        int       `json:"done"`
	Total       int       `json:"total"`
	Workers     []Worker  `json:"workers"`
	WorkerCount int       `json:"worker_count"`
	NeedsCount  int       `json:"needs_count"`
	Question    *Question `json:"question"`
}

// Project is the pure projection of a Brain snapshot.
func Project(snapshot brain.Snapshot, now time.Time) State {
	state := State{Kind: KindIdle, Workers: []Worker{}}
	if snapshot.Objective != nil && snapshot.Objective.Total > 0 {
		state.Total = snapshot.Objective.Total
		state.Done = snapshot.Objective.Done
		if state.Done > state.Total {
			state.Done = state.Total
		}
		if state.Done < 0 {
			state.Done = 0
		}
	}

	running := runningWorkers(snapshot.Workers, now)
	if len(running) > 0 {
		state.Kind = KindRunning
		state.WorkerCount = len(running)
		for index, worker := range running {
			if index == maxWorkers {
				break
			}
			state.Workers = append(state.Workers, Worker{
				Name:  clip(strings.TrimSpace(worker.ref.Name), maxTextRunes),
				Phase: clip(strings.TrimSpace(worker.ref.Phase), maxTextRunes),
			})
		}
		// The earliest start drives the elapsed clock; the newest start
		// identifies the run, so a new Worker beginning is a new run.
		state.StartedAt = running[0].started.UnixMilli()
		newest := running[0]
		for _, worker := range running {
			if worker.started.After(newest.started) {
				newest = worker
			}
		}
		state.RunKey = "run:" + newest.ref.ID + "@" + itoa(newest.started.UnixMilli())
		if len(running) == 1 {
			state.Title = clip(workTitleFor(snapshot.CurrentWork, newest.ref), maxTextRunes)
			state.Phase = clip(strings.TrimSpace(newest.ref.Phase), maxTextRunes)
		} else {
			state.Title = itoa(int64(len(running))) + " workers"
		}
	}

	needs, question := questionFor(snapshot)
	state.NeedsCount = needs
	if question != nil {
		state.Kind = KindNeedsYou
		state.Question = question
		state.RunKey = "ask:" + question.ID
		if state.Title == "" && question.Kind == QuestionWork {
			state.Title = clip(workTitle(snapshot.CurrentWork, question.WorkID), maxTextRunes)
		}
	}
	return state
}

type runningWorker struct {
	ref     brain.WorkerRef
	started time.Time
}

// runningWorkers are the delegated, visible Workers the watcher classifies as
// running, earliest start first.
func runningWorkers(workers []brain.WorkerRef, now time.Time) []runningWorker {
	var out []runningWorker
	for _, ref := range workers {
		if ref.Hidden || !ref.Delegated || ref.Status != string(classifier.StateRunning) {
			continue
		}
		started := ref.Updated
		if ref.StartedAt != nil && !ref.StartedAt.IsZero() {
			started = *ref.StartedAt
		}
		if started.IsZero() {
			started = now
		}
		out = append(out, runningWorker{ref: ref, started: started})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].started.Before(out[j].started) })
	return out
}

func workTitleFor(current []brain.CurrentWork, ref brain.WorkerRef) string {
	for _, item := range current {
		if item.AttemptSessionID == ref.ID && strings.TrimSpace(item.Title) != "" {
			return strings.TrimSpace(item.Title)
		}
	}
	return strings.TrimSpace(ref.Name)
}

func workTitle(current []brain.CurrentWork, workID string) string {
	for _, item := range current {
		if item.ID == workID {
			return strings.TrimSpace(item.Title)
		}
	}
	return ""
}

// questionFor returns how many things wait on the person and the one to show.
// A Work's own question (structured, answered through the Work) wins over the
// chat chips; among Work questions the one that has waited the least shows.
func questionFor(snapshot brain.Snapshot) (int, *Question) {
	count := 0
	var newest *brain.CurrentWork
	var newestAt time.Time
	for index := range snapshot.CurrentWork {
		item := &snapshot.CurrentWork[index]
		if strings.TrimSpace(item.Question) == "" {
			continue
		}
		count++
		at := item.UpdatedAt
		if item.AttentionSince != nil {
			at = *item.AttentionSince
		}
		if newest == nil || at.After(newestAt) {
			newest, newestAt = item, at
		}
	}
	chat := snapshot.QuickReplies != nil && len(flattenChoices(snapshot.QuickReplies.Rows)) > 0
	if chat {
		count++
	}
	if newest != nil {
		full := firstChoices(newest.Choices)
		since := ""
		if newest.AttentionSince != nil {
			since = newest.AttentionSince.UTC().Format(time.RFC3339Nano)
		}
		return count, &Question{
			ID:      questionID(QuestionWork, newest.ID, since, strings.TrimSpace(newest.Question), strings.Join(full, "\x00")),
			Kind:    QuestionWork,
			WorkID:  newest.ID,
			Text:    clip(strings.Join(strings.Fields(newest.Question), " "), maxQuestionText),
			Choices: clipAll(full),
			full:    full,
		}
	}
	if chat {
		full := firstChoices(flattenChoices(snapshot.QuickReplies.Rows))
		return count, &Question{
			ID:      questionID(QuestionChat, "", snapshot.QuickReplies.At.UTC().Format(time.RFC3339Nano), "", strings.Join(full, "\x00")),
			Kind:    QuestionChat,
			Choices: clipAll(full),
			full:    full,
		}
	}
	return count, nil
}

func flattenChoices(rows [][]string) []string {
	var out []string
	for _, row := range rows {
		for _, chip := range row {
			if chip = strings.TrimSpace(chip); chip != "" {
				out = append(out, chip)
			}
		}
	}
	return out
}

func firstChoices(values []string) []string {
	out := make([]string, 0, maxChoices)
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			continue
		}
		out = append(out, value)
		if len(out) == maxChoices {
			break
		}
	}
	return out
}

func clipAll(values []string) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = clip(value, maxChoiceRunes)
	}
	return out
}

func questionID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:8])
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }

// materialKey is what makes two states the same: everything a person would
// see change. The elapsed clock (started_at) and the numbering are excluded.
func materialKey(state State) string {
	state.Epoch, state.Seq, state.StartedAt = 0, 0, 0
	data, _ := json.Marshal(state)
	return string(data)
}

// Projector numbers projections and reports each distinct one once.
type Projector struct {
	mu       sync.Mutex
	epoch    int64
	seq      uint64
	debounce time.Duration
	onChange func(State)
	now      func() time.Time

	emitted    bool
	emittedKey string
	current    State
	pending    *State
	timer      *time.Timer
}

// NewProjector starts numbering at the given boot epoch (unix ms). onChange is
// called without the projector's lock held.
func NewProjector(epoch int64, debounce time.Duration, onChange func(State)) *Projector {
	return &Projector{
		epoch: epoch, debounce: debounce, onChange: onChange, now: time.Now,
		current: State{Kind: KindIdle, Epoch: epoch, Workers: []Worker{}},
	}
}

// Current is the newest state that was reported (idle before the first).
func (p *Projector) Current() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}

// Observe projects a snapshot. A change is reported after the debounce, with
// the newest projection of the burst; no change reports nothing.
func (p *Projector) Observe(snapshot brain.Snapshot) {
	candidate := Project(snapshot, p.now())
	key := materialKey(candidate)

	p.mu.Lock()
	if p.emitted && key == p.emittedKey {
		// Back to what was last reported: drop a pending flap, but keep the
		// newest elapsed-clock anchor for the next report.
		p.pending = nil
		p.current.StartedAt = candidate.StartedAt
		p.mu.Unlock()
		return
	}
	p.pending = &candidate
	if p.debounce <= 0 {
		p.mu.Unlock()
		p.flush()
		return
	}
	if p.timer == nil {
		p.timer = time.AfterFunc(p.debounce, p.flush)
	}
	p.mu.Unlock()
}

// ObserveNow projects a snapshot and reports it at once (no debounce), for a
// phone that is asking what is happening right now. It returns the state.
func (p *Projector) ObserveNow(snapshot brain.Snapshot) State {
	candidate := Project(snapshot, p.now())
	key := materialKey(candidate)
	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if p.emitted && key == p.emittedKey {
		p.pending = nil
		p.current.StartedAt = candidate.StartedAt
		current := p.current
		p.mu.Unlock()
		return current
	}
	p.pending = &candidate
	p.mu.Unlock()
	p.flush()
	return p.Current()
}

func (p *Projector) flush() {
	p.mu.Lock()
	p.timer = nil
	if p.pending == nil {
		p.mu.Unlock()
		return
	}
	state := *p.pending
	p.pending = nil
	key := materialKey(state)
	if p.emitted && key == p.emittedKey {
		p.mu.Unlock()
		return
	}
	p.seq++
	state.Epoch, state.Seq = p.epoch, p.seq
	p.emitted, p.emittedKey, p.current = true, key, state
	onChange := p.onChange
	p.mu.Unlock()
	if onChange != nil {
		onChange(state)
	}
}

// Stop cancels a pending report.
func (p *Projector) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.pending = nil
}
