package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

func TestPlanTmuxMoveNamesHowEachAgentResumes(t *testing.T) {
	const codexID = "019a1b2c-3d4e-7f00-8a9b-0c1d2e3f4a5b"
	cases := []struct {
		name    string
		command string
		resume  string // "" when it stays in tmux
		leave   string
	}{
		{name: "codex", command: "codex --dangerously-bypass-approvals-and-sandbox resume " + codexID,
			resume: "codex --dangerously-bypass-approvals-and-sandbox resume " + codexID + " -c check_for_update_on_startup=false"},
		{name: "codex", command: "codex -c check_for_update_on_startup=false resume " + codexID,
			resume: "codex -c check_for_update_on_startup=false resume " + codexID},
		{name: "codex", command: "codex --dangerously-bypass-approvals-and-sandbox", leave: "its codex conversation id is unknown"},
		{name: "pi", command: "pi --session /tmp/pi/session.jsonl", resume: "pi --session /tmp/pi/session.jsonl"},
		{name: "opencode", command: "opencode -s ses_0123456789abcdefABCDEF", resume: "opencode -s ses_0123456789abcdefABCDEF"},
		{name: "pi", command: "pi", leave: "its pi session is not named in its command"},
		{name: "grok", command: "grok", leave: "Mewla cannot resume grok"},
		{name: "cursor", command: "cursor-agent", leave: "Mewla cannot resume cursor"},
		{name: "zsh", command: "zsh", leave: "Mewla cannot resume custom"},
		{name: "claude", command: "claude --session-id 11111111-1111-1111-1111-111111111111", leave: "its claude conversation cannot be found"},
		{name: "empty", command: "", leave: "no program is known"},
	}
	for _, tc := range cases {
		plan := planTmuxMove(classifier.Worker{ID: "%7", Name: tc.name, Command: tc.command})
		if plan.resume != tc.resume || plan.leave != tc.leave {
			t.Errorf("%q: resume %q, leave %q; want %q, %q", tc.command, plan.resume, plan.leave, tc.resume, tc.leave)
		}
	}
}

func TestTmuxMoveEnvironmentDropsThePane(t *testing.T) {
	got := tmuxMoveEnvironment([]string{
		"HOME=/home/u", "TMUX=/tmp/s,1,0", "TMUX_PANE=%7", "TERM=tmux-256color", "MEWLA_SESSION_ID=", "MEWLA_WORKER_ID=%7",
		"PATH=/usr/bin:/bin", "MEWLA_TMUX_GUARD_DIR=/x", "SHLVL=2", "PWD=/w", "CLAUDE_CONFIG_DIR=/c", "", "junk",
	})
	want := []string{"HOME=/home/u", "MEWLA_WORKER_ID=%7", "PATH=/usr/bin:/bin", "CLAUDE_CONFIG_DIR=/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestCommandOnPathUsesTheProgramsPath(t *testing.T) {
	dir := t.TempDir()
	if err := commandOnPath("sh -c true", []string{"PATH=/usr/bin:/bin"}, dir); err != nil {
		t.Fatal(err)
	}
	if err := commandOnPath("no-such-agent --resume x", []string{"PATH=" + dir}, dir); err == nil || !strings.Contains(err.Error(), "not on its PATH") {
		t.Fatalf("missing program: %v", err)
	}
	if err := commandOnPath("env A=1 /bin/sh -c true", nil, dir); err != nil {
		t.Fatal(err)
	}
	if got := resumedProgram("FOO=1 claude --resume x"); got != "claude" {
		t.Fatalf("program = %q", got)
	}
}

func TestOnlyAFinishedTurnIsIdle(t *testing.T) {
	idle := classifier.Worker{State: classifier.StateDone, ProcessID: 10}
	if !tmuxSessionIdle(idle) {
		t.Fatal("a finished turn is not idle")
	}
	for _, w := range []classifier.Worker{
		{State: classifier.StateRunning, ProcessID: 10},
		{State: classifier.StateBlocked, ProcessID: 10},
		{State: classifier.StateUnknown, ProcessID: 10},
		{State: classifier.StateDone, ProcessID: 10, Prompt: &classifier.Prompt{}},
		{State: classifier.StateDone},
	} {
		if tmuxSessionIdle(w) {
			t.Fatalf("%+v counted as idle", w)
		}
	}
}
