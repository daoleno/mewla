package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/work"
)

// typedModels is the model menu of an agent whose terminal UI is all Mewla
// has (Cursor Agent, Grok): the list its own `models` command prints with the
// person's sign-in, and a switch typed as its documented `/model <id>`, which
// takes effect in the conversation from the next turn.
type typedModels struct {
	provider string
	screen   claudeScreen

	mu      sync.Mutex
	applied map[string]string
}

func newTypedModels(provider string, screen claudeScreen) *typedModels {
	return &typedModels{provider: provider, screen: screen, applied: map[string]string{}}
}

func (t *typedModels) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	models, err := work.ReadCLIModels(ctx, t.provider, worker.Command, worker.Cwd)
	if err != nil {
		return server.AgentModels{}, err
	}
	t.mu.Lock()
	current := t.applied[worker.ID]
	t.mu.Unlock()
	if current == "" {
		current = work.LaunchModelFlag(worker.Command)
	}
	out := server.AgentModels{Label: work.AgentProductName(t.provider, worker.Command)}
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
	if err := t.screen.SendInput(worker.ID, "/model "+model); err != nil {
		return server.AgentModels{}, err
	}
	if err := t.screen.SendKey(worker.ID, "Enter"); err != nil {
		return server.AgentModels{}, err
	}
	t.mu.Lock()
	t.applied[worker.ID] = model
	t.mu.Unlock()
	return t.Models(ctx, worker)
}
