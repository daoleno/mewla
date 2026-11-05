//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The product path on a real Mac, fully automatic: an agent works through
// Service.Act (the same call mewla_computer makes) in a TextEdit that isn't in
// front while a scripted "person" types in another TextEdit that is, and
// optionally a real browser task runs at the same time (MEWLA_ACCEPT_BROWSER_CMD,
// a command that runs the browser acceptance and exits non-zero if it fails).
// Everything is checked from outside the agent's own report: the agent's
// TextEdit holds what was sent, the person's text is exactly what they typed,
// the pointer stayed, the app in front never changed, the guard refused a
// screen action while the person typed, and the windows came back.
//
//	MEWLA_ACCEPT=product [MEWLA_ACCEPT_BROWSER_CMD=...] [MEWLA_ACCEPT_SLEEP=1] \
//	  go test ./screen -run AcceptProduct -v -count=1
//
// Uses the Mac for about two minutes; takes no input but the scripted person's.
func TestAcceptProduct(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "product" {
		t.Skip("set MEWLA_ACCEPT=product (uses the Mac for about two minutes)")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	const minIdle = 20.0
	if idle, ok := waitForIdle(minIdle); !ok {
		t.Skipf("the Mac has been used %.0f s ago; this run needs %.0f s of no input", idle, minIdle)
	}
	dir := t.TempDir()
	agentApp := launchTextEdit(t, dir, "agent.txt", "")
	defer agentApp.quit()
	personApp := launchTextEdit(t, dir, "person.txt", "")
	defer personApp.quit()
	if front := workspaceFrontPID(); front != personApp.pid {
		t.Fatalf("the person's TextEdit (%d) is not in front (%d): someone is using the Mac; nothing was typed", personApp.pid, front)
	}
	say("agent TextEdit %d, person TextEdit %d, macOS %s", agentApp.pid, personApp.pid, productVersion())

	backend := newBackend()
	mb := backend.(*macBackend)
	agentArea := textAreaOf(t, mb, agentApp.pid)
	personArea := textAreaOf(t, mb, personApp.pid)
	home := windowOf(t, mb, agentApp.pid)
	homePos, _ := frontWindowPosition(agentApp.pid)
	say("agent window %d at %v (position %v)", home.ID, home.Bounds, homePos)
	displaysBefore, _ := activeDisplays()

	svc := NewServiceWith(dir, backend)
	svc.SetAgentGate(func() bool { return true })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	const worker = "%accept"
	act := func(action AgentAction) (AgentResult, error) {
		cctx, c := context.WithTimeout(ctx, 40*time.Second)
		defer c()
		return svc.Act(cctx, worker, action)
	}

	// The scripted person starts typing; the sampler watches the whole run.
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	parked := cgPoint{900, 600}
	parkPointer(source, parked)
	typed := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 4)
	typed = strings.TrimSpace(typed) // 179 keys, about 6 s
	typist := &personTyping{text: typed, pointer: parked, pid: personApp.pid, done: make(chan struct{})}
	typist.read = func() string {
		var v string
		withPool(func() { v = elementValue(mb, personApp.pid, personArea) })
		return v
	}
	watch := sampleInput(parked)
	front0 := workspaceFrontPID()
	go typist.run(source, 30*time.Millisecond)
	time.Sleep(500 * time.Millisecond)

	// A browser task at the same time, if one is given.
	var browser *exec.Cmd
	var browserOut strings.Builder
	if cmdline := os.Getenv("MEWLA_ACCEPT_BROWSER_CMD"); cmdline != "" {
		browser = exec.Command("/bin/sh", "-c", cmdline)
		browser.Stdout, browser.Stderr = &browserOut, &browserOut
		if err := browser.Start(); err != nil {
			t.Fatalf("browser task: %v", err)
		}
		say("browser task started")
	}

	failures := 0
	check := func(ok bool, format string, args ...any) {
		status := "PASS"
		if !ok {
			status, failures = "FAIL", failures+1
		}
		say("%s  %s", status, fmt.Sprintf(format, args...))
	}
	pid := fmt.Sprint(agentApp.pid)
	caret := func() string {
		var out string
		withPool(func() { out = selectedRange(agentArea.ref) })
		return out
	}
	agentText := func() string {
		var v string
		withPool(func() { v = elementValue(mb, agentApp.pid, agentArea) })
		return v
	}

	// 1. The agent reads the app and sets its text through an element.
	read, err := act(AgentAction{Action: "app_state", App: pid})
	check(err == nil && strings.Contains(read.Text, "TextArea"), "app_state of the agent's TextEdit while the person types: err=%v", err)
	var areaID int
	if err == nil {
		for _, line := range strings.Split(read.Text, "\n") {
			if strings.Contains(line, "TextArea") {
				fmt.Sscanf(strings.TrimSpace(line), "[%d]", &areaID)
			}
		}
	}
	if areaID > 0 {
		set, err := act(AgentAction{Action: "element", Element: areaID, ElementAction: "set_value", Text: "hello world, hello again"})
		check(err == nil && agentText() == "hello world, hello again", "element set_value landed (text %q): err=%v %s", agentText(), err, strings.SplitN(set.Text, "\n", 2)[0])
	}
	check(caret() == "24,0", "caret at the end of the text before the click: %s", caret())

	// 2. A click at the top left of the text, by a point on the window
	// screenshot the agent was given. The app moves to the agent display.
	area := agentArea.Frame
	local := area.Min.Sub(home.Bounds.Min).Add(image.Pt(10, 10))
	shotBefore, err := act(AgentAction{Action: "screenshot"})
	scale := 1.0
	if err == nil && shotBefore.Width > 0 {
		scale = float64(displayWidth(displaysBefore)) / float64(shotBefore.Width)
	}
	click := AgentAction{Action: "left_click", App: pid, Coordinate: []int{int(float64(local.X)/scale + 0.5), int(float64(local.Y)/scale + 0.5)}}
	res, err := act(click)
	say("click answered: err=%v %q", err, res.Text)
	check(err == nil && strings.Contains(res.Text, "agent display"), "the app moved to the agent display and the click said so")
	check(caret() == "0,0", "the click landed: caret %s", caret())
	check(strings.Contains(res.Text, "Effect seen"), "the answer says an effect was seen")
	onDisplay := mb.ParkedOnDisplay(agentApp.pid)
	check(onDisplay, "the agent's window is on the agent display now")

	// 3. Keys to the app on the agent display.
	_, err = act(AgentAction{Action: "type", App: pid, Text: "agentkeys"})
	check(err == nil && strings.HasPrefix(agentText(), "agentkeys"), "typed text landed at the caret: %q err=%v", agentText(), err)

	// 4. Its window screenshot comes from the agent display.
	win, err := act(AgentAction{Action: "screenshot", App: pid})
	check(err == nil && len(win.Image) > 1000 && strings.Contains(win.Text, "agent display"), "window screenshot of the parked app: err=%v %dx%d %d bytes", err, win.Width, win.Height, len(win.Image))

	// 5. Failure paths: nothing sent for a point outside the window; an
	// honest "no effect" for a point that does nothing.
	before := len(agentText())
	_, err = act(AgentAction{Action: "left_click", App: pid, Coordinate: []int{99999, 99999}})
	check(err != nil && strings.Contains(err.Error(), "outside") && len(agentText()) == before, "a click outside the window is refused and nothing is sent: %v", err)
	inert, err := act(AgentAction{Action: "left_click", App: pid, Coordinate: []int{int(float64(home.Bounds.Dx()) / scale / 2), 8}})
	say("click on the title bar answered: err=%v %q", err, inert.Text)
	check(err == nil && (strings.Contains(inert.Text, "No effect seen") || strings.Contains(inert.Text, "Effect not checked")), "a click that changes nothing doesn't claim success")

	// 6. The guard: a screen action while the person types is refused.
	_, err = act(AgentAction{Action: "key", Text: "a"})
	check(errors.Is(err, ErrPersonAtComputer), "a key to the screen while the person types is refused: %v", err)
	_, err = act(AgentAction{Action: "left_click", Coordinate: []int{400, 300}})
	check(errors.Is(err, ErrPersonAtComputer), "a click on the screen while the person types is refused: %v", err)

	// 7. Optional: the display sleeps and wakes.
	if os.Getenv("MEWLA_ACCEPT_SLEEP") == "1" {
		<-typist.done // the person stops first; sleeping the display is theirs to notice
		say("sleeping the display")
		_ = exec.Command("/usr/bin/pmset", "displaysleepnow").Run()
		time.Sleep(6 * time.Second)
		_ = exec.Command("/usr/bin/caffeinate", "-u", "-t", "2").Run()
		time.Sleep(3 * time.Second)
		woke, err := act(AgentAction{Action: "type", App: pid, Text: "Z"})
		say("after the wake: err=%v %q", err, woke.Text)
		check(err == nil, "an action after the display slept and woke works: %v", err)
		check(strings.Contains(agentText(), "Z"), "and its keys landed: %q", agentText())
		check(mb.ParkedOnDisplay(agentApp.pid), "the app is on the agent display again after the wake")
	}

	<-typist.done
	time.Sleep(300 * time.Millisecond)
	if browser != nil {
		done := make(chan error, 1)
		go func() { done <- browser.Wait() }()
		select {
		case err := <-done:
			check(err == nil, "the browser task finished with its effect verified: %v", err)
		case <-time.After(90 * time.Second):
			_ = browser.Process.Kill()
			check(false, "the browser task timed out")
		}
		say("browser task output:\n%s", browserOut.String())
	}
	seen, moves, last := watch.finish()
	front1 := workspaceFrontPID()

	// 8. Nobody was disturbed.
	check(typist.aborted == "", "the scripted person was never interrupted: %q", typist.aborted)
	var got string
	withPool(func() { got = elementValue(mb, personApp.pid, personArea) })
	check(got == typed, "the person's text is exactly what they typed (%d characters)", len(got))
	check(moves == 0, "the pointer never left where the person parked it (%v): %d samples off it", last, moves)
	check(len(seen) == 1 && seen[personApp.pid] > 0, "the app in front was always the person's: %v (start %d, end %d)", seen, front0, front1)

	// 9. The agent is done: the window is back and the display is gone.
	svc.StopAgent()
	var back image.Point
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		back, _ = frontWindowPosition(agentApp.pid)
		if back == homePos {
			break
		}
	}
	check(back == homePos, "the agent's window is back where it was: %v (was %v)", back, homePos)
	var after []displayInfo
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		after, _ = activeDisplays()
		if len(after) == len(displaysBefore) {
			break
		}
	}
	check(len(after) == len(displaysBefore), "the agent display is gone: %d displays before, %d now", len(displaysBefore), len(after))
	if failures > 0 {
		t.Fatalf("%d checks failed", failures)
	}
}

func displayWidth(displays []displayInfo) int {
	for _, d := range displays {
		if d.Main {
			return d.Bounds.Dx()
		}
	}
	return 1512
}
