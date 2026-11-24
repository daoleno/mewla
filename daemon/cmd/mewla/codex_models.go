package main

import (
	"context"
	"errors"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/server"
)

// Codex's own model menu for a Session Mewla launched: the models its
// app-server lists with the person's sign-in (`model/list`), the thread's
// model and effort, and a switch applied to the running thread
// (`thread/settings/update`, acknowledged by `thread/settings/updated`), so
// the conversation goes on on the new model.

// errCodexNoThread: the model is a setting of Codex's thread, which its first
// message starts.
var errCodexNoThread = errors.New("Codex starts this conversation with its first message; switch the model after it")

// Models reads the Session's menu from its own app-server.
func (c *codexEvents) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	s := c.session(worker.ID)
	if s == nil {
		return server.AgentModels{}, ErrNoCodexStatus
	}
	client, thread := s.connection()
	if client == nil {
		return server.AgentModels{}, ErrNoCodexStatus
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return server.AgentModels{}, err
	}
	// Before its first message the Session has no thread yet: it will start
	// on its configuration's model.
	status, err := client.ReadStatus(ctx, thread, s.cwd)
	if err != nil {
		return server.AgentModels{}, err
	}
	current, effort, provider := "", "", ""
	if status.Config != nil {
		provider = status.Config.ModelProvider
	}
	if status.Thread != nil {
		current, effort = status.Thread.Model, status.Thread.Effort
		if status.Thread.ModelProvider != "" {
			provider = status.Thread.ModelProvider
		}
	} else if chosen, ok := s.firstTurnSettings(thread); ok {
		// Set before the first message: the thread's first turn runs on it.
		current, effort = chosen.model, chosen.effort
	} else if status.Config != nil {
		current, effort = status.Config.Model, status.Config.Effort
	}
	return codexAgentModels(c.servedModels(models, provider), current, effort), nil
}

// servedModels keeps the models a Session's turns can reach. A Codex on
// Mewla's machine gateway lists Codex's own catalog, whatever the person's
// connections offer; the gateway refuses the rest (0.4.28 gate: the sheet
// offered gpt-6.2-astra, and the turn on it failed with a 404). A Codex on a
// Session route is given that route's catalog at launch and lists only it.
func (c *codexEvents) servedModels(models []codexctl.Model, provider string) []codexctl.Model {
	c.mu.Lock()
	serves := c.gatewayServes
	c.mu.Unlock()
	if serves == nil || !strings.EqualFold(strings.TrimSpace(provider), modelprofiles.GatewayProviderName) {
		return models
	}
	out := make([]codexctl.Model, 0, len(models))
	for _, model := range models {
		if serves(model.ID) {
			out = append(out, model)
		}
	}
	return out
}

// ModelsAvailable: the Session's Codex is connected and its thread is known
// (the TUI loads it as it starts; a Codex still on a startup question has
// none yet, and a Codex opened by hand has no app-server Mewla can ask).
func (c *codexEvents) ModelsAvailable(worker *classifier.Worker) bool {
	s := c.session(worker.ID)
	if s == nil {
		return false
	}
	client, thread := s.connection()
	return client != nil && thread != ""
}

// SetModel applies the model and effort to the running thread.
func (c *codexEvents) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (server.AgentModels, error) {
	s := c.session(worker.ID)
	if s == nil {
		return server.AgentModels{}, ErrNoCodexStatus
	}
	_, thread := s.connection()
	if thread == "" {
		return server.AgentModels{}, errCodexNoThread
	}
	// A connection of its own: the settings acknowledgement goes to the
	// connections attached to the thread, and the events connection keeps
	// its own reading of it.
	live, err := codexctl.Open(ctx, s.socket, codexctl.DialOptions{ClientName: "mewla-models"})
	if err != nil {
		return server.AgentModels{}, err
	}
	defer live.Close()
	var target *string
	if effort = strings.TrimSpace(effort); effort != "" {
		target = &effort
	}
	if _, err := live.ApplySettings(ctx, thread, model, target, codexctl.Settings{}, codexctl.DefaultAckTimeout); err != nil {
		if !codexctl.ThreadNotStarted(err) {
			return server.AgentModels{}, err
		}
		// Before the first message the thread has no rollout to attach to;
		// its settings are set directly and its first turn runs on them.
		if err := live.UpdateSettings(ctx, thread, model, target); err != nil {
			return server.AgentModels{}, err
		}
		s.setFirstTurnSettings(thread, model, effort)
	}
	return c.Models(ctx, worker)
}

// codexAgentModels is the menu of Codex's list with the thread's model
// current; an effort Codex reports as "none" is the model's default.
func codexAgentModels(models []codexctl.Model, current, effort string) server.AgentModels {
	if strings.EqualFold(effort, "none") {
		effort = ""
	}
	out := server.AgentModels{Label: "Codex", Current: strings.TrimSpace(current), Effort: effort}
	for _, model := range models {
		out.Models = append(out.Models, server.AgentModel{ID: model.ID, Name: model.DisplayName, Efforts: model.Efforts, DefaultEffort: model.DefaultEffort})
	}
	return out
}
