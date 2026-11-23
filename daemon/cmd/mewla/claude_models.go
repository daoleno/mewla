package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/work"
)

// Claude Code's own model menu for a Session: the models of its picker with
// the person's sign-in (read from Claude itself), the model the Session runs
// on, and a switch made in Claude's own picker with its "use this session
// only" key, so the conversation goes on and the person's default for new
// sessions stays as it is. (A typed `/model <name>` would save it as the
// default.)

type claudeScreen interface {
	CapturePaneContent(sessionID string) (string, error)
	SendKey(sessionID, key string) error
	SendInput(sessionID, text string) error
}

type claudeModels struct {
	screen claudeScreen
	hub    *agentevents.Hub
	// read lists a launch command's models (tests replace it).
	read func(ctx context.Context, command, cwd string) ([]work.ClaudeModel, error)

	mu      sync.Mutex
	lists   map[string]claudeModelList
	applied map[string]claudeChoice
}

type claudeModelList struct {
	models []work.ClaudeModel
	at     time.Time
}

// claudeChoice is a switch Mewla made in a Session, and when.
type claudeChoice struct {
	value, effort string
	at            time.Time
}

// claudeListFresh is how long a Claude's model list is reused.
const claudeListFresh = 10 * time.Minute

func newClaudeModels(screen claudeScreen, hub *agentevents.Hub) *claudeModels {
	return &claudeModels{screen: screen, hub: hub, lists: map[string]claudeModelList{}, applied: map[string]claudeChoice{},
		read: func(ctx context.Context, command, cwd string) ([]work.ClaudeModel, error) {
			return work.ReadClaudeModels(ctx, command, cwd, os.Environ())
		}}
}

func (c *claudeModels) list(ctx context.Context, worker *classifier.Worker) ([]work.ClaudeModel, error) {
	key := strings.Join(strings.Fields(worker.Command), " ")
	c.mu.Lock()
	cached, ok := c.lists[key]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < claudeListFresh {
		return cached.models, nil
	}
	models, err := c.read(ctx, worker.Command, worker.Cwd)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.lists[key] = claudeModelList{models: models, at: time.Now()}
	c.mu.Unlock()
	return models, nil
}

// Models reads the Session's menu.
func (c *claudeModels) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	models, err := c.list(ctx, worker)
	if err != nil {
		return server.AgentModels{}, err
	}
	path := ""
	if c.hub != nil {
		if state, ok := c.hub.State(worker.ID); ok {
			path = state.Binding.Ref
		}
	}
	if path == "" {
		path = work.ClaudeTranscriptFor(*worker)
	}
	transcript := work.ReadClaudeTranscriptState(path)
	c.mu.Lock()
	choice, chosen := c.applied[worker.ID]
	c.mu.Unlock()
	launchModel, launchEffort := work.ClaudeLaunchModel(worker.Command)
	current, effort := claudeCurrent(models, transcript, launchModel), launchEffort
	if transcript.Effort != "" {
		effort = transcript.Effort
	}
	// Mewla's own switch, unless Claude's transcript says something newer
	// (an answer, or a switch the person made in the terminal).
	if chosen && !transcript.At.After(choice.at) {
		current, effort = choice.value, choice.effort
	}
	return claudeAgentModels(models, current, effort), nil
}

// claudeCurrent is the picker value the Session runs on: the one its latest
// answer came from (the launch's own value when it resolves there), else the
// launch's --model, else Claude's default.
func claudeCurrent(models []work.ClaudeModel, transcript work.ClaudeTranscriptChoice, launch string) string {
	if transcript.Named != "" {
		for _, model := range models {
			if model.Value == launch && model.Name == transcript.Named {
				return launch
			}
		}
		for _, model := range models {
			if model.Name == transcript.Named {
				return model.Value
			}
		}
		return transcript.Named
	}
	if resolved := transcript.Resolved; resolved != "" {
		for _, model := range models {
			if model.Value == launch && model.Resolved == resolved {
				return launch
			}
		}
		for _, model := range models {
			if model.Value != "default" && model.Resolved == resolved {
				return model.Value
			}
		}
		for _, model := range models {
			if model.Resolved == resolved {
				return model.Value
			}
		}
		return resolved
	}
	if launch != "" {
		return launch
	}
	return "default"
}

func claudeAgentModels(models []work.ClaudeModel, current, effort string) server.AgentModels {
	out := server.AgentModels{Label: "Claude Code", Current: current, Effort: effort}
	for _, model := range models {
		out.Models = append(out.Models, server.AgentModel{ID: model.Value, Name: model.DisplayName, Efforts: model.Efforts})
	}
	return out
}

var errClaudeBusy = errors.New("Claude is working on a turn; switch the model when it ends")

// SetModel switches the Session in Claude's own picker, for this session only.
func (c *claudeModels) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (server.AgentModels, error) {
	models, err := c.list(ctx, worker)
	if err != nil {
		return server.AgentModels{}, err
	}
	var target *work.ClaudeModel
	for i := range models {
		if models[i].Value == model {
			target = &models[i]
		}
	}
	if target == nil {
		return server.AgentModels{}, fmt.Errorf("Claude Code has no model %q", model)
	}
	if effort != "" && !containsString(target.Efforts, effort) {
		return server.AgentModels{}, fmt.Errorf("%s takes no %s effort", target.DisplayName, effort)
	}
	if c.hub != nil {
		if state, ok := c.hub.State(worker.ID); ok && state.Current != nil && !state.Current.Status.Terminal() {
			return server.AgentModels{}, errClaudeBusy
		}
	}
	if err := c.pick(ctx, worker.ID, *target, effort); err != nil {
		return server.AgentModels{}, err
	}
	c.mu.Lock()
	c.applied[worker.ID] = claudeChoice{value: target.Value, effort: effort, at: time.Now()}
	c.mu.Unlock()
	return c.Models(ctx, worker)
}

// pick opens Claude's picker, moves to the model and effort, reading the
// screen after each key, and takes it for this session only.
func (c *claudeModels) pick(ctx context.Context, sessionID string, target work.ClaudeModel, effort string) (err error) {
	before, err := c.screen.CapturePaneContent(sessionID)
	if err != nil {
		return err
	}
	switches := strings.Count(flatScreen(before), claudeSwitchPrefix)
	if err := c.screen.SendInput(sessionID, "/model"); err != nil {
		return err
	}
	if err := c.screen.SendKey(sessionID, "Enter"); err != nil {
		return err
	}
	open := true
	defer func() {
		if err != nil && open {
			_ = c.screen.SendKey(sessionID, "Escape")
		}
	}()
	picker, err := c.waitPicker(ctx, sessionID, func(work.ClaudePicker) bool { return true })
	if err != nil {
		return err
	}
	index := -1
	for i, option := range picker.Options {
		if option == target.DisplayName {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("Claude's picker does not offer %s", target.DisplayName)
	}
	for picker.Selected != index {
		key := "Down"
		if index < picker.Selected {
			key = "Up"
		}
		if err := c.screen.SendKey(sessionID, key); err != nil {
			return err
		}
		before := picker.Selected
		if picker, err = c.waitPicker(ctx, sessionID, func(p work.ClaudePicker) bool { return p.Selected != before }); err != nil {
			return err
		}
	}
	if effort != "" {
		for steps := 0; picker.Effort != effort; steps++ {
			from, to := indexOf(target.Efforts, picker.Effort), indexOf(target.Efforts, effort)
			if from < 0 || steps > len(target.Efforts) {
				return fmt.Errorf("Claude's picker shows %q effort, not %s", picker.Effort, effort)
			}
			key := "Right"
			if to < from {
				key = "Left"
			}
			if err := c.screen.SendKey(sessionID, key); err != nil {
				return err
			}
			before := picker.Effort
			if picker, err = c.waitPicker(ctx, sessionID, func(p work.ClaudePicker) bool { return p.Effort != before }); err != nil {
				return err
			}
		}
	}
	if !picker.SessionOnly {
		return errors.New("Claude's picker offers no session-only choice here")
	}
	if err := c.screen.SendKey(sessionID, "s"); err != nil {
		return err
	}
	open = false
	return c.waitApplied(ctx, sessionID, switches)
}

const claudeScreenWait = 5 * time.Second

func (c *claudeModels) waitPicker(ctx context.Context, sessionID string, ready func(work.ClaudePicker) bool) (work.ClaudePicker, error) {
	deadline := time.Now().Add(claudeScreenWait)
	last := ""
	for {
		screen, err := c.screen.CapturePaneContent(sessionID)
		if err != nil {
			return work.ClaudePicker{}, err
		}
		last = screen
		if picker, ok := work.ParseClaudePicker(screen); ok && ready(picker) {
			return picker, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return work.ClaudePicker{}, fmt.Errorf("Claude's model picker did not show as expected: %s", lastLine(last))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

const claudeSwitchPrefix = "Set model to "

// errClaudeAsks: Claude asks the person to confirm the switch (it costs a
// re-read of the conversation); the question stays theirs to answer.
type errClaudeAsks struct{ question string }

func (e errClaudeAsks) Error() string { return e.question }

// waitApplied waits for Claude's own new line for the switch ("Set model to …
// for this session only"), or Claude's question about it, or reports
// Claude's last line. Lines are read flattened, as the terminal wraps them.
func (c *claudeModels) waitApplied(ctx context.Context, sessionID string, switches int) error {
	deadline := time.Now().Add(claudeScreenWait)
	for {
		screen, err := c.screen.CapturePaneContent(sessionID)
		if err != nil {
			return err
		}
		flat := flatScreen(screen)
		if _, open := work.ParseClaudePicker(screen); !open {
			if strings.Count(flat, claudeSwitchPrefix) > switches {
				line := flat[strings.LastIndex(flat, claudeSwitchPrefix):]
				if end := strings.Index(line, " effort"); end >= 0 {
					line = line[:end+len(" effort")]
				}
				if strings.Contains(line, "for this session only") {
					return nil
				}
				return errors.New(line)
			}
			if at := strings.LastIndex(flat, "Switch model?"); at >= 0 && strings.Contains(flat[at:], "Yes, switch to") {
				question := flat[at:]
				if end := strings.Index(question, " This conversation"); end >= 0 {
					question = question[:end]
				}
				return errClaudeAsks{question: "Claude asks in the Session: " + question}
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("Claude did not confirm the switch: %s", lastLine(screen))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func flatScreen(screen string) string { return strings.Join(strings.Fields(screen), " ") }

func lastLine(screen string) string {
	lines := strings.Split(strings.TrimSpace(screen), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" && !strings.HasPrefix(line, "─") && !strings.HasPrefix(line, "⏵") {
			return line
		}
	}
	return ""
}

func indexOf(values []string, value string) int {
	for i, v := range values {
		if strings.EqualFold(v, value) {
			return i
		}
	}
	return -1
}

func containsString(values []string, value string) bool { return indexOf(values, value) >= 0 }
