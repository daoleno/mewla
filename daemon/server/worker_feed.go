package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// workerFeedReconcileInterval is how often the daemon checks that what it
// derives for each Session (capabilities, executor, provider) still matches
// what clients were last told. Watcher events cover everything the watcher
// sees; this covers the fields owned by Model Profiles and the executor
// registry, which change without a watcher event.
const workerFeedReconcileInterval = time.Minute

// workerFeedHeartbeatInterval is how often every client is sent the feed's
// current seq. It is the connection's liveness signal (the app drops a socket
// that has been silent for 25 s) and it lets a client notice a change it never
// received when no later change follows.
const workerFeedHeartbeatInterval = 10 * time.Second

// workerFeed is the one ordered stream of Session changes a client follows.
//
// A client receives the full list once per connection (and again only when it
// asks, after noticing a gap). Every later change is a worker_session_created,
// worker_session_updated or worker_session_archived message with a seq one
// greater than the last. The list carries the seq it is current as of, so a
// client applies exactly the changes after it. Assigning seq, queueing the
// message to every client and reading a list all happen under mu, which is what
// makes the order a client sees equal the order of seq.
type workerFeed struct {
	mu  sync.Mutex
	seq uint64
	// sent is the last wire form published for each visible Session.
	sent map[string]sentWorkerSession
}

type sentWorkerSession struct {
	wire *workerSessionWire
	body []byte
}

func (f *workerFeed) publish(kind string, wire *workerSessionWire, emit func([]byte)) {
	if wire == nil || wire.Worker == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishLocked(kind, wire, emit)
}

// publishChange publishes a change to a visible Session: an update for one
// clients hold, a creation for one they do not (it was hidden until now).
func (f *workerFeed) publishChange(wire *workerSessionWire, emit func([]byte)) {
	if wire == nil || wire.Worker == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	kind := "worker_session_updated"
	if _, known := f.sent[wire.ID]; !known {
		kind = "worker_session_created"
	}
	f.publishLocked(kind, wire, emit)
}

// retire tells clients a Session they were sent is gone from the list, because
// it became hidden. It is a no-op for a Session clients never had, so every
// event of a hidden Session can call it.
func (f *workerFeed) retire(id string, emit func([]byte)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retireLocked(id, emit)
}

func (f *workerFeed) retireLocked(id string, emit func([]byte)) {
	last, known := f.sent[id]
	if !known {
		return
	}
	worker := *last.wire.Worker
	worker.State = classifier.StateRemoved
	archived := *last.wire
	archived.Worker = &worker
	f.publishLocked("worker_session_archived", &archived, emit)
}

func (f *workerFeed) publishLocked(kind string, wire *workerSessionWire, emit func([]byte)) {
	body, err := json.Marshal(wire)
	if err != nil {
		log.Printf("worker feed: %s for %s not published: %v", kind, wire.ID, err)
		return
	}
	data, err := json.Marshal(map[string]any{
		"type":           kind,
		"seq":            f.seq + 1,
		"worker_session": json.RawMessage(body),
	})
	if err != nil {
		log.Printf("worker feed: %s for %s not published: %v", kind, wire.ID, err)
		return
	}
	// seq moves only for a message that exists, so a client never sees a hole
	// the daemon itself made.
	f.seq++
	if f.sent == nil {
		f.sent = map[string]sentWorkerSession{}
	}
	if kind == "worker_session_archived" {
		delete(f.sent, wire.ID)
	} else {
		f.sent[wire.ID] = sentWorkerSession{wire: wire, body: body}
	}
	emit(data)
}

// heartbeat sends the seq the feed is at. It carries no Session, so it costs a
// few bytes however many Sessions exist.
func (f *workerFeed) heartbeat(emit func([]byte)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(map[string]any{"type": "worker_session_heartbeat", "seq": f.seq})
	if err != nil {
		return
	}
	emit(data)
}

// snapshot sends the full list, current as of the seq it carries. The list is
// built under the lock so no change can fall between it and its seq.
func (f *workerFeed) snapshot(build func() []*workerSessionWire, send func([]byte)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sessions := build()
	if sessions == nil {
		sessions = []*workerSessionWire{}
	}
	data, err := json.Marshal(map[string]any{
		"type":            "worker_session_list",
		"seq":             f.seq,
		"worker_sessions": sessions,
	})
	if err != nil {
		log.Printf("worker feed: list not sent: %v", err)
		return
	}
	send(data)
}

// reconcile publishes, as ordinary sequenced changes, whatever differs between
// the current Sessions and what clients were last told. It never sends a list.
func (f *workerFeed) reconcile(build func() []*workerSessionWire, emit func([]byte)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current := build()
	present := make(map[string]struct{}, len(current))
	for _, wire := range current {
		if wire == nil || wire.Worker == nil {
			continue
		}
		present[wire.ID] = struct{}{}
		body, err := json.Marshal(wire)
		if err != nil {
			log.Printf("worker feed: reconcile skipped %s: %v", wire.ID, err)
			continue
		}
		previous, known := f.sent[wire.ID]
		switch {
		case !known:
			f.publishLocked("worker_session_created", wire, emit)
		case !bytes.Equal(previous.body, body):
			f.publishLocked("worker_session_updated", wire, emit)
		}
	}
	var gone []string
	for id := range f.sent {
		if _, ok := present[id]; !ok {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		f.retireLocked(id, emit)
	}
}

func (s *Server) publishWorkerSession(kind string, worker *classifier.Worker) {
	s.workerFeed.publish(kind, s.workerSessionWire(worker), s.broadcast)
}

// publishWorkerSessionChange is publishWorkerSession for a change that may be a
// Session becoming visible.
func (s *Server) publishWorkerSessionChange(worker *classifier.Worker) {
	s.workerFeed.publishChange(s.workerSessionWire(worker), s.broadcast)
}

func (s *Server) visibleWorkerSessionWires() []*workerSessionWire {
	return s.workerSessionsWire(s.currentVisibleWorkerSessions())
}

func (s *Server) reconcileWorkerFeed() {
	s.workerFeed.reconcile(s.visibleWorkerSessionWires, s.broadcast)
}

// runWorkerFeedReconcile keeps the derived fields of the feed honest and tells
// clients where the feed is.
func (s *Server) runWorkerFeedReconcile(ctx context.Context) {
	reconcile := time.NewTicker(workerFeedReconcileInterval)
	defer reconcile.Stop()
	heartbeat := time.NewTicker(workerFeedHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcile.C:
			s.reconcileWorkerFeed()
		case <-heartbeat.C:
			s.workerFeed.heartbeat(s.broadcast)
		}
	}
}
