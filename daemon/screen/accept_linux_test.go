//go:build linux && !screenfake

package screen

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
)

// The Linux acceptance for computer use: an agent opens KWrite on its own
// display, reads it as elements, writes a line into its document by element,
// reads it back, and the person's Stop refuses the next action. It starts a
// private agent screen (Xvnc) with its own accessibility bus, and touches
// nothing of the person's.
//
//	MEWLA_ACCEPT=linux go test ./screen -run AcceptLinux -v -count=1
func TestAcceptLinuxComputer(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "linux" {
		t.Skip("set MEWLA_ACCEPT=linux to run the Linux computer-use acceptance")
	}
	app := os.Getenv("MEWLA_ACCEPT_APP")
	if app == "" {
		app = "KWrite"
	}
	if _, err := exec.LookPath(strings.ToLower(app)); err != nil {
		t.Skipf("%s is not installed", app)
	}
	displays := vdisplay.NewService("")
	defer displays.Close()
	s := NewServiceWith(t.TempDir(), platformBackend(displays))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const worker = "%accept"
	act := func(action AgentAction) AgentResult {
		t.Helper()
		started := time.Now()
		result, err := s.Act(ctx, worker, action)
		if err != nil {
			t.Fatalf("%s: %v", action.Action, err)
		}
		t.Logf("%s (%s): %s", action.Action, time.Since(started).Round(time.Millisecond), result.Text)
		return result
	}

	if c, err := s.AgentReady(ctx); err != nil || !c.Supported {
		t.Fatalf("not ready: %+v %v", c, err)
	}
	act(AgentAction{Action: "open_app", App: app})
	apps := act(AgentAction{Action: "apps"})
	if !strings.Contains(strings.ToLower(apps.Text), strings.ToLower(app)) {
		t.Fatalf("apps does not list %s", app)
	}
	read := act(AgentAction{Action: "app_state", App: app, Interactive: true})
	// The document is the one text area. KWrite opened with no file shows a
	// welcome page instead: a new document (ctrl+n) has one.
	textArea := regexp.MustCompile(`\[(\d+)\] TextArea`)
	area := textArea.FindStringSubmatch(read.Text)
	if area == nil {
		act(AgentAction{Action: "key", Text: "ctrl+n", App: app})
		read = act(AgentAction{Action: "wait_for", App: app, Match: &ElementMatch{Role: "TextArea"}, Duration: 5})
		read = act(AgentAction{Action: "app_state", App: app, Interactive: true})
		if area = textArea.FindStringSubmatch(read.Text); area == nil {
			t.Fatalf("no text area in the read")
		}
	}
	id, _ := strconv.Atoi(area[1])
	const line = "Hello from a Mewla agent on Linux"
	act(AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: line})
	back := act(AgentAction{Action: "element", Element: id, ElementAction: "read"})
	if !strings.Contains(back.Text, line) {
		t.Fatalf("read back %q, want the line", back.Text)
	}
	shot := act(AgentAction{Action: "screenshot"})
	if len(shot.Image) == 0 {
		t.Fatal("no screenshot")
	}
	if dir := os.Getenv("MEWLA_ACCEPT_OUT"); dir != "" {
		_ = os.WriteFile(dir+"/linux-agent-screen.jpg", shot.Image, 0o600)
	}
	// The person holds the display from Screen (takes control): the agent
	// waits, and goes on once they let go.
	view, err := displays.View(worker)
	if err != nil {
		t.Fatal(err)
	}
	view.Hold(true)
	if _, err := s.Act(ctx, worker, AgentAction{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "holding the display") {
		t.Fatalf("while held, an action was not refused: %v", err)
	}
	t.Logf("while held: refused as expected")
	view.Hold(false)
	view.Close()
	act(AgentAction{Action: "screenshot"})
	// The person stops the agent from Screen: its next action is refused.
	if err := displays.Halt(worker); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, worker, AgentAction{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("after Stop, an action was not refused: %v", err)
	}
	t.Logf("after Stop: refused as expected")
}
