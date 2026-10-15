package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

type fakeHost struct {
	ran        []string
	tmuxOK     bool
	agentState map[string]doctor.AuthState // id -> auth; missing = not installed
	failRun    bool
	brainHost  string // agent Brain already uses
	brainSet   bool   // a Brain host is set, even with an empty id
	saved      []string
}

func (h *fakeHost) prompt(input string, interactive bool, platform hostdeps.Platform) (*depsPrompt, *bytes.Buffer) {
	var out bytes.Buffer
	return &depsPrompt{
		in:          bufio.NewReader(strings.NewReader(input)),
		w:           &out,
		interactive: interactive,
		platform:    platform,
		run: func(command string) error {
			h.ran = append(h.ran, command)
			if h.failRun {
				return errors.New("exit status 1")
			}
			switch {
			case strings.Contains(command, "tmux"):
				h.tmuxOK = true
			case strings.Contains(command, "claude.ai/install.sh"):
				h.agentState["claude"] = doctor.AuthUnauthenticated
			case command == "claude auth login":
				h.agentState["claude"] = doctor.AuthAuthenticated
			}
			return nil
		},
		tmux: func() doctor.TmuxCheck {
			if h.tmuxOK {
				return doctor.TmuxCheck{Found: true, Path: "/usr/bin/tmux", Version: "3.4", Status: doctor.StatusOK}
			}
			return doctor.TmuxCheck{Status: doctor.StatusFail}
		},
		agents: func() doctor.ExecutorsCheck {
			check := doctor.ExecutorsCheck{}
			for _, id := range []string{"claude", "codex"} {
				auth, installed := h.agentState[id]
				item := doctor.ExecutorCheck{ID: id, BinaryFound: installed, Auth: auth}
				if installed {
					item.BinaryPath = "/home/u/.local/bin/" + id
					item.Usable = auth != doctor.AuthUnauthenticated
				}
				if item.Usable {
					check.UsableCount++
					if check.RecommendedHost == "" {
						check.RecommendedHost = id
					}
				}
				check.Items = append(check.Items, item)
			}
			return check
		},
		brainHost: func() (string, bool) { return h.brainHost, h.brainSet || h.brainHost != "" },
		setBrainHost: func(id string) error {
			h.saved = append(h.saved, id)
			return nil
		},
	}, &out
}

var (
	ubuntuHost = hostdeps.Platform{GOOS: "linux", Pretty: "Ubuntu 24.04 LTS", Manager: "apt", HasSudo: true}
	macHost    = hostdeps.Platform{GOOS: "darwin", Pretty: "macOS"}
)

func TestEnsureTmuxRunsExactlyTheShownCommandAndRechecks(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}}
	d, out := host.prompt("\n", true, ubuntuHost)
	if !d.ensureTmux() {
		t.Fatalf("tmux not ready:\n%s", out)
	}
	want := []string{"sudo apt update", "sudo apt install -y tmux"}
	if strings.Join(host.ran, "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", host.ran, want)
	}
	text := out.String()
	for _, line := range []string{"Install it on Ubuntu 24.04 LTS with:\n  sudo apt update && sudo apt install -y tmux", "Install tmux now? [Y/n]", "tmux 3.4 is ready."} {
		if !strings.Contains(text, line) {
			t.Fatalf("output missing %q:\n%s", line, text)
		}
	}
}

func TestEnsureTmuxOnMacWithoutBrewOffersHomebrewFirst(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}}
	d, out := host.prompt("y\n", true, macHost)
	d.ensureTmux()
	if len(host.ran) != 2 || host.ran[0] != hostdeps.HomebrewInstall || host.ran[1] != "/opt/homebrew/bin/brew install tmux" {
		t.Fatalf("ran %q", host.ran)
	}
	if strings.Contains(out.String(), "apt") {
		t.Fatalf("mac output mentions apt:\n%s", out)
	}
}

func TestEnsureTmuxWithoutTerminalOnlyPrints(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}}
	d, out := host.prompt("y\n", false, ubuntuHost)
	if d.ensureTmux() || len(host.ran) != 0 {
		t.Fatalf("non-interactive ran %q", host.ran)
	}
	if !strings.Contains(out.String(), "sudo apt update && sudo apt install -y tmux") || strings.Contains(out.String(), "[Y/n]") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestEnsureTmuxDeclinedOrClosedInputNeverRuns(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	for _, input := range []string{"n\n", ""} {
		host := &fakeHost{agentState: map[string]doctor.AuthState{}}
		d, _ := host.prompt(input, true, ubuntuHost)
		if d.ensureTmux() || len(host.ran) != 0 {
			t.Fatalf("input %q ran %q", input, host.ran)
		}
	}
}

func TestEnsureTmuxFailedInstallContinues(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}, failRun: true}
	d, out := host.prompt("\n", true, ubuntuHost)
	if d.ensureTmux() {
		t.Fatal("failed install reported ready")
	}
	if !strings.Contains(out.String(), "tmux is still missing. Run the command above") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestEnsureAgentInstallsThenSignsIn(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}}
	d, out := host.prompt("\n\n", true, ubuntuHost)
	if !d.ensureAgent() {
		t.Fatalf("agent not ready:\n%s", out)
	}
	want := []string{"curl -fsSL https://claude.ai/install.sh | bash", "claude auth login"}
	if strings.Join(host.ran, "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", host.ran, want)
	}
	for _, line := range []string{"1. Claude Code", "2. Codex", "3. Skip for now", "Choose 1-3 [1]:", "Sign in now? [Y/n]", "Claude Code is ready. Brain will use it."} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
}

func TestEnsureAgentSkipsWhenOneIsReady(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated}}
	d, out := host.prompt("", true, ubuntuHost)
	if !d.ensureAgent() || len(host.ran) != 0 || out.Len() != 0 {
		t.Fatalf("ready agent still prompted: ran %q\n%s", host.ran, out)
	}
}

func TestEnsureAgentOffersSignInForAnInstalledAgent(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{"claude": doctor.AuthUnauthenticated}}
	d, out := host.prompt("\n", true, ubuntuHost)
	if !d.ensureAgent() || strings.Join(host.ran, "|") != "claude auth login" {
		t.Fatalf("ran %q\n%s", host.ran, out)
	}
	if strings.Contains(out.String(), "Choose 1-3") {
		t.Fatalf("installed agent was offered an install menu:\n%s", out)
	}
}

func TestEnsureAgentSkipAndNonInteractive(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{agentState: map[string]doctor.AuthState{}}
	d, _ := host.prompt("3\n", true, ubuntuHost)
	if d.ensureAgent() || len(host.ran) != 0 {
		t.Fatalf("skip ran %q", host.ran)
	}
	d, out := host.prompt("", false, macHost)
	if d.ensureAgent() || len(host.ran) != 0 {
		t.Fatalf("non-interactive ran %q", host.ran)
	}
	for _, line := range []string{"curl -fsSL https://claude.ai/install.sh | bash   then: claude auth login", "curl -fsSL https://chatgpt.com/codex/install.sh | sh   then: codex login"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
}

func TestCheckAllAsksWhichAgentBrainUsesWhenSeveralAreReady(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	both := map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthAuthenticated}
	host := &fakeHost{tmuxOK: true, agentState: both}
	d, out := host.prompt("\n", true, ubuntuHost)
	if _, ready, err := d.checkAll(); !ready || err != nil {
		t.Fatalf("ready=%v err=%v\n%s", ready, err, out)
	}
	if strings.Join(host.saved, "|") != "claude" {
		t.Fatalf("Enter saved %q, want claude, the default\n%s", host.saved, out)
	}
	for _, line := range []string{
		"Claude Code, Codex · ready",
		"Which agent should Brain use?",
		"1. Claude Code   signed in",
		"2. Codex         signed in",
		"Brain uses Claude Code. Change it any time: mewla brain use codex",
	} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}

	host = &fakeHost{tmuxOK: true, agentState: both}
	d, out = host.prompt("2\n", true, ubuntuHost)
	d.checkAll()
	if strings.Join(host.saved, "|") != "codex" || !strings.Contains(out.String(), "Brain uses Codex. Change it any time: mewla brain use claude") {
		t.Fatalf("choosing 2 saved %q\n%s", host.saved, out)
	}
}

func TestCheckAllListsClaudeCodeFirstEvenWhenOnlyCodexSignInIsVerified(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{tmuxOK: true, agentState: map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthUnknown}}
	d, out := host.prompt("\n", true, ubuntuHost)
	d.checkAll()
	if strings.Join(host.saved, "|") != "claude" || !strings.Contains(out.String(), "1. Claude Code   sign-in not checked") {
		t.Fatalf("saved %q\n%s", host.saved, out)
	}
}

func TestCheckAllDoesNotAskWithOneReadyAgent(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{tmuxOK: true, agentState: map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthUnauthenticated}}
	d, out := host.prompt("", true, ubuntuHost)
	if _, ready, _ := d.checkAll(); !ready {
		t.Fatalf("codex not ready\n%s", out)
	}
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Codex · Brain uses it") {
		t.Fatalf("one ready agent: saved %q\n%s", host.saved, out)
	}
}

func TestCheckAllKeepsAChosenBrainHost(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	both := map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthAuthenticated}
	host := &fakeHost{tmuxOK: true, agentState: both, brainHost: "codex"}
	d, out := host.prompt("", true, ubuntuHost)
	d.checkAll()
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Codex · Brain uses it") {
		t.Fatalf("chosen host: saved %q\n%s", host.saved, out)
	}

	// A Brain that already ran without a recorded executor keeps its agent.
	host = &fakeHost{tmuxOK: true, agentState: both, brainSet: true}
	d, out = host.prompt("", true, ubuntuHost)
	d.checkAll()
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Brain keeps its agent") {
		t.Fatalf("running Brain: saved %q\n%s", host.saved, out)
	}
}

func TestChooseBrainHostWithoutTerminalTakesTheDefault(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{tmuxOK: true, agentState: map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthAuthenticated}}
	d, out := host.prompt("", false, ubuntuHost)
	if err := d.chooseBrainHost(readyAgents(d.agents())); err != nil {
		t.Fatal(err)
	}
	if strings.Join(host.saved, "|") != "claude" || strings.Contains(out.String(), "Which agent") ||
		!strings.Contains(out.String(), "Brain uses the first ready agent, Claude Code. Change it any time: mewla brain use codex") {
		t.Fatalf("saved %q\n%s", host.saved, out)
	}
}

func TestChooseBrainHostClosedInputSavesNothing(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{tmuxOK: true, agentState: map[string]doctor.AuthState{"codex": doctor.AuthAuthenticated, "claude": doctor.AuthAuthenticated}}
	d, out := host.prompt("", true, ubuntuHost)
	if err := d.chooseBrainHost(readyAgents(d.agents())); err != nil {
		t.Fatal(err)
	}
	if len(host.saved) != 0 || !strings.Contains(out.String(), "Choose any time: mewla brain use <claude|codex>") {
		t.Fatalf("saved %q\n%s", host.saved, out)
	}
}

func TestCurrentBrainHostReadsTheBindingBrainUseWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	if id, chosen := currentBrainHost(); id != "" || chosen {
		t.Fatalf("fresh Brain root: %q %v", id, chosen)
	}
	root, err := brain.DefaultRoot()
	if err != nil || !strings.HasPrefix(root, home) {
		t.Fatalf("brain root %q outside temp HOME (%v)", root, err)
	}
	if err := newDepsPrompt(nil, nil, false).setBrainHost("codex"); err != nil {
		t.Fatal(err)
	}
	if id, chosen := currentBrainHost(); id != "codex" || !chosen {
		t.Fatalf("after save: %q %v", id, chosen)
	}
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "grok")
	if id, chosen := currentBrainHost(); id != "grok" || !chosen {
		t.Fatalf("override: %q %v", id, chosen)
	}
}

func TestOfferBootSkipsWhenABootServiceIsInstalled(t *testing.T) {
	previous := bootGOOS
	bootGOOS = "linux"
	defer func() { bootGOOS = previous }()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if bootServiceInstalled() {
		t.Fatal("no unit yet")
	}
	unit, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte(bootManagedMarker+"\n[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	offerBoot(&depsPrompt{in: bufio.NewReader(strings.NewReader("y\n")), w: &out, interactive: true}, "", choiceRemote)
	if out.Len() != 0 {
		t.Fatalf("asked again: %q", out.String())
	}
}
