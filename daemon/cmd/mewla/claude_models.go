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
	// launchArgs is the command line the Session's Claude was started with
	// (its process's own), which keeps the --model and --effort the
	// Worker's recorded command drops.
	launchArgs func(worker *classifier.Worker) string
	// later holds a choice made while Claude is in a turn.
	later *turnPending
	// transcriptOf is the Session's Claude transcript ("" while it has none):
	// what Claude answered to a switch is read there.
	transcriptOf func(worker *classifier.Worker) string

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
		},
		launchArgs:   workerProcessArgs,
		later:        newTurnPending(hub),
		transcriptOf: func(worker *classifier.Worker) string { return claudeTranscriptOf(hub, worker) }}
}

// claudeTranscriptOf is the transcript the Session's Chat reads: the hub's
// binding, else the one Claude's launch names.
func claudeTranscriptOf(hub *agentevents.Hub, worker *classifier.Worker) string {
	if hub != nil {
		if state, ok := hub.State(worker.ID); ok && state.Binding.Ref != "" {
			return state.Binding.Ref
		}
	}
	return work.ClaudeTranscriptFor(*worker)
}

// claudeCommand is the command line of the Claude the Session runs: its
// recorded command when that starts Claude, else the program's own (a
// Session starts as `zsh -i -l -c '<command>'`, and its recorded command
// stays the shell's a while after the shell became Claude). "" while no Claude
// runs yet.
func (c *claudeModels) claudeCommand(worker *classifier.Worker) string {
	if work.LaunchProgramProvider(worker.Command) == work.WorkerProviderClaude {
		return worker.Command
	}
	if c.launchArgs != nil {
		if args := c.launchArgs(worker); work.LaunchProgramProvider(args) == work.WorkerProviderClaude {
			return args
		}
	}
	return ""
}

var errClaudeNotRunning = errors.New("Claude has not started in this Session yet")

func (c *claudeModels) list(ctx context.Context, worker *classifier.Worker) ([]work.ClaudeModel, error) {
	command := c.claudeCommand(worker)
	if command == "" {
		return nil, errClaudeNotRunning
	}
	key := strings.Join(strings.Fields(command), " ")
	c.mu.Lock()
	cached, ok := c.lists[key]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < claudeListFresh {
		return cached.models, nil
	}
	models, err := c.read(ctx, command, worker.Cwd)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.lists[key] = claudeModelList{models: models, at: time.Now()}
	c.mu.Unlock()
	return models, nil
}

// ModelsAvailable: Claude runs in the Session. A Session Mewla just made
// runs its shell before Claude starts, and Claude's list is read with
// Claude's own command line.
func (c *claudeModels) ModelsAvailable(worker *classifier.Worker) bool {
	return c.claudeCommand(worker) != ""
}

// Models reads the Session's menu.
func (c *claudeModels) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	models, err := c.list(ctx, worker)
	if err != nil {
		return server.AgentModels{}, err
	}
	transcript := work.ReadClaudeTranscriptState(c.transcriptOf(worker))
	c.mu.Lock()
	choice, chosen := c.applied[worker.ID]
	c.mu.Unlock()
	launchModel, launchEffort := work.ClaudeLaunchModel(worker.Command)
	if launchModel == "" && c.launchArgs != nil {
		launchModel, launchEffort = work.ClaudeLaunchModel(c.launchArgs(worker))
	}
	current, effort := claudeCurrent(models, transcript, launchModel), launchEffort
	if transcript.Effort != "" {
		effort = transcript.Effort
	}
	// Mewla's own switch, unless Claude's transcript says something newer
	// (an answer, or a switch the person made in the terminal).
	if chosen && !transcript.At.After(choice.at) {
		current, effort = choice.value, choice.effort
	}
	out := claudeAgentModels(models, current, effort)
	out.Pending, out.Failed = c.later.of(worker.ID), c.later.failure(worker.ID)
	return out, nil
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
		out.Models = append(out.Models, server.AgentModel{ID: model.Value, Name: claudeModelLabel(models, model), Efforts: model.Efforts})
	}
	return out
}

// claudeModelLabel is what the person reads for a model: its name with its
// version, as Claude's own lines say it ("Opus 5.5"; the picker's label is
// only "Opus"). Default names the model it runs on now ("Default · Opus
// 5.5"), the one of Claude's list that resolves the same. A model Claude
// gives no name keeps its picker label.
func claudeModelLabel(models []work.ClaudeModel, model work.ClaudeModel) string {
	if model.Name != "" {
		return model.Name
	}
	if model.Value == "default" && model.Resolved != "" {
		for _, other := range models {
			if other.Value != model.Value && other.Name != "" && other.Resolved == model.Resolved {
				return "Default · " + other.Name
			}
		}
	}
	return model.DisplayName
}

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
	chosen := *target
	apply := func(ctx context.Context) error {
		if err := c.pick(ctx, worker, chosen, effort); err != nil {
			return err
		}
		c.mu.Lock()
		c.applied[worker.ID] = claudeChoice{value: chosen.Value, effort: effort, at: time.Now()}
		c.mu.Unlock()
		return nil
	}
	// Claude's picker opens only between turns (typed during one, /model is
	// queued as a message): a choice made while it works or waits for an
	// answer is held and made when the turn ends.
	if c.later.busy(worker.ID) {
		c.later.hold(worker.ID, server.AgentChoice{Model: chosen.Value, Effort: effort}, apply)
		return c.Models(ctx, worker)
	}
	c.later.drop(worker.ID)
	if err := apply(ctx); err != nil {
		return server.AgentModels{}, err
	}
	return c.Models(ctx, worker)
}

// pick opens Claude's picker, moves to the model and effort, reading the
// screen after each key, and takes it for this session only.
func (c *claudeModels) pick(ctx context.Context, worker *classifier.Worker, target work.ClaudeModel, effort string) (err error) {
	sessionID := worker.ID
	// Claude's answer to this switch is the first one it writes from now on
	// (its transcript stamps milliseconds).
	since := time.Now().Truncate(time.Millisecond)
	open := true
	defer func() {
		if err != nil && open {
			_ = c.screen.SendKey(sessionID, "Escape")
		}
	}()
	picker, err := c.openPicker(ctx, sessionID)
	if err != nil {
		// Nothing is open to close (a second Escape opens Claude's rewind).
		open = false
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
	if err := c.waitApplied(ctx, worker, since); err != nil {
		return err
	}
	open = false
	return nil
}

const claudeScreenWait = 4 * time.Second

// claudeOpenWait bounds how long Mewla waits for Claude's picker to open: a
// Claude just started (a new folder, its MCP servers) shows its prompt and
// takes text some seconds before its commands. It fits the switch's budget
// (server.sessionModelSetBudget) with the steps after it.
const claudeOpenWait = 10 * time.Second

// openPicker opens Claude's picker with /model, reading Claude's prompt on
// screen before each key: /model is typed only into an empty prompt, Enter
// is pressed once Claude shows the text (text and Enter that arrive together
// are a paste, and Claude takes the Enter as a new line), and a /model that
// did not open the picker is entered again, never typed again (nor after an
// Enter Claude took without opening it). A prompt
// holding the person's own text is left alone. On failure Mewla's /model is
// taken out of the prompt again, so it never joins the person's next message
// (11-25: on a new Session the retry typed /model twice and Claude answered
// "/model/model" as a message).
func (c *claudeModels) openPicker(ctx context.Context, sessionID string) (work.ClaudePicker, error) {
	deadline := time.Now().Add(claudeOpenWait)
	typed, typedAt := false, time.Time{}
	// entered: Enter was pressed on a /model Claude showed. A prompt empty
	// after that means Claude took the text (as a command still opening, or
	// queued or sent as a message): it is never typed again.
	entered := false
	last := ""
	for {
		screen, err := c.screen.CapturePaneContent(sessionID)
		if err != nil {
			return work.ClaudePicker{}, err
		}
		last = screen
		if picker, ok := work.ParseClaudePicker(screen); ok {
			return picker, nil
		}
		input, prompt := work.ClaudePromptInput(screen)
		switch {
		case !prompt:
			// Claude shows no prompt yet (starting, or a question of its own).
		case input == "" && !entered && (!typed || time.Since(typedAt) > 2*time.Second):
			// Typed again only into a prompt that stayed empty: Claude did
			// not take the text.
			if err := c.screen.SendInput(sessionID, "/model"); err != nil {
				return work.ClaudePicker{}, err
			}
			typed, typedAt = true, time.Now()
		case input == "/model":
			if err := c.screen.SendKey(sessionID, "Enter"); err != nil {
				return work.ClaudePicker{}, err
			}
			entered = true
			if picker, err := c.waitPicker(ctx, sessionID, func(work.ClaudePicker) bool { return true }); err == nil {
				return picker, nil
			}
		case input != "" && !typed:
			return work.ClaudePicker{}, fmt.Errorf("Claude's prompt holds text: %s", input)
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if input, _ := work.ClaudePromptInput(last); input == "/model" {
				for range input {
					_ = c.screen.SendKey(sessionID, "BSpace")
				}
			}
			return work.ClaudePicker{}, fmt.Errorf("Claude's model picker did not open: %s", lastLine(last))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

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

// waitApplied waits for Claude's answer to the switch in its transcript ("Set
// model to … for this session only"; anything else it answered is the
// failure, in its words). The screen is read only for Claude's question in a
// conversation already under way ("Switch model? … the full history gets
// re-read"): the person's pick in the Model sheet is that answer, so its first
// option, "Yes, switch to …", is taken (the classifier knows this confirm and
// never makes it the person's prompt). Lines on the screen do not decide:
// it keeps only its last ones, and after a few switches the earlier answers
// have scrolled away (a counted switch then never "arrived").
func (c *claudeModels) waitApplied(ctx context.Context, worker *classifier.Worker, since time.Time) error {
	sessionID := worker.ID
	deadline := time.Now().Add(claudeScreenWait)
	confirmed := false
	for {
		if answer, ok := work.ReadClaudeModelAnswerSince(c.transcriptOf(worker), since); ok {
			if answer.Named != "" && answer.SessionOnly {
				return nil
			}
			return errors.New(answer.Line)
		}
		screen, err := c.screen.CapturePaneContent(sessionID)
		if err != nil {
			return err
		}
		if _, open := work.ParseClaudePicker(screen); !open && !confirmed && classifier.ClaudeModelSwitchConfirm(screen) {
			if err := c.screen.SendKey(sessionID, "Enter"); err != nil {
				return err
			}
			confirmed = true
			deadline = time.Now().Add(claudeScreenWait)
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("Claude did not confirm the switch: %s", lastLine(screen))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

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

// Settle makes a pick held during the turn that just ended before the next
// message is sent, so that message runs on it.
func (c *claudeModels) Settle(ctx context.Context, worker *classifier.Worker) error {
	if worker == nil {
		return nil
	}
	return c.later.settle(ctx, worker.ID)
}
