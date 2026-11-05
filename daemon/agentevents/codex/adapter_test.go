package codex

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/codextrust"
	"github.com/daoleno/mewla/daemon/shellquote"
)

func TestAdapterDeclaresStructuredEverything(t *testing.T) {
	d := Adapter{}.Descriptor()
	for _, c := range []agentevents.Capability{agentevents.CapBinding, agentevents.CapEvents, agentevents.CapSend, agentevents.CapApprove, agentevents.CapInterrupt} {
		if d.Level(c) != agentevents.LevelStructured {
			t.Errorf("%s = %s", c, d.Level(c))
		}
	}
}

func TestPrepareLaunchRunsTheTUIAgainstItsOwnAppServer(t *testing.T) {
	a := Adapter{ControlDir: "/state/codex-ctl"}
	launch, err := a.PrepareLaunch("codex --dangerously-bypass-approvals-and-sandbox -m gpt-6")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(launch.Control, "/state/codex-ctl/codex-ctl-") || !strings.HasSuffix(launch.Control, ".sock") {
		t.Fatalf("control = %q", launch.Control)
	}
	for _, want := range []string{
		"codex app-server --listen unix://" + launch.Control,
		"exec codex --remote unix://" + launch.Control + " --dangerously-bypass-approvals-and-sandbox -m gpt-6",
		"echo $! > " + launch.Control + ".pid",
	} {
		if !strings.Contains(launch.Command, want) {
			t.Errorf("command lacks %q:\n%s", want, launch.Command)
		}
	}
	other, _ := a.PrepareLaunch("codex")
	if other.Control == launch.Control {
		t.Fatal("two launches share a control socket")
	}
}

func TestPrepareLaunchLeavesWhatItCannotSafelyWrap(t *testing.T) {
	a := Adapter{ControlDir: "/state/codex-ctl"}
	for _, command := range []string{
		"codex exec 'fix it'",
		"codex resume abc",
		"codex --remote unix:///x.sock",
		"codex app-server --listen unix:///x.sock",
		"env FOO=1 codex",
		"codex -c 'x=1'; rm -rf y",
		"claude",
		"my-codex-proxy",
		"",
	} {
		launch, err := a.PrepareLaunch(command)
		if err != nil || launch.Command != command || launch.Control != "" {
			t.Errorf("%q was rewritten: %+v %v", command, launch, err)
		}
	}
	if launch, _ := (Adapter{}).PrepareLaunch("codex"); launch.Control != "" || launch.Command != "codex" {
		t.Fatalf("no control directory, yet %+v", launch)
	}
}

// The app-server loads the folder's own config, so it must carry the launch's
// folder trust; the same line also has to survive a quoted path that holds
// shell syntax.
func TestPrepareLaunchGivesTheAppServerTheFolderTrust(t *testing.T) {
	a := Adapter{ControlDir: "/state/codex-ctl"}
	dir := `/work/it's "odd"; $(x)`
	command := "codex --dangerously-bypass-hook-trust -c check_for_update_on_startup=false " + codextrust.Flag(dir)
	launch, err := a.PrepareLaunch(command)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Control == "" {
		t.Fatalf("a launch with a quoted path was not wrapped: %s", launch.Command)
	}
	server, _, ok := strings.Cut(launch.Command, " > ")
	if !ok || !strings.HasSuffix(server, " "+codextrust.Flag(dir)) {
		t.Fatalf("app-server lacks the trust:\n%s", launch.Command)
	}
	if strings.Count(launch.Command, codextrust.Flag(dir)) != 2 {
		t.Fatalf("trust should be on the app-server and the TUI:\n%s", launch.Command)
	}
	if strings.Contains(server, "check_for_update_on_startup") {
		t.Fatalf("only the trust is forwarded:\n%s", server)
	}
}

// The app-server starts the MCP servers, so the tools added to a launch must
// reach it, not only the TUI.
func TestPrepareLaunchGivesTheAppServerTheMCPTools(t *testing.T) {
	a := Adapter{ControlDir: "/state/codex-ctl"}
	chrome := `mcp_servers.mewla_chrome={command="/bin/mewla",args=["agent-use","chrome-mcp"],enabled=true}`
	off := `mcp_servers.computer-use.enabled=false`
	launch, err := a.PrepareLaunch("codex --model m -c " + shellquote.Word(chrome) + " -c " + shellquote.Word(off) + " -c 'model_reasoning_effort=\"high\"'")
	if err != nil || launch.Control == "" {
		t.Fatalf("not wrapped: %+v %v", launch, err)
	}
	server, _, _ := strings.Cut(launch.Command, " > ")
	for _, want := range []string{chrome, off} {
		if !strings.Contains(server, shellquote.Word(want)) {
			t.Errorf("app-server lacks %q:\n%s", want, server)
		}
	}
	if strings.Contains(server, "model_reasoning_effort") {
		t.Fatalf("only trust and MCP tools are forwarded:\n%s", server)
	}
	if strings.Count(launch.Command, shellquote.Word(chrome)) != 2 {
		t.Fatalf("tool should be on the app-server and the TUI:\n%s", launch.Command)
	}
}
