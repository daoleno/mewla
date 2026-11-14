// Package answered keeps one durable record per request a person (or Brain)
// answered: a Session's prompt or approval, a question an agent asked, a
// Brain Work question. The apps collapse an answered request to one sealed
// line ("Approved · git push origin main", who answered and when) on every
// device and after a restart; this ledger is what that line is read from.
//
// A record says what was asked (the short form a card shows), the answer, who
// gave it and when. It never decides anything: the answer paths record what
// they did, after doing it.
package answered

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Kind is what was asked.
const (
	KindPermission   = "permission"    // a Session's permission menu (Claude) on its screen
	KindPlan         = "plan"          // a plan approval menu
	KindChoice       = "choice"        // an agent's question with options (AskUserQuestion)
	KindStartup      = "startup"       // a startup dialog (trust this folder, ...)
	KindApproval     = "approval"      // an agent approval (hook, Codex, opencode, DSH)
	KindQuestion     = "question"      // an agent's free question (DSH)
	KindWorkQuestion = "work_question" // Brain's question on a Work
)

// Outcome is how it ended.
const (
	OutcomeAnswered    = "answered"     // chosen or replied
	OutcomeApproved    = "approved"     // an approval allowed
	OutcomeDenied      = "denied"       // an approval refused
	OutcomeNotAnswered = "not_answered" // it passed with no answer (timed out, the agent moved on)
	OutcomeDismissed   = "dismissed"    // the person cancelled it without answering
)

// By says who answered.
const (
	ByDevice   = "device"   // a paired device through the app; DeviceID says which
	ByBrain    = "brain"    // Brain, by its brief; Basis is the sentence it relied on
	ByOperator = "operator" // `mewla worker answer` outside Brain
	ByTerminal = "terminal" // in the Session's own terminal or agent UI
	ByTimeout  = "timeout"  // nobody answered in time
)

// Answerer is who gave the answer.
type Answerer struct {
	Kind       string `json:"kind"`
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	Basis      string `json:"basis,omitempty"`
}

// Anchor ties a record to the row of the request in its thread, when the
// request has one: the transcript tool call, the screen prompt, the approval.
type Anchor struct {
	CallID     string `json:"call_id,omitempty"`
	PromptID   string `json:"prompt_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	RPCID      string `json:"rpc_id,omitempty"`
}

// Request is one answered request.
type Request struct {
	// ID is stable per request: <kind family>:<session or work>:<native id>.
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`
	WorkID    string `json:"work_id,omitempty"`
	// ThreadID is the Brain chat thread the request was answered in, for
	// Brain's own requests and Work questions: a new chat shows only what
	// was answered there.
	ThreadID string `json:"thread_id,omitempty"`
	// Title is the card's kind line when the request names one (a tool, a
	// question header); Asked is the short form of what was asked.
	Title string `json:"title,omitempty"`
	// Tool and Summary say what a permission or approval asked, as the
	// agent's own tool call gives it: the tool's name ("Bash") and its one
	// line (the command, the file). Asked carries the same line when it is
	// known. They are absent on a request of another kind, on a record
	// written before they existed, and when the provider could not name the
	// call.
	Tool       string     `json:"tool,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Asked      string     `json:"asked"`
	Answer     string     `json:"answer,omitempty"`
	Outcome    string     `json:"outcome"`
	By         Answerer   `json:"by"`
	AskedAt    *time.Time `json:"asked_at,omitempty"`
	AnsweredAt time.Time  `json:"answered_at"`
	Anchor     Anchor     `json:"anchor,omitempty"`
}

// At is where the record sits in its thread: when it was asked, else when it
// was answered.
func (r Request) At() time.Time {
	if r.AskedAt != nil && !r.AskedAt.IsZero() {
		return *r.AskedAt
	}
	return r.AnsweredAt
}

// SessionLimit is how many records one Session keeps: a Session's thread
// shows its newest 240 events, so older sealed lines have no row to sit at.
// Brain Work questions (no Session) are kept like Brain's own timeline: all.
const SessionLimit = 240

const fieldLimit = 400

// Ledger is the durable set of answered requests, one per ID.
type Ledger struct {
	path    string
	mu      sync.Mutex
	byID    map[string]Request
	lines   int
	version atomic.Uint64
	subs    []func(Request)
	stamp   func(*Request)
}

// OnStamp lets the owner fill fields the answer path does not know (the
// Brain thread) on each record before it is kept.
func (l *Ledger) OnStamp(fn func(*Request)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.stamp = fn
	l.mu.Unlock()
}

// Open loads the ledger in stateDir.
func Open(stateDir string) (*Ledger, error) {
	l := &Ledger{path: filepath.Join(stateDir, "answered_requests.jsonl"), byID: map[string]Request{}}
	raw, err := os.ReadFile(l.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		l.lines++
		var record Request
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.ID == "" {
			continue
		}
		l.byID[record.ID] = record
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	l.pruneLocked()
	return l, nil
}

// OnRecord calls fn, outside the lock, with every record written.
func (l *Ledger) OnRecord(fn func(Request)) {
	if l == nil || fn == nil {
		return
	}
	l.mu.Lock()
	l.subs = append(l.subs, fn)
	l.mu.Unlock()
}

// Version moves on every write, so a projection can tell it is current.
func (l *Ledger) Version() uint64 {
	if l == nil {
		return 0
	}
	return l.version.Load()
}

// strength orders answerers for one request. The first explicit answer
// stands; an answer inferred from the prompt leaving the screen (terminal)
// gives way to the explicit one the app or Brain records a moment later.
func strength(kind string) int {
	switch kind {
	case ByDevice, ByBrain, ByOperator:
		return 2
	case ByTerminal, ByTimeout:
		return 1
	}
	return 0
}

// settlesTerminal is a terminal answer whose outcome was not known (answered)
// finding out how it ended (approved, denied) from the provider's record of
// the call. It is the same answer, told better, not a second one.
func settlesTerminal(existing, r Request) bool {
	return existing.By.Kind == ByTerminal && r.By.Kind == ByTerminal && existing.Outcome == OutcomeAnswered &&
		(r.Outcome == OutcomeApproved || r.Outcome == OutcomeDenied)
}

// Record keeps r unless the request already has an answer at least as
// explicit. It returns the record that stands and whether it was written.
func (l *Ledger) Record(r Request) (Request, bool, error) {
	if l == nil {
		return Request{}, false, nil
	}
	l.mu.Lock()
	stamp := l.stamp
	l.mu.Unlock()
	if stamp != nil {
		stamp(&r)
	}
	r = normalize(r)
	if r.ID == "" || r.Kind == "" || r.Outcome == "" {
		return Request{}, false, errors.New("answered: a record needs an id, a kind and an outcome")
	}
	l.mu.Lock()
	if existing, ok := l.byID[r.ID]; ok && strength(existing.By.Kind) >= strength(r.By.Kind) && !settlesTerminal(existing, r) {
		l.mu.Unlock()
		return existing, false, nil
	} else if ok && r.AskedAt == nil {
		r.AskedAt = existing.AskedAt
	}
	raw, err := json.Marshal(r)
	if err == nil {
		err = appendLine(l.path, raw)
	}
	if err != nil {
		l.mu.Unlock()
		return Request{}, false, err
	}
	l.byID[r.ID] = r
	l.lines++
	l.pruneLocked()
	if l.lines > 2*len(l.byID)+64 {
		_ = l.compactLocked()
	}
	l.version.Add(1)
	subs := append(([]func(Request))(nil), l.subs...)
	l.mu.Unlock()
	for _, fn := range subs {
		fn(r)
	}
	return r, true, nil
}

// Get returns the record of one request.
func (l *Ledger) Get(id string) (Request, bool) {
	if l == nil {
		return Request{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.byID[strings.TrimSpace(id)]
	return r, ok
}

// Filter selects records; empty fields match all.
type Filter struct {
	SessionID string
	ThreadID  string
	WorkIDs   map[string]bool
	Kind      string
	Limit     int
}

// List returns matching records, newest answer first.
func (l *Ledger) List(filter Filter) []Request {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	out := make([]Request, 0, len(l.byID))
	for _, r := range l.byID {
		if filter.SessionID != "" && r.SessionID != filter.SessionID {
			continue
		}
		if filter.ThreadID != "" && r.ThreadID != filter.ThreadID {
			continue
		}
		if filter.WorkIDs != nil && !filter.WorkIDs[r.WorkID] {
			continue
		}
		if filter.Kind != "" && r.Kind != filter.Kind {
			continue
		}
		out = append(out, r)
	}
	l.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].AnsweredAt.Equal(out[j].AnsweredAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].AnsweredAt.After(out[j].AnsweredAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

func (l *Ledger) pruneLocked() {
	bySession := map[string][]Request{}
	for _, r := range l.byID {
		if r.SessionID != "" {
			bySession[r.SessionID] = append(bySession[r.SessionID], r)
		}
	}
	for _, records := range bySession {
		if len(records) <= SessionLimit {
			continue
		}
		sort.Slice(records, func(i, j int) bool { return records[i].AnsweredAt.After(records[j].AnsweredAt) })
		for _, old := range records[SessionLimit:] {
			delete(l.byID, old.ID)
		}
	}
}

func (l *Ledger) compactLocked() error {
	records := make([]Request, 0, len(l.byID))
	for _, r := range l.byID {
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].AnsweredAt.Before(records[j].AnsweredAt) })
	var buf bytes.Buffer
	for _, r := range records {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	if err := atomicfile.Write(l.path, buf.Bytes(), 0o600); err != nil {
		return err
	}
	l.lines = len(records)
	return nil
}

func appendLine(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func normalize(r Request) Request {
	r.ID = strings.TrimSpace(r.ID)
	r.Kind = strings.TrimSpace(r.Kind)
	r.SessionID = strings.TrimSpace(r.SessionID)
	r.WorkID = strings.TrimSpace(r.WorkID)
	r.ThreadID = strings.TrimSpace(r.ThreadID)
	r.Title = Short(r.Title)
	r.Tool = Short(r.Tool)
	r.Summary = Short(r.Summary)
	r.Asked = Short(r.Asked)
	r.Answer = Short(r.Answer)
	r.By.Basis = Short(r.By.Basis)
	if r.AnsweredAt.IsZero() {
		r.AnsweredAt = time.Now()
	}
	r.AnsweredAt = r.AnsweredAt.UTC()
	if r.AskedAt != nil {
		at := r.AskedAt.UTC()
		r.AskedAt = &at
	}
	return r
}

// Short is the one-line form a sealed line shows: whitespace folded, cut at
// a bound with an ellipsis.
func Short(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= fieldLimit {
		return value
	}
	return string(runes[:fieldLimit-1]) + "…"
}
