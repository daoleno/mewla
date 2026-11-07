//go:build darwin && !screenfake

package screen

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// The person's input is told to the agent instead of pausing it, on a real Mac
// and with no human at it: the "person" is this process posting real HID events
// (the way TestAcceptProduct's scripted typist does), and the Watch tap goes
// through Service.WatchWorker, the call /screen/viewer?watch=1 makes for a
// phone, speaking RFB to it as the phone's viewer does.
//
//	MEWLA_ACCEPT=told go test ./screen -run AcceptTold -v -count=1
//
// Uses the Mac for about a minute; needs 20 s without input first.
func TestAcceptTold(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "told" {
		t.Skip("set MEWLA_ACCEPT=told (uses the Mac for about a minute)")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	if idle, ok := waitForIdle(20); !ok {
		t.Skipf("the Mac has been used %.0f s ago; this run needs 20 s of no input", idle)
	}
	failures := 0
	check := func(ok bool, format string, args ...any) {
		status := "PASS"
		if !ok {
			status, failures = "FAIL", failures+1
		}
		say("%s  %s", status, fmt.Sprintf(format, args...))
	}
	dir := t.TempDir()
	app := launchTextEdit(t, dir, "front.txt", "")
	defer app.quit()
	if front := workspaceFrontPID(); front != app.pid {
		t.Fatalf("TextEdit (%d) is not in front (%d): someone is using the Mac", app.pid, front)
	}
	backend := newBackend()
	mb := backend.(*macBackend)
	area := textAreaOf(t, mb, app.pid)
	text := func() string {
		var v string
		withPool(func() { v = elementValue(mb, app.pid, area) })
		return v
	}
	svc := NewServiceWith(dir, backend)
	svc.SetAgentGate(func() bool { return true })
	const worker, device = "%told", "accept-device"
	// The device is approved in this test's own state directory: nothing of the
	// computer's real configuration changes.
	if err := svc.update(func(c *Config) error {
		c.Devices = map[string]Approval{device: {Name: "Acceptance viewer", ApprovedAt: time.Now().UTC()}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	act := func(a AgentAction) (AgentResult, error) {
		c, done := context.WithTimeout(ctx, 40*time.Second)
		defer done()
		return svc.Act(c, worker, a)
	}
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	personKey := func(r rune) {
		key := macANSIKeys[r]
		for _, down := range []bool{true, false} {
			if ev := mac.CGEventCreateKeyboardEvent(source, key.code, down); ev != 0 {
				mac.CGEventPost(cgHIDEventTap, ev)
				mac.CFRelease(ev)
			}
		}
	}

	// 1. The agent types on the screen itself, into the TextEdit in front.
	_, err := act(AgentAction{Action: "screenshot"})
	check(err == nil, "the agent has the screen: %v", err)
	r, err := act(AgentAction{Action: "type", Text: "Hello "})
	check(err == nil && strings.Contains(text(), "Hello"), "the agent typed into the front app: err=%v text=%q", err, text())
	check(!strings.Contains(r.Text, "person used"), "nothing to tell yet: %q", r.Text)

	// 2. The person types a real key at the computer: the agent is not paused or
	// refused, and its next answer tells it once.
	time.Sleep(personMargin + 100*time.Millisecond)
	personKey('p')
	started := time.Now()
	r, err = act(AgentAction{Action: "type", Text: "again "})
	check(err == nil, "a screen action right after the person's key goes on: %v", err)
	check(time.Since(started) < 5*time.Second, "and does not wait for them: %v", time.Since(started).Round(10*time.Millisecond))
	check(strings.Contains(r.Text, "person used") && strings.Contains(r.Text, "key press"), "its answer tells it: %q", r.Text)
	r, err = act(AgentAction{Action: "screenshot"})
	check(err == nil && !strings.Contains(r.Text, "person used"), "and only once: %q", r.Text)

	// 3. A key hold cut by the person: the keys come up, the answer says so.
	done := make(chan AgentResult, 1)
	go func() {
		res, _ := act(AgentAction{Action: "hold_key", Text: "shift", Duration: 6})
		done <- res
	}()
	time.Sleep(1500 * time.Millisecond)
	personKey('q')
	select {
	case res := <-done:
		check(strings.Contains(res.Text, "cut short") && strings.Contains(res.Text, "key hold was interrupted"), "the hold was cut and the agent told: %q", res.Text)
	case <-time.After(10 * time.Second):
		check(false, "the hold did not end when the person used the keyboard")
	}
	time.Sleep(personMargin + 100*time.Millisecond)
	_, err = act(AgentAction{Action: "type", Text: "ok "})
	check(err == nil, "the agent goes on afterwards: %v", err)
	check(strings.Contains(text(), "ok"), "with shift released (lower case lands): %q", text())

	// 4. The Watch tap, through the daemon's Watch path.
	time.Sleep(personMargin + 100*time.Millisecond)
	conn, err := svc.WatchWorker(ctx, device, worker, nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer conn.Close()
	read := func(n int) []byte {
		b := make([]byte, n)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, err := io.ReadFull(conn, b); err != nil {
			t.Fatalf("viewer read: %v", err)
		}
		return b
	}
	write := func(b []byte) {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write(b); err != nil {
			t.Fatalf("viewer write: %v", err)
		}
	}
	write([]byte{1}) // ClientInit
	init := read(24)
	read(int(binary.BigEndian.Uint32(init[20:])))
	write([]byte{3, 0, 0, 0, 0, 0, 0x04, 0, 0x04, 0}) // FramebufferUpdateRequest
	at := area.Frame.Min.Add(image.Pt(10, 10))
	pointer := func(buttons byte) {
		b := []byte{5, buttons, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(b[2:], uint16(at.X))
		binary.BigEndian.PutUint16(b[4:], uint16(at.Y))
		write(b)
	}
	pointer(0)
	pointer(1)
	pointer(0)
	for _, down := range []byte{1, 0} {
		b := []byte{4, down, 0, 0, 0, 0, 0, 'W'}
		write(b)
	}
	time.Sleep(700 * time.Millisecond)
	check(strings.ContainsAny(text(), "wW"), "the Watch tap and key reached the app: %q", text())
	if _, ok := svc.Agent(); !ok {
		check(false, "watching ended the agent's control")
	}
	r, err = act(AgentAction{Action: "screenshot"})
	check(err == nil && strings.Contains(r.Text, "person used") && strings.Contains(r.Text, "click"), "the agent is told what the watcher did: err=%v %q", err, r.Text)
	say("final text: %q", text())
	if failures > 0 {
		t.Fatalf("%d checks failed", failures)
	}
}

// The scripted person for a real Worker: a separate process that, once the file
// MEWLA_ACCEPT_CLICK_FLAG exists, posts real clicks into the front TextEdit's
// text area (MEWLA_ACCEPT_CLICKS of them, MEWLA_ACCEPT_CLICK_GAP apart) and logs
// when to MEWLA_ACCEPT_CLICK_LOG, so the Worker's own words can be matched
// against them.
//
//	MEWLA_ACCEPT=personclick MEWLA_ACCEPT_CLICK_FLAG=/tmp/go MEWLA_ACCEPT_CLICK_LOG=/tmp/clicks.log \
//	  go test ./screen -run AcceptPersonClick -v -count=1 -timeout 15m
func TestAcceptPersonClick(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "personclick" {
		t.Skip("set MEWLA_ACCEPT=personclick")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	flag, logPath := os.Getenv("MEWLA_ACCEPT_CLICK_FLAG"), os.Getenv("MEWLA_ACCEPT_CLICK_LOG")
	if flag == "" || logPath == "" {
		t.Fatal("MEWLA_ACCEPT_CLICK_FLAG and MEWLA_ACCEPT_CLICK_LOG are required")
	}
	clicks, gap := 2, 12*time.Second
	if v := os.Getenv("MEWLA_ACCEPT_CLICKS"); v != "" {
		fmt.Sscan(v, &clicks)
	}
	if d, err := time.ParseDuration(os.Getenv("MEWLA_ACCEPT_CLICK_GAP")); err == nil && d > 0 {
		gap = d
	}
	for deadline := time.Now().Add(10 * time.Minute); ; time.Sleep(200 * time.Millisecond) {
		if _, err := os.Stat(flag); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the flag never appeared")
		}
	}
	logf := func(format string, args ...any) {
		line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...) + "\n"
		fmt.Print(line)
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(line)
			f.Close()
		}
	}
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	mb := newBackend().(*macBackend)
	for i := 0; i < clicks; i++ {
		front := workspaceFrontPID()
		area := textAreaOf(t, mb, front)
		at := cgPoint{float64(area.Frame.Min.X + 12), float64(area.Frame.Min.Y + 12)}
		for _, ev := range []uintptr{
			mac.CGEventCreateMouseEvent(source, cgEventMouseMoved, at, 0),
			mac.CGEventCreateMouseEvent(source, macDownTypes[0], at, 0),
			mac.CGEventCreateMouseEvent(source, macUpTypes[0], at, 0),
		} {
			if ev != 0 {
				mac.CGEventPost(cgHIDEventTap, ev)
				mac.CFRelease(ev)
				time.Sleep(40 * time.Millisecond)
			}
		}
		logf("PERSON CLICK %d of %d in pid %d at %.0f,%.0f", i+1, clicks, front, at.X, at.Y)
		if i+1 < clicks {
			time.Sleep(gap)
		}
	}
}
