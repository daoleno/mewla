package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// fakeClaude draws Claude Code's model picker and answers its keys the way
// 2.1.330 does: it opens on the current model, ↑/↓ wrap, ←/→ step the effort,
// "s" takes the pick for this session only (or asks first, after a turn).
type fakeClaude struct {
	labels   []string
	names    []string
	efforts  []string
	current  int
	effort   int
	open     bool
	selected int
	pending  int
	asks     bool
	history  []string
	keys     []string
}

func (f *fakeClaude) CapturePaneContent(string) (string, error) {
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
	if text != "/model" {
		return fmt.Errorf("typed %q", text)
	}
	return nil
}

func (f *fakeClaude) SendKey(_ string, key string) error {
	f.keys = append(f.keys, key)
	switch key {
	case "Enter":
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

func TestClaudeSwitchLeavesClaudesQuestionToThePerson(t *testing.T) {
	screen := newFakeClaude()
	screen.asks = true
	models := claudeModelsForTest(screen)
	worker := &classifier.Worker{ID: "w1", Command: "claude", Cwd: t.TempDir()}
	_, err := models.SetModel(context.Background(), worker, "opus", "")
	var asks errClaudeAsks
	if !errors.As(err, &asks) || !strings.Contains(err.Error(), "Switch model? Your next response will be slower and use more tokens") || strings.Contains(err.Error(), "cached") {
		t.Fatalf("err = %v", err)
	}
	if screen.current != 3 || screen.pending != 1 {
		t.Fatalf("current %d pending %d", screen.current, screen.pending)
	}
	if last := screen.keys[len(screen.keys)-1]; last != "s" {
		t.Fatalf("Mewla answered Claude's question with %q", last)
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
