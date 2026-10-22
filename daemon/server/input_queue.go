package server

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// slowSessionInputLog is how long one send_input may hold its worker's input
// lane before the daemon says so; a lane that long is why a later message or
// a reconnected resend waits.
const slowSessionInputLog = 3 * time.Second

// sessionInputLanes runs each worker's send_input requests one at a time, in
// the order they were read. Input waits on the provider, so it must not run on
// the connection's read loop: that loop also answers subscribes, diagnostics
// and keepalive. A lane belongs to the worker, not the connection: a message
// resent on a reconnected socket queues behind the dead socket's still running
// handler, so the same request id is always handled one after the other and the
// second sees the first's receipt instead of racing it.
type sessionInputLanes struct {
	mu    sync.Mutex
	lanes map[string]*sessionInputLane
}

type sessionInputLane struct {
	pending []func()
}

// submit queues run on workerID's lane and returns at once.
func (l *sessionInputLanes) submit(workerID string, run func()) {
	l.mu.Lock()
	if l.lanes == nil {
		l.lanes = map[string]*sessionInputLane{}
	}
	lane := l.lanes[workerID]
	if lane != nil {
		lane.pending = append(lane.pending, run)
		l.mu.Unlock()
		return
	}
	lane = &sessionInputLane{pending: []func(){run}}
	l.lanes[workerID] = lane
	l.mu.Unlock()
	go l.drain(workerID, lane)
}

// drain is the lane's only goroutine; it ends, and the lane is forgotten, once
// nothing is waiting.
func (l *sessionInputLanes) drain(workerID string, lane *sessionInputLane) {
	for {
		l.mu.Lock()
		if len(lane.pending) == 0 {
			delete(l.lanes, workerID)
			l.mu.Unlock()
			return
		}
		run := lane.pending[0]
		lane.pending[0] = nil
		lane.pending = lane.pending[1:]
		l.mu.Unlock()
		run()
	}
}

// handleQueuedSessionInput orders a client's send_input behind earlier input to
// the same worker without holding up the connection's read loop.
func (s *Server) handleQueuedSessionInput(conn *websocket.Conn, raw clientMessage) {
	queuedAt := time.Now()
	s.inputLanes.submit(raw.WorkerID, func() {
		started := time.Now()
		s.handleSessionInputMessage(conn, raw)
		if took := time.Since(started); took >= slowSessionInputLog {
			log.Printf(
				"websocket: slow send_input worker=%s request=%s waited=%s took=%s",
				raw.WorkerID, raw.RequestID, started.Sub(queuedAt).Round(time.Millisecond), took.Round(time.Millisecond),
			)
		}
	})
}
