package agentevents

import (
	"sort"
	"sync"
	"time"
)

// maxTerminalTurns bounds the settled turns a Worker's state remembers.
const maxTerminalTurns = 16

// Turn is the state of one agent turn.
type Turn struct {
	ID         string
	Status     TurnStatus
	StartedAt  time.Time
	SettledAt  time.Time
	ProgressAt time.Time
	Error      *TurnError
}

// Admission is the last user message the agent accepted.
type Admission struct {
	ID                   string
	At                   time.Time
	InputSHA256          string
	InputUnwrappedSHA256 string
	ClientID             string
	// Cursor increases with every admission, so a stale snapshot can be told
	// from a newer one.
	Cursor uint64
}

// State is everything the Hub knows about one Worker.
type State struct {
	Binding Binding
	Bound   bool
	// Connected is true while the adapter's event source is live. A Worker
	// whose source is down is not described by events; callers fall back.
	Connected bool
	// Current is the latest turn, running or settled.
	Current   *Turn
	Terminals []Turn
	Admission *Admission
	Approvals []Approval
	Exited    bool
	ExitCode  *int
	LastEvent time.Time
}

// Hub holds each Worker's state and fans events out to subscribers. It is the
// only place adapters publish to and core code reads from.
type Hub struct {
	mu      sync.Mutex
	workers map[string]*workerState
	nextSub int
}

type workerState struct {
	State
	subs map[int]chan Event
}

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{workers: map[string]*workerState{}} }

func (h *Hub) worker(id string) *workerState {
	w := h.workers[id]
	if w == nil {
		w = &workerState{subs: map[int]chan Event{}}
		h.workers[id] = w
	}
	return w
}

// SetConnected records whether the adapter's event source for the Worker is
// live.
func (h *Hub) SetConnected(workerID string, connected bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.worker(workerID).Connected = connected
}

// Publish applies an event to its Worker's state and delivers it to the
// Worker's subscribers. A subscriber that cannot keep up misses events rather
// than blocking the adapter; it recovers from State.
func (h *Hub) Publish(e Event) {
	if e.WorkerID == "" {
		return
	}
	h.mu.Lock()
	w := h.worker(e.WorkerID)
	w.apply(e)
	subs := make([]chan Event, 0, len(w.subs))
	for _, ch := range w.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (w *workerState) apply(e Event) {
	if e.At.After(w.LastEvent) {
		w.LastEvent = e.At
	}
	switch e.Kind {
	case KindBound:
		if e.Binding != nil {
			w.Binding, w.Bound = *e.Binding, true
		}
	case KindTurnStarted:
		w.Current = &Turn{ID: e.TurnID, Status: TurnRunning, StartedAt: e.At, ProgressAt: e.At}
	case KindProgress:
		if w.Current != nil && !w.Current.Status.Terminal() && e.At.After(w.Current.ProgressAt) {
			w.Current.ProgressAt = e.At
		}
	case KindTurnFinished:
		turn := Turn{ID: e.TurnID, Status: e.Status, SettledAt: e.At, Error: e.Error, ProgressAt: e.At}
		if w.Current != nil && (w.Current.ID == e.TurnID || e.TurnID == "") {
			turn.ID, turn.StartedAt = w.Current.ID, w.Current.StartedAt
		}
		if turn.Status == "" {
			turn.Status = TurnCompleted
		}
		// A failure the agent is still retrying is progress, not an end.
		if turn.Status == TurnFailed && turn.Error != nil && turn.Error.WillRetry {
			if w.Current != nil {
				w.Current.Error = turn.Error
				w.Current.ProgressAt = e.At
			}
			return
		}
		w.Current = &turn
		w.Terminals = append(w.Terminals, turn)
		if len(w.Terminals) > maxTerminalTurns {
			w.Terminals = w.Terminals[len(w.Terminals)-maxTerminalTurns:]
		}
	case KindUserMessage:
		var cursor uint64 = 1
		if w.Admission != nil {
			cursor = w.Admission.Cursor + 1
		}
		w.Admission = &Admission{
			ID: e.ID, At: e.At, InputSHA256: e.InputSHA256,
			InputUnwrappedSHA256: e.InputUnwrappedSHA256, ClientID: e.ClientID, Cursor: cursor,
		}
	case KindApprovalRequest:
		if e.Approval != nil {
			w.Approvals = append(w.Approvals[:0:0], w.Approvals...)
			w.Approvals = append(w.Approvals, *e.Approval)
		}
	case KindApprovalResolved:
		kept := w.Approvals[:0:0]
		for _, a := range w.Approvals {
			if a.ID != e.ApprovalID {
				kept = append(kept, a)
			}
		}
		w.Approvals = kept
	case KindExit:
		w.Exited, w.ExitCode = true, e.ExitCode
		w.Connected = false
	}
}

// State returns a copy of the Worker's state; ok is false when the Hub has
// never heard of it.
func (h *Hub) State(workerID string) (State, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.workers[workerID]
	if !ok {
		return State{}, false
	}
	return w.snapshot(), true
}

func (w *workerState) snapshot() State {
	s := w.State
	if w.Current != nil {
		c := *w.Current
		s.Current = &c
	}
	s.Terminals = append([]Turn(nil), w.Terminals...)
	if w.Admission != nil {
		a := *w.Admission
		s.Admission = &a
	}
	s.Approvals = append([]Approval(nil), w.Approvals...)
	return s
}

// Subscribe delivers the Worker's future events until cancel is called.
func (h *Hub) Subscribe(workerID string) (events <-chan Event, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.worker(workerID)
	h.nextSub++
	id := h.nextSub
	ch := make(chan Event, 64)
	w.subs[id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := w.subs[id]; ok {
			delete(w.subs, id)
		}
	}
}

// Forget drops everything known about a Worker.
func (h *Hub) Forget(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.workers, workerID)
}

// Workers lists the ids the Hub holds state for.
func (h *Hub) Workers() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.workers))
	for id := range h.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
