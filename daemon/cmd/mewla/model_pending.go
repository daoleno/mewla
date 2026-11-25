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
	// failed is a held choice the agent did not take when its turn ended,
	// with the agent's reason: the Model sheet says so until the next pick.
	failed map[string]server.AgentFailure
}

type heldChoice struct {
	choice   server.AgentChoice
	gen      uint64
	apply    func(ctx context.Context) error
	applying bool
}

func newTurnPending(hub *agentevents.Hub) *turnPending {
	return &turnPending{hub: hub, poll: 400 * time.Millisecond, wait: 12 * time.Hour, pending: map[string]heldChoice{}, failed: map[string]server.AgentFailure{}}
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

// failure is the held choice the agent did not take, if any.
func (t *turnPending) failure(workerID string) *server.AgentFailure {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	failed, ok := t.failed[workerID]
	if !ok {
		return nil
	}
	return &failed
}

// drop forgets a held choice and an earlier failure (a newer choice was
// applied at once).
func (t *turnPending) drop(workerID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.pending, workerID)
	delete(t.failed, workerID)
	t.mu.Unlock()
}

// done ends a held choice that was being made: it is no longer pending, and
// a failure is kept for the Model sheet.
func (t *turnPending) done(workerID string, held heldChoice, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now, ok := t.pending[workerID]; !ok || now.gen != held.gen {
		return
	}
	delete(t.pending, workerID)
	if err != nil {
		t.failed[workerID] = server.AgentFailure{Model: held.choice.Model, Effort: held.choice.Effort, Reason: err.Error()}
	}
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
	held := heldChoice{choice: choice, gen: gen, apply: apply}
	t.mu.Lock()
	t.pending[workerID] = held
	delete(t.failed, workerID)
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
		if !t.claim(workerID, gen) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := apply(ctx)
		cancel()
		t.done(workerID, held, err)
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

// claim marks the held choice as being made; false when it is gone or
// already being made.
func (t *turnPending) claim(workerID string, gen uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	held, ok := t.pending[workerID]
	if !ok || held.gen != gen || held.applying {
		return false
	}
	held.applying = true
	t.pending[workerID] = held
	return true
}

// settle makes a held choice now, when the agent's turn is over: a message
// about to be sent then runs on it. A choice already being made is waited
// for. While a turn runs the choice stays held.
func (t *turnPending) settle(ctx context.Context, workerID string) error {
	if t == nil {
		return nil
	}
	for {
		t.mu.Lock()
		held, ok := t.pending[workerID]
		if !ok {
			t.mu.Unlock()
			return nil
		}
		if !held.applying {
			if t.busy(workerID) {
				t.mu.Unlock()
				return nil
			}
			held.applying = true
			t.pending[workerID] = held
			t.mu.Unlock()
			err := held.apply(ctx)
			t.done(workerID, held, err)
			return err
		}
		t.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
