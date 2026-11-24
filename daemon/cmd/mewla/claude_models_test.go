package main

import (
	"context"
	"errors"
	"fmt"
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
	// deaf is how many /model Claude takes into its input without opening
	// the picker (it has just started).
	deaf    int
	typed   string
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
		b.WriteString("  Switch model?\n  Your next response will be slower and use\n  more tokens\n\n  This conversation is cached.\n  ❯ 1. Yes, switch to " + f.names[f.pending] + "\n    2. No, go back\n")
	}
	b.WriteString("❯ \n")
	return b.String(), nil
}

func (f *fakeClaude) SendInput(_ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if text != "/model" {
		return fmt.Errorf("typed %q", text)
	}
	f.typed += text
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
			f.history = append(f.history, "❯ /model", "  ⎿  Set model to "+f.names[f.current]+" for this", "  session only with "+f.efforts[f.effort]+" effort")
			return nil
		}
		if f.deaf > 0 {
			f.deaf--
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
	case "C-u":
		f.typed = ""
	case "s":
		f.open = false
		if f.asks {
			f.pending = f.selected
			return nil
		}
		f.current = f.selected
		f.history = append(f.history, "❯ /model", "  ⎿  Set model to "+f.names[f.current]+" for this", "  session only with "+f.efforts[f.effort]+" effort")
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			return errors.New("a digit picks and saves as the default")
		}
	}
	return nil
}

func claudeModelsForTest(screen *fakeClaude) *claudeModels {
	c := newClaudeModels(screen, nil)
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

func newFakeClaude() *fakeClaude {
	return &fakeClaude{
		labels:  []string{"Default (recommended)", "Opus", "Fable", "Sonnet"},
		names:   []string{"Opus 5.5", "Opus 5.5", "Fable 5.2", "Sonnet 5.5"},
		efforts: []string{"low", "medium", "high", "xhigh", "max"},
		current: 3, effort: 1, pending: -1,
		history: []string{"❯ /model", "  ⎿  Set model to Sonnet 5.5 for this", "  session only with medium effort"},
	}
}

func TestClaudeSwitchUsesThePickersSessionOnlyKey(t *testing.T) {
	screen := newFakeClaude()
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
	screen := newFakeClaude()
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
	screen := newFakeClaude()
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
	screen := newFakeClaude()
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
	screen := newFakeClaude()
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
	screen := newFakeClaude()
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

// A Claude that has just started takes /model into its input without opening
// its picker: Mewla clears it and types it again (11-24 gate: the first pick
// on a new Session failed and left "/model" in Claude's input).
func TestClaudeSwitchRetriesAClaudeNotReadyYet(t *testing.T) {
	screen := newFakeClaude()
	screen.deaf = 1
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	after, err := models.SetModel(context.Background(), worker, "fable", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Current != "fable" || screen.typed != "" {
		t.Fatalf("current %q, input left %q", after.Current, screen.typed)
	}
}

// A Session Mewla just made runs its shell before Claude starts: no menu is
// offered until Claude runs (11-24 gate: Claude's list was read with the
// shell's command line, "zsh: no such option: input_format").
func TestClaudeMenuWaitsForClaudeToRun(t *testing.T) {
	models := claudeModelsForTest(newFakeClaude())
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
