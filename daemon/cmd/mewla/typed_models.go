package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/work"
)

// typedModels is the model menu of an agent whose terminal UI is all Mewla
// has (Cursor Agent, Grok): the list its own `models` command prints with the
// person's sign-in, and a switch typed as its documented `/model <id>`, which
// takes effect in the conversation from the next turn. Typed while the agent
// works, /model would be queued as a message, so such a choice is held until
// the turn ends.
type typedModels struct {
	provider string
	screen   claudeScreen
	later    *turnPending
	// read lists a launch command's models (tests replace it).
	read func(ctx context.Context, provider, command, cwd string) ([]work.CLIModel, error)

	mu      sync.Mutex
	applied map[string]string
	// lists is the latest read per program and folder: whether the agent
	// can list its models (it is signed in) decides whether its Sessions
	// offer a model choice.
	lists map[string]typedList
}

type typedList struct {
	models  []work.CLIModel
	err     error
	at      time.Time
	reading bool
}

// typedListFresh is how long a read of the agent's models is reused.
const typedListFresh = 5 * time.Minute

func newTypedModels(provider string, screen claudeScreen, hub *agentevents.Hub) *typedModels {
	return &typedModels{provider: provider, screen: screen, later: newTurnPending(hub), read: work.ReadCLIModels,
		applied: map[string]string{}, lists: map[string]typedList{}}
}

func typedListKey(worker *classifier.Worker) string {
	return strings.Join(strings.Fields(worker.Command), " ") + "\x00" + worker.Cwd
}

func (t *typedModels) list(ctx context.Context, worker *classifier.Worker) ([]work.CLIModel, error) {
	key := typedListKey(worker)
	t.mu.Lock()
	cached, ok := t.lists[key]
	t.mu.Unlock()
	if ok && !cached.at.IsZero() && time.Since(cached.at) < typedListFresh {
		return cached.models, cached.err
	}
	models, err := t.read(ctx, t.provider, worker.Command, worker.Cwd)
	t.mu.Lock()
	t.lists[key] = typedList{models: models, err: err, at: time.Now()}
	t.mu.Unlock()
	return models, err
}

// ModelsAvailable: the agent listed its models for this Session's command
// (it is signed in). The first ask reads in the background and answers no
// until the read is in.
func (t *typedModels) ModelsAvailable(worker *classifier.Worker) bool {
	key := typedListKey(worker)
	t.mu.Lock()
	cached, ok := t.lists[key]
	stale := !ok || (!cached.at.IsZero() && time.Since(cached.at) >= typedListFresh)
	if stale && !cached.reading {
		cached.reading = true
		t.lists[key] = cached
		copied := *worker
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = t.list(ctx, &copied)
		}()
	}
	t.mu.Unlock()
	return ok && !cached.at.IsZero() && cached.err == nil && len(cached.models) > 0
}

func (t *typedModels) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	models, err := t.list(ctx, worker)
	if err != nil {
		return server.AgentModels{}, err
	}
	t.mu.Lock()
	current := t.applied[worker.ID]
	t.mu.Unlock()
	if current == "" {
		current = work.LaunchModelFlag(worker.Command)
	}
	out := server.AgentModels{Label: work.AgentProductName(t.provider, worker.Command), Pending: t.later.of(worker.ID)}
	for _, model := range models {
		if current == "" && (model.Current || model.Default) {
			current = model.ID
		}
		out.Models = append(out.Models, server.AgentModel{ID: model.ID, Name: model.Name})
	}
	if current == "" && len(models) > 0 {
		current = models[0].ID
	}
	out.Current = current
	return out, nil
}

func (t *typedModels) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (server.AgentModels, error) {
	if effort != "" {
		return server.AgentModels{}, fmt.Errorf("%s takes its reasoning level in the model id", work.AgentProductName(t.provider, worker.Command))
	}
	id := worker.ID
	apply := func(context.Context) error {
		if err := t.screen.SendInput(id, "/model "+model); err != nil {
			return err
		}
		if err := t.screen.SendKey(id, "Enter"); err != nil {
			return err
		}
		t.mu.Lock()
		t.applied[id] = model
		t.mu.Unlock()
		return nil
	}
	if t.later.busy(id) {
		t.later.hold(id, server.AgentChoice{Model: model}, apply)
		return t.Models(ctx, worker)
	}
	t.later.drop(id)
	if err := apply(ctx); err != nil {
		return server.AgentModels{}, err
	}
	return t.Models(ctx, worker)
}

// Settle makes a pick held during the turn that just ended before the next
// message is sent, so that message runs on it.
func (t *typedModels) Settle(ctx context.Context, worker *classifier.Worker) error {
	if worker == nil {
		return nil
	}
	return t.later.settle(ctx, worker.ID)
}
