package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/server"
)

// turnPending holds a model choice for an agent that takes it only between
// turns (Claude's picker, a typed /model): a choice made while the agent
// works, or waits for an answer inside its turn, is kept and applied the
// moment the turn ends. The person's tap is never refused for it: the Model
// sheet shows the choice as pending until then.
type turnPending struct {
	hub *agentevents.Hub
	// poll is how often a held choice looks for the end of the turn.
	poll time.Duration
	// wait bounds how long a choice is held for one turn.
	wait time.Duration

	mu      sync.Mutex
	pending map[string]heldChoice
}

type heldChoice struct {
	choice server.AgentChoice
	gen    uint64
}

func newTurnPending(hub *agentevents.Hub) *turnPending {
	return &turnPending{hub: hub, poll: 400 * time.Millisecond, wait: 12 * time.Hour, pending: map[string]heldChoice{}}
}

// busy reports a turn the agent is in: working, or waiting for an answer.
func (t *turnPending) busy(workerID string) bool {
	if t == nil || t.hub == nil {
		return false
	}
	state, ok := t.hub.State(workerID)
	return ok && state.Current != nil && !state.Current.Status.Terminal()
}

// of is the choice held for a Session, if any.
func (t *turnPending) of(workerID string) *server.AgentChoice {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	held, ok := t.pending[workerID]
	if !ok {
		return nil
	}
	choice := held.choice
	return &choice
}

// drop forgets a held choice (a newer one was applied at once).
func (t *turnPending) drop(workerID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.pending, workerID)
	t.mu.Unlock()
}

var turnPendingGen struct {
	sync.Mutex
	n uint64
}

// hold keeps the choice and applies it when the turn ends; a later choice
// replaces it.
func (t *turnPending) hold(workerID string, choice server.AgentChoice, apply func(ctx context.Context) error) {
	turnPendingGen.Lock()
	turnPendingGen.n++
	gen := turnPendingGen.n
	turnPendingGen.Unlock()
	t.mu.Lock()
	t.pending[workerID] = heldChoice{choice: choice, gen: gen}
	t.mu.Unlock()
	go func() {
		deadline := time.Now().Add(t.wait)
		for time.Now().Before(deadline) {
			if !t.current(workerID, gen) {
				return
			}
			if !t.busy(workerID) {
				break
			}
			time.Sleep(t.poll)
		}
		if !t.current(workerID, gen) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := apply(ctx)
		cancel()
		t.mu.Lock()
		if held, ok := t.pending[workerID]; ok && held.gen == gen {
			delete(t.pending, workerID)
		}
		t.mu.Unlock()
		if err != nil {
			log.Printf("model switch for %s after its turn: %v", workerID, err)
		}
	}()
}

func (t *turnPending) current(workerID string, gen uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	held, ok := t.pending[workerID]
	return ok && held.gen == gen
}
