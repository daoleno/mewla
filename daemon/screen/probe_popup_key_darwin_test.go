//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A probe, not a test: does choosing from a pop-up of an app behind the
// person's window leave the person's window key? Cua Driver Bench counts any
// didResignKey of the person's window as a disturbance, and a pop-up's menu
// takes the keyboard from it for as long as the menu is open. Two copies of
// testdata/axprobe run, one as the target (with a pop-up button) behind and
// one as the person's own app in front; the person's window's key and active
// events are counted, and for how long it was not key, while the target's pop-up
// is chosen from in each way:
//
//	old      the path before: press the pop-up, hold the menu 1.5 s (a model's turn), cancel
//	one      choose in one step, no guard
//	drop     one step, guard that drops the person's lost-key notice
//	release  one step, guard that also hands the keyboard back by theft id
//	oldrel   the old path under the release guard
//
//	MEWLA_POPUP_PROBE=build  go test ./screen -run PopupKeyProbe -v -count=1   compile only; no screen
//	MEWLA_POPUP_PROBE=run    go test ./screen -run PopupKeyProbe -v -count=1   about two minutes of the screen
//
// It waits while the screen is locked and for 20 s without input, starts the two
// helper apps and quits them by pid, touches no other app, and ends with
//
//	CHOSEN: <variant>   the first guard variant (drop, release) that left the person's window key in
//	                    every repeat and chose the item every time, or "none"
//
// It is only valid if the unguarded one-step path does make the person's window resign; if not
// it says so and chooses none. The report also goes to $MEWLA_POPUP_PROBE_OUT.
func TestPopupKeyProbe(t *testing.T) {
	mode := os.Getenv("MEWLA_POPUP_PROBE")
	if mode != "build" && mode != "run" {
		t.Skip("set MEWLA_POPUP_PROBE=build (compile the helper app) or run (the probe, about two minutes of the screen)")
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
		out := os.Getenv("MEWLA_POPUP_PROBE_OUT")
		if out == "" {
			out = filepath.Join(os.TempDir(), "mewla-popupkeyprobe-report.txt")
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

	targetLog := probeLog{path: filepath.Join(dir, "target.jsonl")}
	target := exec.Command(bin, targetLog.path)
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Process.Signal(syscall.SIGTERM) }()
	waitLayout := func(l probeLog) bool {
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			for _, e := range l.events(0) {
				if e["ev"] == "layout" {
					return true
				}
			}
		}
		return false
	}
	if !waitLayout(targetLog) {
		t.Fatal("the target app never reported its layout")
	}
	time.Sleep(700 * time.Millisecond)
	personLog := probeLog{path: filepath.Join(dir, "person.jsonl")}
	person := exec.Command(bin, personLog.path, "person")
	if err := person.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = person.Process.Signal(syscall.SIGTERM) }()
	if !waitLayout(personLog) {
		t.Fatal("the person's app never reported its layout")
	}
	time.Sleep(700 * time.Millisecond)
	personPid, targetPid := person.Process.Pid, target.Process.Pid
	if front := workspaceFrontPID(); front != personPid {
		t.Fatalf("the person's app (%d) is not in front (%d): someone is using the Mac; nothing was chosen", personPid, front)
	}
	say("target pid %d, person's app pid %d in front", targetPid, personPid)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	mb := backend.(*macBackend)
	// Load every symbol the variants need, then switch variants at will.
	keyGuardVariant = "release"
	if why := loadKeyGuard(); why != "" {
		say("the guard cannot be loaded here: %s", why)
	}

	popupOf := func() *Element {
		tree, err := mb.ReadApp(ctx, targetPid, 500)
		if err != nil {
			say("read target: %v", err)
			return nil
		}
		for i := range tree.Elements {
			if tree.Elements[i].Role == "AXPopUpButton" {
				e := tree.Elements[i]
				return &e
			}
		}
		return nil
	}
	closeMenu := func() {
		if p := popupOf(); p != nil {
			withPool(func() { cancelPopupMenus(p.ref) })
		}
	}
	// notKey is how long the person's window was not key since `since`, from its own log.
	notKey := func(since int64) (resigns int, ms int64, resignActive int) {
		var lost int64
		for _, e := range personLog.events(since) {
			name := fmt.Sprint(e["name"])
			t, _ := e["t"].(float64)
			switch {
			case e["ev"] == "window" && strings.Contains(name, "ResignKey"):
				resigns++
				lost = int64(t)
			case e["ev"] == "window" && strings.Contains(name, "BecomeKey") && lost != 0:
				ms += int64(t) - lost
				lost = 0
			case e["ev"] == "app" && strings.Contains(name, "ResignActive"):
				resignActive++
			}
		}
		return
	}
	targetCount := func(since int64, name string) int {
		n := 0
		for _, e := range targetLog.events(since) {
			if (e["ev"] == "window" || e["ev"] == "app") && strings.Contains(fmt.Sprint(e["name"]), name) {
				n++
			}
		}
		return n
	}
	picked := func(since int64) string {
		var title string
		for _, e := range targetLog.events(since) {
			if e["ev"] == "picked" {
				title = fmt.Sprint(e["title"])
			}
		}
		return title
	}

	type outcome struct {
		resigns, resignActive int
		lostMS                int64
		activated             int
		picked, want          string
		detail                string
	}
	items := []string{"Beta", "Gamma", "Alpha"}
	run := func(name string, i int, act func(popup *Element, want string) string) outcome {
		want := items[i%len(items)]
		popup := popupOf()
		if popup == nil {
			return outcome{detail: "no pop-up"}
		}
		since := time.Now().UnixMilli()
		detail := act(popup, want)
		time.Sleep(700 * time.Millisecond)
		resigns, ms, ra := notKey(since)
		o := outcome{resigns: resigns, resignActive: ra, lostMS: ms, activated: targetCount(since, "BecomeActive"), picked: picked(since), want: want, detail: detail}
		closeMenu()
		if resigns > 0 || ra > 0 {
			_ = mb.RaiseApp(ctx, personPid)
			time.Sleep(700 * time.Millisecond)
		}
		return o
	}
	oneStep := func(variant string) func(*Element, string) string {
		return func(popup *Element, want string) string {
			keyGuardVariant = variant
			var guard KeyGuard
			if variant != "off" {
				guard = mb.GuardKey(personPid, targetPid)
			}
			choice, err := mb.ChoosePopupItem(ctx, *popup, want)
			var stats KeyGuardStats
			if guard != nil {
				stats = guard.End()
			}
			return fmt.Sprintf("one step %s: err=%v open=%v held=%d missed=%d guard=%v", variant, err, choice.Open.Round(time.Millisecond), stats.Held, stats.Missed, guard != nil)
		}
	}
	old := func(variant string) func(*Element, string) string {
		return func(popup *Element, want string) string {
			keyGuardVariant = variant
			var guard KeyGuard
			if variant != "off" {
				guard = mb.GuardKey(personPid, targetPid)
			}
			err := mb.ActOnElement(ctx, *popup, "press", "")
			time.Sleep(1500 * time.Millisecond)
			var stats KeyGuardStats
			if guard != nil {
				stats = guard.End()
			}
			return fmt.Sprintf("press and hold 1.5 s (%s): err=%v held=%d missed=%d", variant, err, stats.Held, stats.Missed)
		}
	}

	const repeats = 3
	type scenario struct {
		name string
		act  func(*Element, string) string
	}
	scenarios := []scenario{
		{"control", func(*Element, string) string { return "nothing" }},
		{"old", old("off")},
		{"one", oneStep("off")},
		{"drop", oneStep("drop")},
		{"release", oneStep("release")},
		{"oldrel", old("release")},
		{"control2", func(*Element, string) string { return "nothing" }},
	}
	clean := map[string]bool{}
	unguardedResigns := false
	for _, sc := range scenarios {
		ok := true
		var lines []string
		for i := 0; i < repeats; i++ {
			o := run(sc.name, i, sc.act)
			lines = append(lines, fmt.Sprintf("person ResignKey×%d (not key %d ms) ResignActive×%d | target BecomeActive×%d picked %q want %q | %s", o.resigns, o.lostMS, o.resignActive, o.activated, o.picked, o.want, o.detail))
			if o.resigns > 0 || o.resignActive > 0 {
				ok = false
			}
			if strings.HasPrefix(sc.name, "one") && sc.name == "one" && o.resigns > 0 {
				unguardedResigns = true
			}
			if (sc.name == "one" || sc.name == "drop" || sc.name == "release") && o.picked != o.want {
				ok = false
			}
		}
		clean[sc.name] = ok
		say("---- %s: person's window stayed key (and the item was chosen, for one-step) in all %d repeats: %v", sc.name, repeats, ok)
		for _, l := range lines {
			say("     %s", l)
		}
	}
	chosen := "none"
	switch {
	case !unguardedResigns:
		say("the unguarded one-step choice did NOT make the person's window resign here, so this probe does not reproduce the benchmark's effect: nothing is chosen")
	case clean["drop"]:
		chosen = "drop"
	case clean["release"]:
		chosen = "release"
	}
	say("CHOSEN: %s", chosen)
}
