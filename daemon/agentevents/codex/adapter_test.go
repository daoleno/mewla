package codex

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
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
