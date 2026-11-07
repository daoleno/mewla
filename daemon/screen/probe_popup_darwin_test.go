//go:build darwin && !screenfake

package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A probe, not a test: what can an app behind the person's window be asked to
// do without the person noticing? Two of the Cua Driver Bench tasks (a team and
// a priority chosen from pop-up buttons, a drag) fail in the background today
// because the person's typing is blocked while a pop-up's menu is open, and a
// drag has to go through the real pointer. This asks the real system, with a
// small app of its own (testdata/axprobe) behind a TextEdit in which a scripted
// person types the whole time:
//
//	MEWLA_AX_PROBE=build  go test ./screen -run AXProbe -v -count=1   compile the helper app only; no screen
//	MEWLA_AX_PROBE=run    go test ./screen -run AXProbe -v -count=1   the probe, 2-3 minutes of the screen
//
// "run" waits while the screen is locked (up to MEWLA_AX_PROBE_WAIT, default 45m)
// and for the Mac to have had 20 s without input, then runs by itself and exits.
// It starts the helper app and one TextEdit on a scratch file and quits both
// by pid. It touches no other app. The report goes to stdout and to
// $MEWLA_AX_PROBE_OUT (default $TMPDIR/mewla-axprobe-report.txt).

func buildAXProbe(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "axprobe")
	cmd := exec.Command("/usr/bin/xcrun", "swiftc", "-O", "-swift-version", "5", "-o", bin, filepath.Join("testdata", "axprobe", "main.swift"))
	cmd.Env = os.Environ()
	if os.Getenv("SDKROOT") == "" {
		if sdks, _ := filepath.Glob("/Library/Developer/CommandLineTools/SDKs/MacOSX26*.sdk"); len(sdks) > 0 {
			cmd.Env = append(cmd.Env, "SDKROOT="+sdks[len(sdks)-1], "DEVELOPER_DIR=/Library/Developer/CommandLineTools")
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	return bin
}

// probeLog is the helper app's own log.
type probeLog struct{ path string }

func (l probeLog) events(since int64) []map[string]any {
	raw, _ := os.ReadFile(l.path)
	var out []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) == nil {
			if t, _ := e["t"].(float64); int64(t) >= since {
				out = append(out, e)
			}
		}
	}
	return out
}

func (l probeLog) summary(since int64) string {
	counts := map[string]int{}
	var order []string
	var last map[string]any
	for _, e := range l.events(since) {
		key := fmt.Sprint(e["ev"])
		if n, ok := e["name"]; ok {
			key += ":" + strings.TrimSuffix(strings.TrimPrefix(fmt.Sprint(n), "NS"), "Notification")
		}
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
		if e["ev"] == "up" || e["ev"] == "picked" {
			last = e
		}
	}
	var parts []string
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s×%d", k, counts[k]))
	}
	s := strings.Join(parts, " ")
	if last != nil {
		s += fmt.Sprintf(" last=%v", last)
	}
	if s == "" {
		return "nothing reached the app"
	}
	return s
}

// axChildren lists the AX children of ref, with their roles and titles; the
// returned func releases them.
func axChildren(ref uintptr) (kids []uintptr, roles, titles []string, release func()) {
	list, code := axCopy(ref, "AXChildren")
	if code != 0 || list == 0 {
		return nil, nil, nil, func() {}
	}
	for i, n := int64(0), axLib.arrayCount(list); i < n; i++ {
		kid := axLib.arrayAt(list, i)
		kids = append(kids, kid)
		roles = append(roles, axString(kid, "AXRole"))
		titles = append(titles, axString(kid, "AXTitle"))
	}
	return kids, roles, titles, func() { axLib.release(list) }
}

func axDump(ref uintptr, depth, max int, out *[]string) {
	kids, roles, titles, release := axChildren(ref)
	defer release()
	for i, kid := range kids {
		*out = append(*out, fmt.Sprintf("%s%s %q", strings.Repeat("  ", depth), roles[i], titles[i]))
		if depth+1 < max {
			axDump(kid, depth+1, max, out)
		}
	}
}

func axRawAction(ref uintptr, action string) int32 {
	name := axLib.createString(0, action, cfStringEncodingUTF8)
	if name == 0 {
		return -1
	}
	defer axLib.release(name)
	return appsLib.performAction(ref, name)
}

// menuItemOf finds the item titled title in the pop-up's menu, wherever the
// menu hangs in the pop-up's children, and runs fn on it inside the pool.
func menuItemOf(popup uintptr, title string, fn func(item uintptr, menu uintptr)) bool {
	found := false
	withPool(func() {
		kids, roles, _, release := axChildren(popup)
		defer release()
		for i, kid := range kids {
			if roles[i] != "AXMenu" {
				continue
			}
			items, itemRoles, itemTitles, releaseItems := axChildren(kid)
			for j, item := range items {
				if itemRoles[j] == "AXMenuItem" && itemTitles[j] == title {
					fn(item, kid)
					found = true
				}
			}
			releaseItems()
		}
	})
	return found
}

func TestAXProbe(t *testing.T) {
	mode := os.Getenv("MEWLA_AX_PROBE")
	if mode != "build" && mode != "run" {
		t.Skip("set MEWLA_AX_PROBE=build (compile the helper app) or run (the probe, 2-3 minutes of the screen)")
	}
	dir := t.TempDir()
	bin := buildAXProbe(t, dir)
	fmt.Println("helper app built:", bin)
	if mode == "build" {
		return
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	report := &strings.Builder{}
	say := func(format string, args ...any) {
		line := fmt.Sprintf("%s "+format, append([]any{time.Now().Format("15:04:05.000")}, args...)...)
		fmt.Println(line)
		report.WriteString(line + "\n")
	}
	defer func() {
		out := os.Getenv("MEWLA_AX_PROBE_OUT")
		if out == "" {
			out = filepath.Join(os.TempDir(), "mewla-axprobe-report.txt")
		}
		_ = os.WriteFile(out, []byte(report.String()), 0o600)
		fmt.Println("report written to", out)
	}()

	backend := newBackend()
	wait, err := time.ParseDuration(os.Getenv("MEWLA_AX_PROBE_WAIT"))
	if err != nil || wait <= 0 {
		wait = 45 * time.Minute
	}
	for deadline, last := time.Now().Add(wait), time.Now(); ; time.Sleep(3 * time.Second) {
		if reporter, ok := backend.(presenceReporter); !ok || !reporter.Presence().Locked {
			break
		}
		if time.Since(last) > time.Minute {
			say("the screen is locked; waiting for it to be unlocked")
			last = time.Now()
		}
		if time.Now().After(deadline) {
			say("the screen stayed locked for %s: nothing was run", wait)
			t.Skip("locked")
		}
	}
	t.Setenv("MEWLA_ACCEPT_WAIT_IDLE", wait.String())
	if idle, ok := waitForIdle(20); !ok {
		say("the Mac was used %.0f s ago: nothing was run", idle)
		t.Skip("in use")
	}

	say("macOS %s", productVersion())
	probe := probeLog{path: filepath.Join(dir, "lab.jsonl")}
	lab := exec.Command(bin, probe.path)
	if err := lab.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lab.Process.Signal(syscall.SIGTERM) }()
	var handle image.Point
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		for _, e := range probe.events(0) {
			if e["ev"] == "layout" {
				handle = image.Pt(int(e["handleX"].(float64)), int(e["handleY"].(float64)))
			}
		}
		if handle != (image.Point{}) {
			break
		}
	}
	if handle == (image.Point{}) {
		t.Fatal("the helper app never reported its layout")
	}
	labPid := lab.Process.Pid
	person := launchTextEdit(t, dir, "person.txt", "")
	defer person.quit()
	if front := workspaceFrontPID(); front != person.pid {
		t.Fatalf("the person's TextEdit (%d) is not in front (%d): someone is using the Mac; nothing was typed", person.pid, front)
	}
	say("helper app pid %d (drag handle at %v), person's TextEdit pid %d in front", labPid, handle, person.pid)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	mb := backend.(*macBackend)
	clicker := session.(*macSession)
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	personArea := textAreaOf(t, mb, person.pid)

	labElements := func() (popup *Element, selected string, moved string) {
		tree, err := mb.ReadApp(ctx, labPid, 500)
		if err != nil {
			say("read lab: %v", err)
			return nil, "", ""
		}
		for i := range tree.Elements {
			e := tree.Elements[i]
			switch {
			case e.Role == "AXPopUpButton" && popup == nil:
				c := tree.Elements[i]
				popup = &c
			case e.Role == "AXStaticText" && strings.HasPrefix(e.Value, "Selected:"):
				selected = e.Value
			case e.Role == "AXStaticText" && strings.HasPrefix(e.Value, "Handle:"):
				moved = e.Value
			}
		}
		return popup, selected, moved
	}

	typed := "The quick brown fox jumps over the lazy dog 0123456789 and then it goes on typing for a while"
	// scenario runs act while the scripted person types, and says what each side saw.
	scenario := func(name string, act func() string) {
		say("---- %s", name)
		_ = mb.ActOnElement(ctx, *personArea, "set_value", "")
		time.Sleep(300 * time.Millisecond)
		parked := cgPoint{900, 600}
		parkPointer(source, parked)
		typist := &personTyping{text: typed, pointer: parked, pid: person.pid, done: make(chan struct{})}
		typist.read = func() string {
			var v string
			withPool(func() { v = elementValue(mb, person.pid, personArea) })
			return v
		}
		since := time.Now().UnixMilli()
		watch := sampleInput(parked)
		go typist.run(source, 40*time.Millisecond)
		time.Sleep(400 * time.Millisecond)
		result := act()
		<-typist.done
		time.Sleep(300 * time.Millisecond)
		seen, moves, _ := watch.finish()
		var got string
		withPool(func() { got = elementValue(mb, person.pid, personArea) })
		_, selected, moved := labElements()
		say("ACTION   %s", result)
		say("LAB      %s | %s | app saw: %s", selected, moved, probe.summary(since))
		say("PERSON   typing intact: %v (typed %d of %d characters); stopped early: %q", got == typed, len(got), len(typed), typist.aborted)
		say("POINTER  samples off the parked point: %d; apps seen in front: %v (person %d)", moves, seen, person.pid)
		// A menu left open or a lost key window must not carry into the next scenario.
		time.Sleep(500 * time.Millisecond)
	}
	closeMenus := func() {
		if p, _, _ := labElements(); p != nil {
			withPool(func() {
				kids, roles, _, release := axChildren(p.ref)
				defer release()
				for i, kid := range kids {
					if roles[i] == "AXMenu" {
						axRawAction(kid, "AXCancel")
					}
				}
			})
		}
	}

	scenario("0. control: the person types, nothing else", func() string { return "nothing" })

	popup, selected, _ := labElements()
	if popup == nil {
		t.Fatalf("no pop-up button in the helper app (selected %q)", selected)
	}
	var tree []string
	withPool(func() { axDump(popup.ref, 0, 3, &tree) })
	say("---- 1. the closed pop-up button's own AX subtree (%s, actions %v)", popup.Title, popup.Actions)
	if len(tree) == 0 {
		say("no AX children while the menu is closed")
	}
	for _, line := range tree {
		say("   %s", line)
	}

	scenario("2. AXPress on the CLOSED menu's item Beta (the menu is never shown)", func() string {
		popup, _, _ := labElements()
		var code int32 = -2
		if !menuItemOf(popup.ref, "Beta", func(item, _ uintptr) { code = axRawAction(item, "AXPress") }) {
			return "the closed pop-up has no menu item Beta to press"
		}
		return fmt.Sprintf("AXPress on menu item Beta: AX error %d", code)
	})
	scenario("3. AXPick on the CLOSED menu's item Gamma", func() string {
		popup, _, _ := labElements()
		var code int32 = -2
		if !menuItemOf(popup.ref, "Gamma", func(item, _ uintptr) { code = axRawAction(item, "AXPick") }) {
			return "the closed pop-up has no menu item Gamma to pick"
		}
		return fmt.Sprintf("AXPick on menu item Gamma: AX error %d", code)
	})
	scenario("4. set_value Alpha on the pop-up button itself", func() string {
		popup, _, _ := labElements()
		return fmt.Sprintf("set_value: %v", mb.ActOnElement(ctx, *popup, "set_value", "Alpha"))
	})
	scenario("5. the real path today: AXPress on the pop-up (opens its menu), 1.5 s, then close it", func() string {
		popup, _, _ := labElements()
		err := mb.ActOnElement(ctx, *popup, "press", "")
		time.Sleep(1500 * time.Millisecond)
		var tree []string
		withPool(func() { axDump(popup.ref, 0, 2, &tree) })
		closeMenus()
		return fmt.Sprintf("press: %v; subtree while open: %s", err, strings.Join(tree, " / "))
	})
	closeMenus()

	for _, drag := range []struct {
		name string
		how  SwipeHow
	}{{"6. background drag with the existing SwipeTo(drag) (iPhone Mirroring's own route), handle +160,+40", SwipeDrag}} {
		scenario(drag.name, func() string {
			window := windowOf(t, mb, labPid)
			err := clicker.SwipeTo(labPid, window, handle.X, handle.Y, 160, 40, drag.how)
			time.Sleep(500 * time.Millisecond)
			return fmt.Sprintf("SwipeTo from %v by (160,40): %v (window %d at %v)", handle, err, window.ID, window.Bounds)
		})
	}

	scenario("7. control again: the person types, nothing else", func() string { return "nothing" })
	say("done")
}
