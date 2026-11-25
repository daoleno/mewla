package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// fakeClaude draws Claude Code's model picker and answers its keys the way
// 2.1.330 does: it opens on the current model, ↑/↓ wrap, ←/→ step the effort,
// "s" takes the pick for this session only (or asks first, after a turn).
type fakeClaude struct {
	// mu: a pick held for the end of a turn is made from its own goroutine.
	mu       sync.Mutex
	labels   []string
	names    []string
	efforts  []string
	current  int
	effort   int
	open     bool
	selected int
	pending  int
	asks     bool
	// deaf is how many Enter a /model in Claude's prompt takes before the
	// picker opens (Claude has just started).
	deaf int
	// lag is how many screens show the prompt before Claude shows typed text.
	lag int
	// swallow: Enter takes the prompt's text without opening the picker.
	swallow bool
	// placeholder is the dimmed suggestion an empty prompt shows.
	placeholder string
	// transcript is the Session's transcript, where Claude records each
	// /model and its answer.
	transcript string
	// rows, when set, is how many lines the terminal shows: the oldest
	// scroll away.
	rows    int
	typed   string
	inputs  int
	sent    []string
	history []string
	keys    []string
}

func (f *fakeClaude) CapturePaneContent(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	b.WriteString(strings.Join(f.history, "\n") + "\n")
	if f.open {
		b.WriteString("  Select model\n")
		for i, label := range f.labels {
			mark := "  "
			if i == f.selected {
				mark = "❯ "
			}
			fmt.Fprintf(&b, "  %s%d. %s    %s · text\n", mark, i+1, label, f.names[i])
		}
		fmt.Fprintf(&b, "  ● %s effort ←/→ to adjust\n  s to use this session only · Enter to save · Esc to cancel\n", strings.ToUpper(f.efforts[f.effort][:1])+f.efforts[f.effort][1:])
	}
	if f.asks && f.pending >= 0 {
		b.WriteString("  Switch model?\n  Your next response will be slower and use\n  more tokens\n\n  This conversation is cached.\n  ❯ 1. Yes, switch to " + f.names[f.pending] + "\n    2. No, go back\n\n  Enter to confirm · Esc to cancel\n")
	}
	if !f.open && (!f.asks || f.pending < 0) {
		// Claude's prompt between its rules; typed text shows once Claude
		// took it (lag: screens before it does).
		shown := f.typed
		if f.lag > 0 {
			f.lag--
			shown = ""
		}
		if shown == "" {
			shown = f.placeholder
		}
		b.WriteString("────────────────────\n❯ " + shown + "\n────────────────────\n  ⏵⏵ auto mode on (shift+tab to cycle)\n")
	}
	if f.rows > 0 {
		lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		if len(lines) > f.rows {
			lines = lines[len(lines)-f.rows:]
		}
		return strings.Join(lines, "\n") + "\n", nil
	}
	return b.String(), nil
}

// switched is Claude taking the pick: its line on screen, and the /model
// command with its answer in the transcript, as Claude Code 2.1.332 writes
// them.
func (f *fakeClaude) switched() {
	line := "Set model to " + f.names[f.current] + " for this session only with " + f.efforts[f.effort] + " effort"
	f.history = append(f.history, "❯ /model", "  ⎿  Set model to "+f.names[f.current]+" for this", "  session only with "+f.efforts[f.effort]+" effort")
	if f.transcript == "" {
		return
	}
	at := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	record := func(text string) string {
		raw, _ := json.Marshal(map[string]any{"type": "user", "timestamp": at, "message": map[string]any{"role": "user", "content": text}})
		return string(raw) + "\n"
	}
	file, err := os.OpenFile(f.transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(record("<command-name>/model</command-name>\n            <command-message>model</command-message>\n            <command-args></command-args>") +
		record("<local-command-stdout>"+strings.Replace(strings.Replace(line, f.names[f.current], "`"+f.names[f.current]+"`", 1), " "+f.efforts[f.effort]+" effort", " `"+f.efforts[f.effort]+"` effort", 1)+"</local-command-stdout>"))
}

func (f *fakeClaude) SendInput(_ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if text != "/model" {
		return fmt.Errorf("typed %q", text)
	}
	f.typed += text
	f.inputs++
	return nil
}

func (f *fakeClaude) SendKey(_ string, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, key)
	switch key {
	case "Enter":
		if f.pending >= 0 {
			// Claude's question: its first option, "Yes, switch to …".
			f.current, f.pending = f.pending, -1
			f.switched()
			return nil
		}
		if f.deaf > 0 || f.lag > 0 {
			// Not taking commands yet, or text and Enter came as one paste:
			// the text stays in the prompt.
			if f.deaf > 0 {
				f.deaf--
			}
			return nil
		}
		if f.swallow {
			// A Claude that takes the command without opening its picker
			// (it queues it while a turn the hub missed runs).
			f.sent = append(f.sent, f.typed)
			f.typed = ""
			return nil
		}
		if f.typed != "/model" {
			// What Claude does with any other text: it is sent as a message.
			f.sent = append(f.sent, f.typed)
			f.typed = ""
			return nil
		}
		f.typed = ""
		f.open, f.selected, f.pending = true, f.current, -1
	case "Down":
		f.selected = (f.selected + 1) % len(f.labels)
	case "Up":
		f.selected = (f.selected + len(f.labels) - 1) % len(f.labels)
	case "Right":
		if f.effort < len(f.efforts)-1 {
			f.effort++
		}
	case "Left":
		if f.effort > 0 {
			f.effort--
		}
	case "Escape":
		f.open = false
	case "BSpace":
		if f.typed != "" {
			f.typed = f.typed[:len(f.typed)-1]
		}
	case "s":
		f.open = false
		if f.asks {
			f.pending = f.selected
			return nil
		}
		f.current = f.selected
		f.switched()
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			return errors.New("a digit picks and saves as the default")
		}
	}
	return nil
}

func claudeModelsForTest(screen *fakeClaude) *claudeModels {
	c := newClaudeModels(screen, nil)
	c.transcriptOf = func(*classifier.Worker) string { return screen.transcript }
	c.read = func(context.Context, string, string) ([]work.ClaudeModel, error) {
		efforts := []string{"low", "medium", "high", "xhigh", "max"}
		return []work.ClaudeModel{
			{Value: "default", DisplayName: "Default (recommended)", Resolved: "claude-opus-5-5", Efforts: efforts},
			{Value: "opus", DisplayName: "Opus", Resolved: "claude-opus-5-5", Name: "Opus 5.5", Efforts: efforts},
			{Value: "fable", DisplayName: "Fable", Resolved: "claude-fable-5-2", Name: "Fable 5.2", Efforts: efforts},
			{Value: "sonnet", DisplayName: "Sonnet", Resolved: "claude-sonnet-5-5", Name: "Sonnet 5.5", Efforts: efforts},
		}, nil
	}
	return c
}

func newFakeClaude(t *testing.T) *fakeClaude {
	return &fakeClaude{transcript: filepath.Join(t.TempDir(), "session.jsonl"),
		labels:  []string{"Default (recommended)", "Opus", "Fable", "Sonnet"},
		names:   []string{"Opus 5.5", "Opus 5.5", "Fable 5.2", "Sonnet 5.5"},
		efforts: []string{"low", "medium", "high", "xhigh", "max"},
		current: 3, effort: 1, pending: -1,
		history: []string{"❯ /model", "  ⎿  Set model to Sonnet 5.5 for this", "  session only with medium effort"},
	}
}

func TestClaudeSwitchUsesThePickersSessionOnlyKey(t *testing.T) {
	screen := newFakeClaude(t)
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude --permission-mode default", Cwd: t.TempDir()}
	after, err := models.SetModel(context.Background(), worker, "fable", "high")
	if err != nil {
		t.Fatal(err)
	}
	if screen.current != 2 || screen.efforts[screen.effort] != "high" {
		t.Fatalf("Claude is on %s with %s effort", screen.labels[screen.current], screen.efforts[screen.effort])
	}
	if got := strings.Join(screen.keys, " "); got != "Enter Up Right s" {
		t.Fatalf("keys = %q", got)
	}
	if after.Current != "fable" || after.Effort != "high" || len(after.Models) != 4 {
		t.Fatalf("menu after = %#v", after)
	}
}

// After a turn Claude asks to confirm a switch (the conversation is re-read).
// The person's pick in the Model sheet is that answer: the switch is made and
// no question is left open to swallow their next message (11-24: it was
// reported as "Could not switch model" and left on screen).
func TestClaudeSwitchAnswersClaudesQuestion(t *testing.T) {
	screen := newFakeClaude(t)
	screen.asks = true
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	after, err := models.SetModel(context.Background(), worker, "opus", "")
	if err != nil {
		t.Fatal(err)
	}
	if screen.current != 1 || screen.pending != -1 || screen.open {
		t.Fatalf("current %d pending %d open %v", screen.current, screen.pending, screen.open)
	}
	if got := strings.Join(screen.keys, " "); got != "Enter Up Up s Enter" {
		t.Fatalf("keys = %q", got)
	}
	if after.Current != "opus" {
		t.Fatalf("menu after = %#v", after)
	}
}

// A switch that fails after the picker opened leaves nothing open on screen.
func TestClaudeSwitchFailureClosesThePicker(t *testing.T) {
	screen := newFakeClaude(t)
	screen.asks = true
	screen.names[1] = "Opus 5.5"
	models := claudeModelsForTest(screen)
	models.screen = &noConfirmClaude{screen}
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	if _, err := models.SetModel(context.Background(), worker, "opus", ""); err == nil {
		t.Fatal("a switch Claude never confirmed was reported as made")
	}
	if last := screen.keys[len(screen.keys)-1]; last != "Escape" {
		t.Fatalf("left Claude's screen after %q", last)
	}
}

// noConfirmClaude is a Claude whose question ignores Enter.
type noConfirmClaude struct{ *fakeClaude }

func (n *noConfirmClaude) SendKey(id, key string) error {
	if key == "Enter" && n.pending >= 0 {
		n.keys = append(n.keys, key)
		return nil
	}
	return n.fakeClaude.SendKey(id, key)
}

// A choice made while Claude works (or waits for an answer in its turn) is
// held, shown as pending, and made in Claude's picker when the turn ends.
func TestClaudeSwitchWhileWorkingIsMadeWhenTheTurnEnds(t *testing.T) {
	screen := newFakeClaude(t)
	hub := agentevents.NewHub()
	models := claudeModelsForTest(screen)
	models.hub = hub
	models.later = newTurnPending(hub)
	models.later.poll = 10 * time.Millisecond
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "t1", At: time.Now()})
	after, err := models.SetModel(context.Background(), worker, "fable", "high")
	if err != nil {
		t.Fatalf("a switch while Claude works was refused: %v", err)
	}
	if after.Pending == nil || after.Pending.Model != "fable" || after.Pending.Effort != "high" || len(screen.keys) != 0 {
		t.Fatalf("pending %#v keys %v", after.Pending, screen.keys)
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnFinished, TurnID: "t1", Status: agentevents.TurnCompleted, At: time.Now()})
	deadline := time.Now().Add(5 * time.Second)
	on := func() (int, string) {
		screen.mu.Lock()
		defer screen.mu.Unlock()
		return screen.current, screen.efforts[screen.effort]
	}
	for current, _ := on(); current != 2 && time.Now().Before(deadline); current, _ = on() {
		time.Sleep(20 * time.Millisecond)
	}
	if current, effort := on(); current != 2 || effort != "high" {
		t.Fatalf("after the turn Claude is on %s", screen.labels[current])
	}
	for models.later.of("w1") != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if models.later.of("w1") != nil {
		t.Fatal("the choice stayed pending after it was made")
	}
}

// A message sent right after the turn ends runs on the model picked during
// that turn: the send makes the held pick first instead of racing it.
func TestClaudeHeldPickIsMadeBeforeTheNextMessage(t *testing.T) {
	screen := newFakeClaude(t)
	hub := agentevents.NewHub()
	models := claudeModelsForTest(screen)
	models.hub = hub
	models.later = newTurnPending(hub)
	models.later.poll = time.Hour // the background wait never gets there first
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "t1", At: time.Now()})
	if _, err := models.SetModel(context.Background(), worker, "fable", "high"); err != nil {
		t.Fatal(err)
	}
	if err := models.Settle(context.Background(), worker); err != nil || len(screen.keys) != 0 {
		t.Fatalf("a pick was made while Claude works: %v keys %v", err, screen.keys)
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnFinished, TurnID: "t1", Status: agentevents.TurnCompleted, At: time.Now()})
	if err := models.Settle(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	if screen.current != 2 || screen.efforts[screen.effort] != "high" || models.later.of("w1") != nil {
		t.Fatalf("before the next message Claude is on %s, pending %#v", screen.labels[screen.current], models.later.of("w1"))
	}
}

// The Session's model before its first message is the one Claude was started
// with: the Worker's recorded command keeps only --session-id.
func TestClaudeCurrentReadsTheLaunchFlagsOfTheProcess(t *testing.T) {
	screen := newFakeClaude(t)
	models := claudeModelsForTest(screen)
	models.launchArgs = func(*classifier.Worker) string {
		return "claude --model sonnet --effort low --settings {} --session-id 3311e9d4"
	}
	worker := &classifier.Worker{ID: "w1", Command: "claude --session-id 3311e9d4", Cwd: t.TempDir(), ProcessID: 42}
	got, err := models.Models(context.Background(), worker)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "sonnet" || got.Effort != "low" {
		t.Fatalf("current %q effort %q", got.Current, got.Effort)
	}
}

func TestClaudeCurrentFollowsTheTranscript(t *testing.T) {
	models := []work.ClaudeModel{
		{Value: "default", Resolved: "claude-opus-5-5"},
		{Value: "opus", Resolved: "claude-opus-5-5", Name: "Opus 5.5"},
		{Value: "sonnet", Resolved: "claude-sonnet-5-5", Name: "Sonnet 5.5"},
	}
	for _, tc := range []struct {
		transcript work.ClaudeTranscriptChoice
		launch     string
		want       string
	}{
		{want: "default"},
		{launch: "sonnet", want: "sonnet"},
		{transcript: work.ClaudeTranscriptChoice{Resolved: "claude-opus-5-5"}, want: "opus"},
		{transcript: work.ClaudeTranscriptChoice{Resolved: "claude-opus-5-5"}, launch: "default", want: "default"},
		{transcript: work.ClaudeTranscriptChoice{Named: "Sonnet 5.5"}, launch: "opus", want: "sonnet"},
		{transcript: work.ClaudeTranscriptChoice{Resolved: "claude-haiku-9"}, want: "claude-haiku-9"},
	} {
		if got := claudeCurrent(models, tc.transcript, tc.launch); got != tc.want {
			t.Errorf("%+v launch %q: %q, want %q", tc.transcript, tc.launch, got, tc.want)
		}
	}
}

// The person reads what they get: each model with its version, as Claude's
// own lines name it, and Default with the model it runs on now (11-25: the
// sheet said "Opus", "Sonnet", "Default (recommended)").
func TestClaudeMenuNamesEachModelWithItsVersion(t *testing.T) {
	models := claudeModelsForTest(newFakeClaude(t))
	got, err := models.Models(context.Background(), &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, model := range got.Models {
		names = append(names, model.ID+"="+model.Name)
	}
	if want := "default=Default · Opus 5.5 opus=Opus 5.5 fable=Fable 5.2 sonnet=Sonnet 5.5"; strings.Join(names, " ") != want {
		t.Fatalf("menu = %q, want %q", strings.Join(names, " "), want)
	}
	// A model Claude gives no name keeps its picker label.
	if label := claudeModelLabel(nil, work.ClaudeModel{Value: "custom", DisplayName: "Custom"}); label != "Custom" {
		t.Fatalf("unnamed model = %q", label)
	}
}

// A pick held for the end of a turn that Claude then does not take is said in
// the menu, with Claude's reason, until the next pick: it was only logged,
// and the Model sheet showed the old model with nothing said.
func TestClaudeHeldPickThatFailsSaysWhy(t *testing.T) {
	screen := newFakeClaude(t)
	hub := agentevents.NewHub()
	models := claudeModelsForTest(screen)
	models.hub = hub
	models.later = newTurnPending(hub)
	models.later.poll = 10 * time.Millisecond
	broken := &brokenScreen{fakeClaude: screen, gone: true}
	models.screen = broken
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnStarted, TurnID: "t1", At: time.Now()})
	if _, err := models.SetModel(context.Background(), worker, "fable", "high"); err != nil {
		t.Fatal(err)
	}
	hub.Publish(agentevents.Event{WorkerID: "w1", Kind: agentevents.KindTurnFinished, TurnID: "t1", Status: agentevents.TurnCompleted, At: time.Now()})
	deadline := time.Now().Add(5 * time.Second)
	for models.later.of("w1") != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	after, err := models.Models(context.Background(), worker)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != nil || after.Failed == nil || after.Failed.Model != "fable" || after.Failed.Effort != "high" || !strings.Contains(after.Failed.Reason, "the pane is gone") {
		t.Fatalf("after a failed held pick: pending %#v failed %#v", after.Pending, after.Failed)
	}
	// The next pick clears it.
	broken.gone = false
	if after, err = models.SetModel(context.Background(), worker, "opus", ""); err != nil || after.Failed != nil {
		t.Fatalf("next pick: %v, failed %#v", err, after.Failed)
	}
}

// brokenScreen is a Session whose screen cannot be read (its pane is gone).
type brokenScreen struct {
	*fakeClaude
	gone bool
}

func (b *brokenScreen) CapturePaneContent(id string) (string, error) {
	if b.gone {
		return "", errors.New("the pane is gone")
	}
	return b.fakeClaude.CapturePaneContent(id)
}

// A Claude that has just started takes /model into its input without opening
// its picker: Mewla clears it and types it again (11-24 gate: the first pick
// on a new Session failed and left "/model" in Claude's input).
func TestClaudeSwitchRetriesAClaudeNotReadyYet(t *testing.T) {
	screen := newFakeClaude(t)
	screen.deaf = 1
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	after, err := models.SetModel(context.Background(), worker, "fable", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Current != "fable" || screen.typed != "" || screen.inputs != 1 || len(screen.sent) != 0 {
		t.Fatalf("current %q, input left %q, typed %d times, sent %q", after.Current, screen.typed, screen.inputs, screen.sent)
	}
}

// A new Claude shows typed text a moment after it took it; an Enter before
// that is part of a paste. /model is entered once Claude shows it, and never
// typed twice (11-25: a new Session's retry made "/model/model", which
// Claude answered as a message, and the switch took 16 s).
func TestClaudeSwitchEntersModelOnceClaudeShowsIt(t *testing.T) {
	screen := newFakeClaude(t)
	screen.lag = 3
	screen.placeholder = `Try "refactor <filepath>"`
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	started := time.Now()
	after, err := models.SetModel(context.Background(), worker, "fable", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Current != "fable" || screen.inputs != 1 || len(screen.sent) != 0 || time.Since(started) > 3*time.Second {
		t.Fatalf("current %q, typed %d times, sent %q, took %s", after.Current, screen.inputs, screen.sent, time.Since(started))
	}
}

// A /model Claude took without opening its picker is never typed again: the
// switch fails with Claude's last line, and at most the one /model went in.
func TestClaudeSwitchNeverTypesModelTwice(t *testing.T) {
	screen := newFakeClaude(t)
	screen.swallow = true
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	if _, err := models.SetModel(context.Background(), worker, "fable", ""); err == nil {
		t.Fatal("a picker that never opened was reported as a switch")
	}
	if screen.inputs != 1 || len(screen.sent) > 1 {
		t.Fatalf("typed %d times, sent %q", screen.inputs, screen.sent)
	}
}

// The person's own text in Claude's prompt is left as it is: the switch is
// refused with the reason, and nothing is typed or entered.
func TestClaudeSwitchLeavesThePersonsDraft(t *testing.T) {
	screen := newFakeClaude(t)
	screen.typed = "fix the login bug"
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	_, err := models.SetModel(context.Background(), worker, "fable", "")
	if err == nil || !strings.Contains(err.Error(), "fix the login bug") {
		t.Fatalf("err = %v", err)
	}
	if screen.typed != "fix the login bug" || len(screen.keys) != 0 || len(screen.sent) != 0 {
		t.Fatalf("draft %q, keys %v, sent %q", screen.typed, screen.keys, screen.sent)
	}
}

// A Session Mewla just made runs its shell before Claude starts: no menu is
// offered until Claude runs (11-24 gate: Claude's list was read with the
// shell's command line, "zsh: no such option: input_format").
func TestClaudeMenuWaitsForClaudeToRun(t *testing.T) {
	models := claudeModelsForTest(newFakeClaude(t))
	if models.ModelsAvailable(&classifier.Worker{ID: "w1", Command: "zsh"}) {
		t.Fatal("a shell was offered Claude's models")
	}
	if !models.ModelsAvailable(&classifier.Worker{ID: "w1", Command: "claude --session-id 3311e9d4"}) {
		t.Fatal("a running Claude was offered no models")
	}
	// A Session starts as its login shell running the launch command; the
	// recorded command stays the shell's a while after it became Claude.
	wrapped := &classifier.Worker{ID: "w1", Command: "/bin/zsh -i -l -c claude --model haiku --settings {}", ProcessID: 42}
	program := "/bin/zsh -i -l -c claude --model haiku --settings {}"
	models.launchArgs = func(*classifier.Worker) string { return program }
	if models.ModelsAvailable(wrapped) {
		t.Fatal("the login shell was offered Claude's models before it started Claude")
	}
	program = "claude --model haiku --settings {}"
	read := ""
	models.read = func(_ context.Context, command, _ string) ([]work.ClaudeModel, error) {
		read = command
		return []work.ClaudeModel{{Value: "haiku", DisplayName: "Haiku", Resolved: "claude-haiku-5-5", Efforts: []string{"low"}}}, nil
	}
	if !models.ModelsAvailable(wrapped) {
		t.Fatal("the shell became Claude, and no models were offered")
	}
	if _, err := models.Models(context.Background(), wrapped); err != nil || read != program {
		t.Fatalf("Claude's list was read with %q: %v", read, err)
	}
}

// Claude's answer to each switch is read from its transcript: the screen
// keeps only its last lines. A Session switched a dozen times in a row (the
// 0.4.29 held-pick loop) was told "Claude did not confirm the switch" from
// the fifth on, while Claude had switched each time: the earlier "Set model
// to" lines had scrolled away, so their count stopped growing.
func TestClaudeSwitchesWhoseLinesScrolledAwayStillLand(t *testing.T) {
	screen := newFakeClaude(t)
	screen.asks = true
	// A turn and a switch are 9 lines; the pane holds two of them above
	// Claude's prompt, so each new "Set model to" line pushes an old one out.
	screen.rows = 22
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	picks := []string{"opus", "fable", "sonnet"}
	for i := 0; i < 12; i++ {
		want := picks[i%len(picks)]
		after, err := models.SetModel(context.Background(), worker, want, "low")
		if err != nil {
			t.Fatalf("switch %d to %s: %v", i+1, want, err)
		}
		if after.Current != want || after.Effort != "low" || screen.labels[screen.current] == "" {
			t.Fatalf("switch %d: menu says %s/%s, want %s/low", i+1, after.Current, after.Effort, want)
		}
		// Claude answers a turn between the switches, as in the held-pick loop.
		screen.mu.Lock()
		screen.history = append(screen.history, "❯ Run this exact shell command, then reply with the word done: sleep 12", "● Bash(sleep 12)", "  ⎿  (No output)", "● done", "", "✻ Worked for 14s")
		screen.mu.Unlock()
	}
}

// A /model Claude answers with no switch fails with Claude's own words.
func TestClaudeSwitchClaudeDidNotMakeSaysWhy(t *testing.T) {
	screen := newFakeClaude(t)
	models := claudeModelsForTest(screen)
	models.screen = &keepingClaude{screen}
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	_, err := models.SetModel(context.Background(), worker, "opus", "")
	if err == nil || err.Error() != "Kept model as Sonnet 5.5" {
		t.Fatalf("err = %v, want Claude's own answer", err)
	}
}

// keepingClaude closes its picker on "s" without switching, and says so.
type keepingClaude struct{ *fakeClaude }

func (k *keepingClaude) SendKey(id, key string) error {
	if key != "s" {
		return k.fakeClaude.SendKey(id, key)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = append(k.keys, key)
	k.open = false
	at := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	file, err := os.OpenFile(k.transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, text := range []string{"<command-name>/model</command-name>", "<local-command-stdout>Kept model as Sonnet 5.5</local-command-stdout>"} {
		raw, _ := json.Marshal(map[string]any{"type": "user", "timestamp": at, "message": map[string]any{"role": "user", "content": text}})
		_, _ = file.Write(append(raw, '\n'))
	}
	return nil
}
