//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Mac acceptance for background input, fully automatic: no one has to
// watch or touch the Mac. A scripted "person" types into a TextEdit in front,
// with the pointer parked at a known point, while the agent works in another
// TextEdit that is behind it (or on a virtual display). It then checks the
// agent's effect on its own app (the caret moved, the text changed) and that
// the person was not disturbed (their text is exactly what they typed, the
// pointer is where they left it, the app in front never changed).
//
//	MEWLA_ACCEPT=click  go test ./screen -run Accept -v -count=1
//	MEWLA_ACCEPT=click MEWLA_ACCEPT_VDISPLAY=1 ...   the agent's TextEdit on a virtual display
//
// It starts two TextEdit instances on scratch files under TMPDIR and quits
// them, by pid, at the end. It uses the Mac while it runs: ask first.

type textEditApp struct {
	pid  int
	file string
}

// launchTextEdit opens a new TextEdit instance on a scratch file holding text.
// The newest instance is in front afterwards.
func launchTextEdit(t *testing.T, dir, name, text string) textEditApp {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	before := textEditPIDs()
	if out, err := exec.Command("/usr/bin/open", "-n", "-a", "TextEdit", file).CombinedOutput(); err != nil {
		t.Fatalf("open TextEdit: %v %s", err, out)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		for pid := range textEditPIDs() {
			if !before[pid] {
				time.Sleep(1200 * time.Millisecond)
				return textEditApp{pid, file}
			}
		}
	}
	t.Fatal("the new TextEdit never appeared")
	return textEditApp{}
}

func textEditPIDs() map[int]bool {
	out, _ := exec.Command("/usr/bin/pgrep", "-x", "TextEdit").Output()
	pids := map[int]bool{}
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids[pid] = true
		}
	}
	return pids
}

func (a textEditApp) quit() {
	_ = exec.Command("/bin/kill", strconv.Itoa(a.pid)).Run()
}

// personTyping is the scripted person: it parks the pointer, then types a
// text at the system's keyboard, a key every few milliseconds, as a
// hardware keyboard would.
type personTyping struct {
	text    string
	pointer cgPoint
	done    chan struct{}
}

func (p *personTyping) run(source uintptr, cpm time.Duration) {
	defer close(p.done)
	for _, r := range p.text {
		key, ok := macANSIKeys[r]
		if !ok {
			continue
		}
		for _, down := range []bool{true, false} {
			ev := mac.CGEventCreateKeyboardEvent(source, key.code, down)
			if ev == 0 {
				continue
			}
			var flags uint64
			if key.shift {
				flags = macFlagShift
			}
			mac.CGEventSetFlags(ev, flags)
			mac.CGEventPost(cgHIDEventTap, ev)
			mac.CFRelease(ev)
		}
		time.Sleep(cpm)
	}
}

// parkPointer moves the pointer with a hardware-style event.
func parkPointer(source uintptr, at cgPoint) {
	ev := mac.CGEventCreateMouseEvent(source, cgEventMouseMoved, at, 0)
	mac.CGEventPost(cgHIDEventTap, ev)
	mac.CFRelease(ev)
	time.Sleep(150 * time.Millisecond)
}

// inputSampler samples which app is in front until stopped.
type inputSampler struct {
	stop   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	seen   map[int]int
	moves  int
	origin cgPoint
	lastAt cgPoint
}

func sampleInput(pointer cgPoint) *inputSampler {
	w := &inputSampler{stop: make(chan struct{}), seen: map[int]int{}, origin: pointer}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-w.stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			var front int
			var at cgPoint
			withPool(func() { front, at = workspaceFrontPID(), pointerAt() })
			w.mu.Lock()
			w.seen[front]++
			w.lastAt = at
			if at != w.origin {
				w.moves++
			}
			w.mu.Unlock()
		}
	}()
	return w
}

func (w *inputSampler) finish() (seen map[int]int, pointerMoves int, last cgPoint) {
	close(w.stop)
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen, w.moves, w.lastAt
}

func textAreaOf(t *testing.T, backend *macBackend, pid int) *Element {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tree, err := backend.ReadApp(ctx, pid, 500)
	if err != nil {
		t.Fatalf("read app %d: %v", pid, err)
	}
	for i := range tree.Elements {
		if tree.Elements[i].Role == "AXTextArea" {
			return &tree.Elements[i]
		}
	}
	t.Fatalf("app %d has no text area", pid)
	return nil
}

func windowOf(t *testing.T, backend *macBackend, pid int) AppWindow {
	t.Helper()
	list, err := backend.Apps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.PID == pid && len(a.Windows) > 0 {
			return a.Windows[0]
		}
	}
	t.Fatalf("no window of %d", pid)
	return AppWindow{}
}

func TestAcceptBackgroundClick(t *testing.T) {
	if os.Getenv("MEWLA_ACCEPT") != "click" {
		t.Skip("set MEWLA_ACCEPT=click (uses the Mac for about a minute)")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	dir := t.TempDir()
	agent := launchTextEdit(t, dir, "agent.txt", "")
	defer agent.quit()
	personApp := launchTextEdit(t, dir, "person.txt", "")
	defer personApp.quit()
	personPid := personApp.pid
	say("agent TextEdit pid %d, person TextEdit pid %d, front app %d", agent.pid, personApp.pid, workspaceFrontPID())
	say("macOS %s, window-local stamp %v", productVersion(), windowLocalStamp(productVersion()))

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var origin image.Point
	if os.Getenv("MEWLA_ACCEPT_VDISPLAY") == "1" {
		vd, err := newVirtualDisplay("Mewla Agent", 1600, 1000, 60, false)
		if err != nil {
			t.Fatalf("virtual display: %v", err)
		}
		defer vd.Close()
		time.Sleep(500 * time.Millisecond)
		displays, _ := activeDisplays()
		for _, d := range displays {
			if d.ID == vd.ID {
				origin = d.Bounds.Min
			}
		}
		say("virtual display %d at %v", vd.ID, origin)
		captureDisplayID = func() uint32 { return vd.ID }
		defer func() { captureDisplayID = func() uint32 { return mac.CGMainDisplayID() } }()
		var home image.Point
		var known bool
		withPool(func() {
			home, known = frontWindowPosition(agent.pid)
			say("agent window to the virtual display: %v", moveFrontWindow(agent.pid, origin.Add(image.Pt(40, 60))))
		})
		defer withPool(func() {
			if known {
				say("agent window back: %v", moveFrontWindow(agent.pid, home))
			}
		})
		time.Sleep(700 * time.Millisecond)
	}

	backend := newBackend()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	mb := backend.(*macBackend)
	clicker := session.(*macSession)
	if ok, why := clicker.BackgroundClicks(); !ok {
		t.Fatalf("no background clicks: %s", why)
	}

	agentArea := textAreaOf(t, mb, agent.pid)
	personArea := textAreaOf(t, mb, personApp.pid)
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)

	for _, c := range clickStyles {
		say("---- %s", c.name)
		say("agent set text: %v", mb.ActOnElement(ctx, *agentArea, "set_value", "hello world, hello again"))
		say("person set text: %v", mb.ActOnElement(ctx, *personArea, "set_value", ""))
		time.Sleep(300 * time.Millisecond)
		caret := func() string {
			var out string
			withPool(func() { out = selectedRange(agentArea.ref) })
			return out
		}
		window := windowOf(t, mb, agent.pid)
		window.Bounds = window.Bounds.Sub(origin)
		at := agentArea.Frame.Min.Add(image.Pt(10, 10)).Sub(origin)

		parked := cgPoint{900, 600}
		parkPointer(source, parked)
		typed := "the quick brown fox jumps over the lazy dog 0123456789"
		typist := &personTyping{text: typed, pointer: parked, done: make(chan struct{})}
		watch := sampleInput(parked)
		front0 := workspaceFrontPID()
		before := caret()
		go typist.run(source, 25*time.Millisecond)
		time.Sleep(300 * time.Millisecond)
		err := clicker.clickTo(agent.pid, window, at.X, at.Y, 0, 1, c.style)
		time.Sleep(500 * time.Millisecond)
		after := caret()
		<-typist.done
		time.Sleep(300 * time.Millisecond)
		seen, moves, last := watch.finish()

		var got string
		withPool(func() { got = elementValue(mb, personPid, personArea) })
		say("click error: %v", err)
		say("EFFECT   agent caret %s -> %s (moved: %v)", before, after, before != after)
		say("PERSON   typed text intact: %v (%q)", got == typed, got)
		say("POINTER  parked %v, last %v, samples off it: %d", parked, last, moves)
		say("FRONT    at start %d, apps seen in front %v (person %d)", front0, seen, personPid)
	}
}

// elementValue reads a text area's current value.
func elementValue(b *macBackend, pid int, area *Element) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tree, err := b.ReadApp(ctx, pid, 500)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	for _, e := range tree.Elements {
		if e.Role == "AXTextArea" {
			return e.Value
		}
	}
	return "no text area"
}
