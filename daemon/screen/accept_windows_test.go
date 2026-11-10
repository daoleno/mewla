//go:build windows && !screenfake

package screen

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Windows acceptance for computer use: an agent opens Notepad, reads it
// as elements, writes a line into its document by element, reads it back,
// takes a screenshot, and a person taking over ends its control. It must run
// in the signed-in session (a logon task or the desktop), not over SSH: only
// there can a program see and use the screen.
//
//	set MEWLA_ACCEPT=windows
//	screen.test.exe -test.run AcceptWindows -test.v
//
// MEWLA_ACCEPT_OUT names a folder for the screenshot. Notepad is left open
// with the line in it, unsaved; close it by hand.
func TestAcceptWindowsComputer(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "windows" {
		t.Skip("set MEWLA_ACCEPT=windows to run the Windows computer-use acceptance")
	}
	app := os.Getenv("MEWLA_ACCEPT_APP")
	if app == "" {
		app = "Notepad"
	}
	s := NewServiceWith(t.TempDir(), newBackend())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := s.AgentReady(ctx)
	t.Logf("capability %+v, ready error %v, presence %+v", c, err, s.presence())
	if err != nil {
		t.Fatal(err)
	}
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
	shot := act(AgentAction{Action: "screenshot"})
	save := func(name string, image []byte) {
		if dir := os.Getenv("MEWLA_ACCEPT_OUT"); dir != "" && len(image) > 0 {
			_ = os.WriteFile(filepath.Join(dir, name), image, 0o600)
		}
	}
	save("windows-before.jpg", shot.Image)
	act(AgentAction{Action: "open_app", App: app})
	act(AgentAction{Action: "apps"})
	read := act(AgentAction{Action: "app_state", App: app, Interactive: true})
	field := regexp.MustCompile(`\[(\d+)\] (TextArea|TextField)`)
	m := field.FindStringSubmatch(read.Text)
	if m == nil {
		read = act(AgentAction{Action: "wait_for", App: app, Match: &ElementMatch{Role: "TextArea"}, Duration: 5})
		read = act(AgentAction{Action: "app_state", App: app, Interactive: true})
		if m = field.FindStringSubmatch(read.Text); m == nil {
			t.Fatal("no text field in the read")
		}
	}
	id, _ := strconv.Atoi(m[1])
	const line = "Hello from a Mewla agent on Windows"
	act(AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: line})
	back := act(AgentAction{Action: "element", Element: id, ElementAction: "read"})
	if !strings.Contains(back.Text, line) {
		t.Errorf("read back %q, want the line", back.Text)
	}
	shot = act(AgentAction{Action: "screenshot"})
	save("windows-notepad.jpg", shot.Image)
	// Keys and clicks go through SendInput too: a click in the app and a key.
	act(AgentAction{Action: "key", Text: "End", App: app})
	act(AgentAction{Action: "type", Text: " and keys, Shift: ABC!", App: app})
	// An app takes typed keys in its own time.
	act(AgentAction{Action: "wait", Duration: 1})
	back = act(AgentAction{Action: "element", Element: id, ElementAction: "read"})
	if !strings.Contains(back.Text, line+" and keys, Shift: ABC!") {
		t.Errorf("typed keys did not land: %q", back.Text)
	}
	shot = act(AgentAction{Action: "screenshot"})
	save("windows-notepad-typed.jpg", shot.Image)
	// A person takes over from Screen: the agent's control ends.
	control := s.TakeControl(func(error) {})
	if _, err := s.Act(ctx, worker, AgentAction{Action: "screenshot"}); err == nil {
		t.Errorf("after a takeover, an action was not refused")
	} else {
		t.Logf("after takeover: %v", err)
	}
	s.ReleaseControl(control)
}

// TestAcceptWindowsPresence watches what the person does for 25 seconds,
// as the agent hears it: key presses and clicks at the computer count;
// Mewla's own input (sent here at the start) does not, nor does it show the
// screen as locked.
//
//	set MEWLA_ACCEPT=presence
func TestAcceptWindowsPresence(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "presence" {
		t.Skip("set MEWLA_ACCEPT=presence")
	}
	b := newBackend().(*windowsBackend)
	session, err := b.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	time.Sleep(time.Second) // the hooks start
	for _, sym := range []uint32{'x', 'y'} {
		_ = session.Key(sym, true)
		_ = session.Key(sym, false)
	}
	_ = session.Pointer(10, 10, 0)
	last := b.Presence()
	t.Logf("after Mewla's own input: %+v", last)
	for time.Since(start) < 25*time.Second {
		time.Sleep(200 * time.Millisecond)
		p := b.Presence()
		if p != last {
			for k := range p.Input {
				if p.Input[k] != last.Input[k] {
					t.Logf("%5.1fs: the person's %s", time.Since(start).Seconds(), InputKind(k))
				}
			}
			if p.Locked != last.Locked {
				t.Logf("%5.1fs: locked=%v", time.Since(start).Seconds(), p.Locked)
			}
			last = p
		}
	}
}
