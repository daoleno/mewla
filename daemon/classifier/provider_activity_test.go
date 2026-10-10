package classifier

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultActivityProbe_RegistersOnlyPaneAndProcessAdapters(t *testing.T) {
	probe := DefaultActivityProbe()
	if probe == nil || len(probe.adapters) != 3 {
		t.Fatalf("adapters = %d, want 3", len(probe.adapters))
	}
	names := map[string]bool{}
	for _, adapter := range probe.adapters {
		names[adapter.Name()] = true
	}
	for _, want := range []string{"cursor", "codex", "claude"} {
		if !names[want] {
			t.Fatalf("missing adapter %q in %#v", want, names)
		}
	}
	if names["grok"] {
		t.Fatalf("Grok has no pane/process adapter behavior: %#v", names)
	}
}

func TestProviderAdapters_OrdinaryShellNoMatch(t *testing.T) {
	got := DefaultActivityProbe().Infer(ActivityInput{
		Worker:      Worker{Command: "zsh", Cwd: "/tmp"},
		PaneContent: "$ echo hi\nhi\n$ ls\nfile\n$",
	})
	if got.State != "" || got.Provider != "" {
		t.Fatalf("ordinary shell got %#v, want empty", got)
	}
}

func TestDefaultActivityProbe_TranscriptOnlyEvidenceStaysUnknown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now().UTC()

	tests := []struct {
		name    string
		command string
		cwd     string
		pane    string
		write   func(*testing.T, string, string, time.Time) string
	}{
		{
			name:    "codex active rollout",
			command: "codex",
			cwd:     "/repo/codex",
			pane:    "OpenAI Codex\n› ",
			write:   writeIgnoredCodexRollout,
		},
		{
			name:    "claude blocked transcript",
			command: "claude",
			cwd:     "/repo/claude",
			pane:    "Claude Code\n❯ ",
			write:   writeIgnoredClaudeTranscript,
		},
		{
			name:    "cursor active transcript",
			command: "cursor-agent",
			cwd:     "/repo/cursor",
			pane:    "Cursor Agent\n→ Add a follow-up",
			write:   writeIgnoredCursorTranscript,
		},
		{
			name:    "grok failed updates",
			command: "grok",
			cwd:     "/repo/grok",
			pane:    "Grok\n❯ ",
			write:   writeIgnoredGrokUpdates,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := testCase.write(t, home, testCase.cwd, now)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			worker := &Worker{
				ID:        testCase.name,
				Command:   testCase.command,
				Cwd:       testCase.cwd,
				PaneAlive: true,
				State:     StateUnknown,
			}
			signal := DefaultActivityProbe().Infer(ActivityInput{
				Worker:      *worker,
				PaneContent: testCase.pane,
			})
			state, _ := ResolveSessionStatus(worker, StateUnknown, "Session idle", now, signal)
			if state != StateUnknown {
				t.Fatalf("state = %q from signal %#v, want honest unknown", state, signal)
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("provider transcript changed: before=%q after=%q", before, after)
			}
		})
	}
}

func TestProviderAdapters_RetainPaneAndProcessEvidence(t *testing.T) {
	tests := []struct {
		name       string
		input      ActivityInput
		wantState  WorkerState
		wantSource string
	}{
		{
			name:       "codex approval",
			input:      ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: "OpenAI Codex\nDo you want to continue?"},
			wantState:  StateBlocked,
			wantSource: "codex_pane_blocked",
		},
		{
			name:       "codex visible working",
			input:      ActivityInput{Worker: Worker{Command: "codex"}, PaneContent: "OpenAI Codex\nWorking...\nesc to interrupt"},
			wantState:  StateRunning,
			wantSource: "codex_pane_working",
		},
		{
			name:       "claude permission",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n Edit file\n Do you want to make this edit to main.go?\n ❯ 1. Yes\n   2. Yes, allow all edits during this session (shift+tab)\n   3. No\n Esc to cancel · Tab to amend\n"},
			wantState:  StateBlocked,
			wantSource: "claude_pane_blocked",
		},
		{
			name:       "claude ask user question",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n←  ☐ Fruit  ☐ Colors  ✔ Submit  →\nWhich fruit?\n❯ 1. Apple\n     Red\n  2. Type something.\n────\n  3. Chat about this\n\nEnter to select · Tab/Arrow keys to navigate · Esc to\ncancel\n"},
			wantState:  StateBlocked,
			wantSource: "claude_pane_choice",
		},
		{
			name:       "claude ask user question review",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\nReview your answers\n ● Which fruit?\n   → Apple\nReady to submit your answers?\n❯ 1. Submit answers\n  2. Cancel\n"},
			wantState:  StateBlocked,
			wantSource: "claude_pane_choice",
		},
		{
			name:       "claude plan approval",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n Ready to code?\n Here is Claude's plan:\n Steps\n Claude has written up a plan and is ready to execute. Would you like to proceed?\n ❯ 1. Yes, and use auto mode\n   2. Yes, manually approve edits\n   3. Tell Claude what to change\n      shift+tab to approve with this feedback\n ctrl+g to edit in VS Code\n"},
			wantState:  StateBlocked,
			wantSource: "claude_pane_plan",
		},
		{
			name:       "claude bash permission",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n Bash command\n mkdir -p made_by_test\n Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, and don't ask again for mkdir commands\n   3. No\n Esc to cancel · Tab to amend\n"},
			wantState:  StateBlocked,
			wantSource: "claude_pane_blocked",
		},
		{
			name:       "claude answered choice is not blocked",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n● User answered Claude's questions:\n  ⎿  · Which fruit? → Apple\n● Apple it is.\n────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)\n"},
			wantState:  StateUnknown,
			wantSource: "claude_idle",
		},
		{
			name:       "claude conversation about permissions is not blocked",
			input:      ActivityInput{Worker: Worker{Command: "claude"}, PaneContent: "Claude Code\n● The AskUserQuestion prompt needs permission required to allow the user to answer.\n  Do you want to proceed with the plan?\n────\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt\n"},
			wantState:  StateUnknown,
			wantSource: "claude_idle",
		},
		{
			name:       "cursor workspace trust",
			input:      ActivityInput{Worker: Worker{Command: "cursor-agent"}, PaneContent: "Cursor Agent\nWorkspace Trust Required\nTrust this workspace?"},
			wantState:  StateBlocked,
			wantSource: "cursor_pane_trust",
		},
		{
			name:       "cursor permission",
			input:      ActivityInput{Worker: Worker{Command: "cursor-agent"}, PaneContent: "Cursor Agent\nPermission required"},
			wantState:  StateBlocked,
			wantSource: "cursor_pane_permission",
		},
		{
			name:       "cursor stop marker",
			input:      ActivityInput{Worker: Worker{Command: "cursor-agent"}, PaneContent: "Cursor Agent\nctrl+c to stop"},
			wantState:  StateRunning,
			wantSource: "cursor_pane_stop_marker",
		},
		{
			name:       "cursor non MCP child",
			input:      ActivityInput{Worker: Worker{Command: "cursor-agent"}, PaneContent: "Cursor Agent", ToolChildActive: true},
			wantState:  StateRunning,
			wantSource: "cursor_tool_child",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := DefaultActivityProbe().Infer(testCase.input)
			if got.State != testCase.wantState || got.Source != testCase.wantSource {
				t.Fatalf("signal = %#v, want state=%q source=%q", got, testCase.wantState, testCase.wantSource)
			}
		})
	}
}

func TestCodexHistoricalApprovalTextBeforeIdleComposerDoesNotBlock(t *testing.T) {
	pane := "OpenAI Codex\n• Test output: Press enter to continue\n› \nmodel footer"
	signal := NewCodexActivityAdapter().Infer(ActivityInput{
		Worker:      Worker{Command: "codex"},
		PaneContent: pane,
	})
	if signal.State != StateUnknown || signal.Source != "codex_idle" {
		t.Fatalf("historical approval text produced activity %#v", signal)
	}
	state, _ := Classify(true, strings.Split(pane, "\n"), "codex")
	if state != StateUnknown {
		t.Fatalf("historical approval text classified as %q, want unknown", state)
	}
}

func TestResolveSessionStatus_ProgressAndStickyFactsOutrankPaneEvidence(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	lease := now.Add(time.Minute)

	tests := []struct {
		name       string
		worker     *Worker
		classified WorkerState
		signal     ActivitySignal
		want       WorkerState
	}{
		{
			name: "active progress lease",
			worker: &Worker{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "Explicit progress",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &lease,
			},
			classified: StateUnknown,
			signal:     ActivitySignal{State: StateUnknown},
			want:       StateRunning,
		},
		{
			name:       "pane blocked beats running signal",
			worker:     &Worker{PaneAlive: true, State: StateUnknown},
			classified: StateBlocked,
			signal:     ActivitySignal{State: StateRunning, Source: "cursor_pane_stop_marker"},
			want:       StateBlocked,
		},
		{
			name:       "pane failed beats running signal",
			worker:     &Worker{PaneAlive: true, State: StateUnknown},
			classified: StateFailed,
			signal:     ActivitySignal{State: StateRunning, Source: "codex_pane_working"},
			want:       StateFailed,
		},
		{
			name: "sticky done",
			worker: &Worker{
				PaneAlive:      true,
				State:          StateDone,
				Summary:        "Finished",
				LastProgressAt: &progressAt,
			},
			classified: StateUnknown,
			signal:     ActivitySignal{State: StateRunning, Source: "codex_pane_working"},
			want:       StateDone,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, _ := ResolveSessionStatus(testCase.worker, testCase.classified, "detail", now, testCase.signal)
			if got != testCase.want {
				t.Fatalf("state = %q, want %q", got, testCase.want)
			}
		})
	}
}

func writeIgnoredCodexRollout(t *testing.T, home, cwd string, now time.Time) string {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	return writeIgnoredTranscript(t, filepath.Join(dir, "rollout-active.jsonl"), now,
		`{"type":"session_meta","payload":{"cwd":"`+cwd+`"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
	)
}

func writeIgnoredClaudeTranscript(t *testing.T, home, cwd string, now time.Time) string {
	t.Helper()
	project := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	return writeIgnoredTranscript(t, filepath.Join(home, ".claude", "projects", project, "blocked.jsonl"), now,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"ask","name":"AskUserQuestion"}]}}`,
	)
}

func writeIgnoredCursorTranscript(t *testing.T, home, cwd string, now time.Time) string {
	t.Helper()
	project := strings.Trim(strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-"), "-")
	path := filepath.Join(home, ".cursor", "projects", project, "agent-transcripts", "active", "active.jsonl")
	return writeIgnoredTranscript(t, path, now, `{"role":"user","message":{"content":[{"type":"text","text":"work"}]}}`)
}

func writeIgnoredGrokUpdates(t *testing.T, home, cwd string, now time.Time) string {
	t.Helper()
	path := filepath.Join(home, ".grok", "sessions", url.PathEscape(cwd), "failed", "updates.jsonl")
	return writeIgnoredTranscript(t, path, now, `{"params":{"update":{"sessionUpdate":"error"}}}`)
}

func writeIgnoredTranscript(t *testing.T, path string, now time.Time, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	return path
}
