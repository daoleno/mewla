package work

import (
	"reflect"
	"testing"
)

func TestParseClaudeInitializeReadsThePickersModels(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"mewla-models","response":{"commands":[],"models":[` +
		`{"value":"default","resolvedModel":"claude-opus-5-5","displayName":"Default (recommended)","supportsEffort":true,"supportedEffortLevels":["low","medium","high","xhigh","max"]},` +
		`{"value":"sonnet","resolvedModel":"claude-sonnet-5-5","displayName":"Sonnet","description":"Sonnet 5.5 · Efficient for routine tasks","supportsEffort":true,"supportedEffortLevels":["low","medium","high"]},` +
		`{"value":"old","resolvedModel":"claude-old","displayName":"Old","supportsEffort":false,"supportedEffortLevels":["low"]}]}}}`)
	models, ok, err := ParseClaudeInitialize(line)
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := []ClaudeModel{
		{Value: "default", DisplayName: "Default (recommended)", Resolved: "claude-opus-5-5", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{Value: "sonnet", DisplayName: "Sonnet", Resolved: "claude-sonnet-5-5", Name: "Sonnet 5.5", Efforts: []string{"low", "medium", "high"}},
		{Value: "old", DisplayName: "Old", Resolved: "claude-old"},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v", models)
	}
	if _, ok, _ := ParseClaudeInitialize([]byte(`{"type":"system","subtype":"init"}`)); ok {
		t.Fatal("a line that is not the answer was taken")
	}
	if _, ok, err := ParseClaudeInitialize([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"mewla-models","error":"Not logged in"}}`)); !ok || err == nil || err.Error() != "Claude Code: Not logged in" {
		t.Fatalf("refusal: ok=%v err=%v", ok, err)
	}
}

func TestClaudeTranscriptStateFollowsSwitchLinesAndAnswers(t *testing.T) {
	switched := `{"type":"user","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"<local-command-stdout>Set model to ` + "`Sonnet 5.5`" + ` for this session only with ` + "`low`" + ` effort</local-command-stdout>"}}`
	answered := `{"type":"assistant","timestamp":"2026-01-01T00:00:02Z","message":{"model":"claude-sonnet-5-5","role":"assistant"}}`
	typed := `{"type":"user","timestamp":"2026-01-01T00:00:03Z","message":{"role":"user","content":"<local-command-stdout>Set model to ` + "`Opus 5.5`" + ` and saved as your default for new sessions</local-command-stdout>"}}`
	synthetic := `{"type":"assistant","timestamp":"2026-01-01T00:00:04Z","message":{"model":"<synthetic>","role":"assistant"}}`

	got := ClaudeTranscriptState([]byte(switched))
	if got.Named != "Sonnet 5.5" || got.Effort != "low" || got.Resolved != "" || got.At.Second() != 1 {
		t.Fatalf("after the switch: %#v", got)
	}
	got = ClaudeTranscriptState([]byte(switched + "\n" + answered))
	if got.Resolved != "claude-sonnet-5-5" || got.Named != "" || got.Effort != "low" || got.At.Second() != 2 {
		t.Fatalf("after the answer: %#v", got)
	}
	got = ClaudeTranscriptState([]byte(switched + "\n" + answered + "\n" + typed + "\n" + synthetic + "\n{\"type\":\"assist"))
	if got.Named != "Opus 5.5" || got.Effort != "low" || got.At.Second() != 3 {
		t.Fatalf("after a typed /model: %#v", got)
	}
	if got := ClaudeTranscriptState(nil); got.Named != "" || got.Resolved != "" {
		t.Fatalf("empty transcript: %#v", got)
	}
}

func TestClaudeLaunchModelReadsModelAndEffortFlags(t *testing.T) {
	for command, want := range map[string][2]string{
		"claude --permission-mode default":            {"", ""},
		"claude --model opus --effort high":           {"opus", "high"},
		"env A=1 claude --model=sonnet --effort=low":  {"sonnet", "low"},
		"claude --model 'claude-sonnet-5-5' --resume": {"claude-sonnet-5-5", ""},
	} {
		model, effort := ClaudeLaunchModel(command)
		if model != want[0] || effort != want[1] {
			t.Errorf("%q: model %q effort %q", command, model, effort)
		}
	}
}

// The picker as Claude Code 2.1.330 draws it (scratch config, 160 columns).
const claudePickerScreen = `❯ /model
  ⎿  Set model to Sonnet 5.5 and saved as your default for new sessions
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  Select model
  Your pick is the default for new sessions. For other models, use --model.
    1. Default (recommended)    Use the default model (currently Opus 5.5) · $4/$20 per Mtok
    2. Opus                     Opus 5.5 · Best for everyday, complex tasks · $4/$20 per Mtok
  ❯ 3. Fable                    Fable 5.2 · Most capable for your hardest and longest-running tasks · $8/$40 per Mtok
    4. Sonnet ✔                 Sonnet 5.5 · Efficient for routine tasks · $2/$10 per Mtok
    5. Haiku                    Haiku 5.5 · Fastest for quick answers · $0.10/$0.50 per Mtok ($0.50/$2.50 for prompts over 100k)
  ● High effort ←/→ to adjust
  s to use this session only · Enter to save · Esc to cancel
`

func TestParseClaudePickerReadsOptionsSelectionAndEffort(t *testing.T) {
	picker, ok := ParseClaudePicker(claudePickerScreen)
	if !ok {
		t.Fatal("picker not read")
	}
	want := ClaudePicker{Options: []string{"Default (recommended)", "Opus", "Fable", "Sonnet", "Haiku"}, Selected: 2, Effort: "high", SessionOnly: true}
	if !reflect.DeepEqual(picker, want) {
		t.Fatalf("picker = %#v", picker)
	}
	first, ok := ParseClaudePicker("  Select model\n  ❯ 1. Default (recommended) ✔  Use the default model\n    2. Opus   Opus 5.5\n  ◐ Medium effort (default) ←/→ to adjust\n")
	if !ok || first.Selected != 0 || first.Options[0] != "Default (recommended)" || first.Effort != "medium" || first.SessionOnly {
		t.Fatalf("first = %#v", first)
	}
	if _, ok := ParseClaudePicker("❯ \n  ⏵⏵ auto mode on\n"); ok {
		t.Fatal("a screen without the picker was read as one")
	}
}
