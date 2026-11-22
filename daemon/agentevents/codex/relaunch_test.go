package codex

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
)

const relaunchThread = "01a20138-c75d-7d82-8c87-1217573b1588"

// The app-server died with the TUI: a new one starts the way a fresh Worker's
// does, and the TUI resumes the same thread through it. The permission flags
// a remote resume refuses move to the server; the rest stay on the TUI.
func TestPrepareRelaunchStartsANewAppServerForTheSameThread(t *testing.T) {
	a := Adapter{ControlDir: t.TempDir(), Mewla: "/bin/mewla"}
	old := filepath.Join(t.TempDir(), "gone.sock")
	launch, err := a.PrepareRelaunch("codex resume "+relaunchThread+" --dangerously-bypass-approvals-and-sandbox --dangerously-bypass-hook-trust -m gpt-6 -c 'mcp_servers.mewla_chrome.command=\"m\"'", old)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Control == "" || launch.Control == old || !strings.HasSuffix(launch.Control, ".sock") {
		t.Fatalf("launch = %+v", launch)
	}
	server, tui, ok := strings.Cut(launch.Command, "; exec ")
	if !ok {
		t.Fatalf("no TUI exec in %q", launch.Command)
	}
	for _, want := range []string{
		"set +m; codex app-server --listen unix://" + launch.Control,
		"-c 'model=\"gpt-6\"'",
		"-c 'mcp_servers.mewla_chrome.command=\"m\"'",
		`-c 'approval_policy="never"' -c 'sandbox_mode="danger-full-access"'`,
		"echo $! > " + launch.Control + ".pid",
		"/bin/mewla codex-server-wait " + launch.Control,
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server part lacks %q:\n%s", want, server)
		}
	}
	if want := "codex resume " + relaunchThread + " --remote unix://" + launch.Control + " --dangerously-bypass-hook-trust -m gpt-6 -c 'mcp_servers.mewla_chrome.command=\"m\"'"; tui != want {
		t.Fatalf("TUI = %q\nwant  %q", tui, want)
	}
}

// The app-server outlived the TUI: it is draining its turn and holds the
// thread, so nothing is started until it is gone.
func TestPrepareRelaunchWaitsWhileTheAppServerStillRuns(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "cx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "ctl.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	a := Adapter{ControlDir: t.TempDir(), Mewla: "/bin/mewla"}
	command := "codex resume " + relaunchThread + " -a never -m gpt-6"
	if _, err := a.PrepareRelaunch(command, socket); !errors.Is(err, agentevents.ErrControlBusy) {
		t.Fatalf("while the app-server runs: %v", err)
	}
	_ = ln.Close()
	launch, err := a.PrepareRelaunch(command, socket)
	if err != nil || launch.Control == socket || !strings.Contains(launch.Command, "app-server --listen") {
		t.Fatalf("once it is gone: %+v, %v", launch, err)
	}
}

// A Model Profile route's resume (routes.ResumeLaunch) names its provider on
// the TUI; the new app-server, which calls the model, carries it.
func TestPrepareRelaunchCarriesAModelRouteToTheAppServer(t *testing.T) {
	a := Adapter{ControlDir: t.TempDir()}
	launch, err := a.PrepareRelaunch("codex resume "+relaunchThread+` --model gpt-6 -c 'model_provider="openai"' -c 'openai_base_url="http://127.0.0.1:9/r/abc/v1"'`, "")
	if err != nil {
		t.Fatal(err)
	}
	server, _, _ := strings.Cut(launch.Command, "; exec ")
	for _, want := range []string{`-c 'model="gpt-6"'`, `-c 'model_provider="openai"'`, `-c 'openai_base_url="http://127.0.0.1:9/r/abc/v1"'`} {
		if !strings.Contains(server, want) {
			t.Errorf("app-server lacks %s:\n%s", want, server)
		}
	}
}

func TestPrepareRelaunchRefusesWhatIsNotAResume(t *testing.T) {
	a := Adapter{ControlDir: t.TempDir()}
	for _, command := range []string{"codex", "codex -m gpt-6", "codex resume --last", "codex resume " + relaunchThread + " --remote unix:///x.sock", "claude --resume x"} {
		if _, err := a.PrepareRelaunch(command, ""); err == nil {
			t.Errorf("%q was relaunched", command)
		}
	}
}

func TestSplitResumeArgsMovesEveryPermissionForm(t *testing.T) {
	tui, server := splitResumeArgs("--sandbox=workspace-write --ask-for-approval on-request --full-auto -m x --add-dir=/a")
	if tui != "-m x" {
		t.Fatalf("tui = %q", tui)
	}
	for _, want := range []string{`sandbox_mode="workspace-write"`, `approval_policy="on-request"`, `sandbox_workspace_write.writable_roots=["/a"]`} {
		if !strings.Contains(server, want) {
			t.Errorf("server lacks %q: %q", want, server)
		}
	}
}
