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
	ran       []string
	installed map[string]bool // id -> installed
	failRun   bool
	brainHost string // agent Brain already uses
	brainSet  bool   // a Brain host is set, even with an empty id
	saved     []string
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
			case strings.Contains(command, "claude.ai/install.sh"):
				h.installed["claude"] = true
			case strings.Contains(command, "chatgpt.com/codex/install.sh"):
				h.installed["codex"] = true
			}
			return nil
		},
		agents: func() doctor.ExecutorsCheck {
			check := doctor.ExecutorsCheck{}
			for _, id := range []string{"claude", "codex"} {
				installed := h.installed[id]
				item := doctor.ExecutorCheck{ID: id, BinaryFound: installed, Usable: installed}
				if installed {
					item.BinaryPath = "/home/u/.local/bin/" + id
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
	ubuntuHost = hostdeps.Platform{GOOS: "linux", Pretty: "Ubuntu 24.04 LTS"}
	macHost    = hostdeps.Platform{GOOS: "darwin", Pretty: "macOS"}
)

func TestEnsureAgentInstalls(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{}}
	d, out := host.prompt("\n\n", true, ubuntuHost)
	if !d.ensureAgent() {
		t.Fatalf("agent not ready:\n%s", out)
	}
	want := []string{"curl -fsSL https://claude.ai/install.sh | bash"}
	if strings.Join(host.ran, "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", host.ran, want)
	}
	for _, line := range []string{"1. Claude Code", "2. Codex", "3. Skip for now", "Choose 1-3 [1]:", "Claude Code is ready. Brain will use it."} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
}

// Mewla for Mac's Install Codex (no agent on this Mac): Codex's official
// install, then Brain moves to Codex, which Brain was not set to. Codex asks
// for its own sign-in when Brain starts on it; Mewla runs no login.
func TestInstallAgentInstallsAndMovesBrain(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{}}
	d, out := host.prompt("\n", true, macHost)
	var moved []string
	if !d.installAgent("codex", func(id string) error { moved = append(moved, id); return nil }) {
		t.Fatalf("not installed:\n%s", out)
	}
	want := []string{"curl -fsSL https://chatgpt.com/codex/install.sh | sh"}
	if strings.Join(host.ran, "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", host.ran, want)
	}
	if strings.Join(moved, "|") != "codex" || !strings.Contains(out.String(), "Brain runs on Codex.") {
		t.Fatalf("Brain moved to %q:\n%s", moved, out)
	}
}

func TestInstallAgentOnlyMovesBrainToAnAgentAlreadyReady(t *testing.T) {
	host := &fakeHost{installed: map[string]bool{"claude": true}}
	d, out := host.prompt("", true, macHost)
	var moved []string
	if !d.installAgent("claude", func(id string) error { moved = append(moved, id); return nil }) || len(host.ran) != 0 || strings.Join(moved, "|") != "claude" {
		t.Fatalf("ran %q moved %q:\n%s", host.ran, moved, out)
	}
}

func TestInstallAgentThatFailsLeavesBrainWhereItIs(t *testing.T) {
	host := &fakeHost{installed: map[string]bool{}, failRun: true}
	d, out := host.prompt("", true, macHost)
	moved := false
	if d.installAgent("claude", func(string) error { moved = true; return nil }) || moved {
		t.Fatalf("a failed install moved Brain:\n%s", out)
	}
	if !strings.Contains(out.String(), "Claude Code is not installed. Try again later: curl -fsSL https://claude.ai/install.sh | bash") {
		t.Fatalf("output:\n%s", out)
	}
	if d.installAgent("grok", func(string) error { moved = true; return nil }) || moved || !strings.Contains(out.String(), "Mewla can't install grok on this computer.") {
		t.Fatalf("an agent without an install:\n%s", out)
	}
}

func TestEnsureAgentSkipsWhenOneIsReady(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{"codex": true}}
	d, out := host.prompt("", true, ubuntuHost)
	if !d.ensureAgent() || len(host.ran) != 0 || out.Len() != 0 {
		t.Fatalf("ready agent still prompted: ran %q\n%s", host.ran, out)
	}
}

func TestEnsureAgentSkipAndNonInteractive(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{}}
	d, _ := host.prompt("3\n", true, ubuntuHost)
	if d.ensureAgent() || len(host.ran) != 0 {
		t.Fatalf("skip ran %q", host.ran)
	}
	d, out := host.prompt("", false, macHost)
	if d.ensureAgent() || len(host.ran) != 0 {
		t.Fatalf("non-interactive ran %q", host.ran)
	}
	for _, line := range []string{"curl -fsSL https://claude.ai/install.sh | bash", "curl -fsSL https://chatgpt.com/codex/install.sh | sh"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
	if strings.Contains(out.String(), "login") {
		t.Fatalf("non-interactive output names a sign-in:\n%s", out)
	}
}

func TestCheckAllAsksWhichAgentBrainUsesWhenSeveralAreReady(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	both := map[string]bool{"codex": true, "claude": true}
	host := &fakeHost{installed: both}
	d, out := host.prompt("\n", true, ubuntuHost)
	if ready, err := d.checkAll(); !ready || err != nil {
		t.Fatalf("ready=%v err=%v\n%s", ready, err, out)
	}
	if strings.Join(host.saved, "|") != "claude" {
		t.Fatalf("Enter saved %q, want claude, the default\n%s", host.saved, out)
	}
	for _, line := range []string{
		"Claude Code, Codex · ready",
		"Which agent should Brain use?",
		"1. Claude Code",
		"2. Codex",
		"Brain uses Claude Code. Change it any time: mewla brain use codex",
	} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
	if strings.Contains(out.String(), "sign") {
		t.Fatalf("first run speaks of sign-in:\n%s", out)
	}

	host = &fakeHost{installed: both}
	d, out = host.prompt("2\n", true, ubuntuHost)
	d.checkAll()
	if strings.Join(host.saved, "|") != "codex" || !strings.Contains(out.String(), "Brain uses Codex. Change it any time: mewla brain use claude") {
		t.Fatalf("choosing 2 saved %q\n%s", host.saved, out)
	}
}

func TestCheckAllDoesNotAskWithOneReadyAgent(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{"codex": true}}
	d, out := host.prompt("", true, ubuntuHost)
	if ready, _ := d.checkAll(); !ready {
		t.Fatalf("codex not ready\n%s", out)
	}
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Codex · Brain uses it") {
		t.Fatalf("one ready agent: saved %q\n%s", host.saved, out)
	}
	if len(host.ran) != 0 {
		t.Fatalf("first run ran %q\n%s", host.ran, out)
	}
}

func TestCheckAllKeepsAChosenBrainHost(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	both := map[string]bool{"codex": true, "claude": true}
	host := &fakeHost{installed: both, brainHost: "codex"}
	d, out := host.prompt("", true, ubuntuHost)
	d.checkAll()
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Codex · Brain uses it") {
		t.Fatalf("chosen host: saved %q\n%s", host.saved, out)
	}

	// A Brain that already ran without a recorded executor keeps its agent.
	host = &fakeHost{installed: both, brainSet: true}
	d, out = host.prompt("", true, ubuntuHost)
	d.checkAll()
	if len(host.saved) != 0 || strings.Contains(out.String(), "Which agent") || !strings.Contains(out.String(), "Brain keeps its agent") {
		t.Fatalf("running Brain: saved %q\n%s", host.saved, out)
	}
}

func TestChooseBrainHostWithoutTerminalTakesTheDefault(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	host := &fakeHost{installed: map[string]bool{"codex": true, "claude": true}}
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
	host := &fakeHost{installed: map[string]bool{"codex": true, "claude": true}}
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
