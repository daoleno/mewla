//go:build darwin && !screenfake

package screen

import (
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
)

// The key-focus guard.
//
// What happens: an app that is not in front opens a menu (a pop-up button's,
// a context menu). AppKit's menu tracking asks the window server for the
// keyboard (SLPSStealKeyFocusReturningID). The window server then sends the
// app that holds it, the person's front app, a notice that it lost key focus,
// and the person's window resigns key: their typing stops landing in it until
// the menu is gone. No app was activated and no window raised, so none of the
// usual signs shows.
//
// What the guard does: for as long as Mewla acts on the app behind, a per-app
// event tap sits on the person's front app, and the lost-key-focus notice
// whose thief is the guarded app (CGEvent type 21, subtype 0x4000 in field
// 64, thief pid in field 73) is dropped before the person's app sees it.
// Nothing is moved by arming it: a theft on arming would send the person's
// keystrokes to Mewla.
//
// Two variants, chosen by MEWLA_SCREEN_KEYGUARD:
//
//	drop     only drop the notice. In a one-step pop-up choice the menu is open
//	         for tens of milliseconds and AppKit gives the keyboard back itself.
//	release  also hand the keyboard back at once, by theft id: steal on Mewla's
//	         own connection (which takes the next id), release it, release the
//	         newest live id below it, and drop the notice that lookup sends the
//	         target, which would end its menu tracking. Needed when a menu is
//	         left open across model turns.
//	off      no guard.
//
// This is cua-driver's published technique (trycua/cua, libs/cua-driver/rust/
// crates/platform-macos/src/key_focus_guard.rs, FSL-1.1-MIT), written afresh.
// The calls are private or unusual, so a macOS that lacks them only loses the
// guard.

const (
	focusNoticeType   = 21
	focusFieldSubtype = 64
	focusFieldThief   = 73
	focusLostKey      = 0x4000

	tapDisabledByTimeout = 0xFFFFFFFE
	tapDisabledByInput   = 0xFFFFFFFF

	// keyGuardTail keeps a target guarded after the action that armed it
	// ends: AppKit opens a pressed pop-up's menu just after the press returns.
	keyGuardTail = 3 * time.Second
	// lookupWindow is how long the target's tap hides Mewla's own theft.
	lookupWindow = 500 * time.Millisecond
	// idSlack are older theft ids tried after the newest one.
	idSlack = 3
	// stealOptions are the options AppKit itself passes.
	stealOptions = 0xC80
)

// keyGuardVariant is what the guard does; the probe on a Mac decides the
// default. MEWLA_SCREEN_KEYGUARD overrides it.
var keyGuardVariant = func() string {
	switch v := os.Getenv("MEWLA_SCREEN_KEYGUARD"); v {
	case "off", "drop", "release":
		return v
	}
	return "drop"
}()

var kg struct {
	once sync.Once
	why  string

	tapCreate func(pid int32, place, options uint32, mask uint64, callback, info uintptr) uintptr
	tapEnable func(port uintptr, enable bool)
	// tapSession is CGEventTapCreate, for the probe's look at everything
	// that flows through the session.
	tapSession func(tap, place, options uint32, mask uint64, callback, info uintptr) uintptr
	field      func(event uintptr, field uint32) int64

	sourceFor   func(alloc, port uintptr, order int64) uintptr
	loopCurrent func() uintptr
	loopAdd     func(loop, source, mode uintptr)
	loopRun     func()
	loopStop    func(loop uintptr)
	invalidate  func(port uintptr)
	release     func(ref uintptr)
	mode        uintptr

	steal      func(cid, options uint32, id *int32) int32
	releaseID  func(id int32) int32
	connection func() uint32

	frontCallback  uintptr
	targetCallback uintptr

	mu      sync.Mutex
	front   *keyTap
	targets map[int]*keyTap
	guarded map[int]*guardedApp
	lookup  time.Time

	held, missed atomic.Int64
}

type keyTap struct {
	pid  int
	port uintptr
	loop uintptr
}

type guardedApp struct {
	leases int
	until  time.Time
}

func loadKeyGuard() string {
	kg.once.Do(func() {
		if keyGuardVariant == "off" {
			kg.why = "the key-focus guard is turned off (MEWLA_SCREEN_KEYGUARD=off)"
			return
		}
		if !loadApps() {
			kg.why = "no accessibility"
			return
		}
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			kg.why = err.Error()
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			kg.why = err.Error()
			return
		}
		sky, err := purego.Dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			sky = 0
		}
		for _, fn := range []struct {
			ptr  any
			lib  uintptr
			name string
		}{
			{&kg.tapCreate, as, "CGEventTapCreateForPid"},
			{&kg.tapEnable, as, "CGEventTapEnable"},
			{&kg.field, as, "CGEventGetIntegerValueField"},
			{&kg.sourceFor, cf, "CFMachPortCreateRunLoopSource"},
			{&kg.loopCurrent, cf, "CFRunLoopGetCurrent"},
			{&kg.loopAdd, cf, "CFRunLoopAddSource"},
			{&kg.loopRun, cf, "CFRunLoopRun"},
			{&kg.loopStop, cf, "CFRunLoopStop"},
			{&kg.invalidate, cf, "CFMachPortInvalidate"},
			{&kg.release, cf, "CFRelease"},
		} {
			if _, err := purego.Dlsym(fn.lib, fn.name); err != nil {
				kg.why = "this macOS has no " + fn.name
				return
			}
			purego.RegisterLibFunc(fn.ptr, fn.lib, fn.name)
		}
		if _, err := purego.Dlsym(as, "CGEventTapCreate"); err == nil {
			purego.RegisterLibFunc(&kg.tapSession, as, "CGEventTapCreate")
		}
		if keyGuardVariant == "release" {
			if sky == 0 {
				kg.why = "no SkyLight to hand the keyboard back"
				return
			}
			for _, fn := range []struct {
				ptr  any
				name string
			}{
				{&kg.steal, "SLPSStealKeyFocusReturningID"},
				{&kg.releaseID, "SLPSReleaseKeyFocusWithID"},
				{&kg.connection, "SLSMainConnectionID"},
			} {
				if _, err := purego.Dlsym(sky, fn.name); err != nil {
					kg.why = "this macOS's SkyLight has no " + fn.name
					return
				}
				purego.RegisterLibFunc(fn.ptr, sky, fn.name)
			}
		}
		// A run loop mode is matched by the text of its name.
		if kg.mode = axLib.createString(0, "kCFRunLoopCommonModes", cfStringEncodingUTF8); kg.mode == 0 {
			kg.why = "no run loop mode"
			return
		}
		log.Printf("screen: key-focus guard variant %q", keyGuardVariant)
		kg.targets = map[int]*keyTap{}
		kg.guarded = map[int]*guardedApp{}
		kg.frontCallback = purego.NewCallback(frontTapCallback)
		kg.targetCallback = purego.NewCallback(targetTapCallback)
	})
	return kg.why
}

// isGuarded says whether thefts by pid are to be handed back now.
func isGuarded(pid int) bool {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	g := kg.guarded[pid]
	return g != nil && (g.leases > 0 || time.Now().Before(g.until))
}

func reenable(port uintptr) {
	if port != 0 {
		kg.tapEnable(port, true)
	}
}

// frontTapCallback runs on the tap's thread for each focus notice the
// person's app is about to get. It does no accessibility work.
func frontTapCallback(proxy, typ, event, info uintptr) uintptr {
	typ &= 0xFFFFFFFF
	if typ == tapDisabledByTimeout || typ == tapDisabledByInput {
		kg.mu.Lock()
		var port uintptr
		if kg.front != nil {
			port = kg.front.port
		}
		kg.mu.Unlock()
		reenable(port)
		return event
	}
	if event == 0 || typ != focusNoticeType || kg.field(event, focusFieldSubtype) != focusLostKey {
		return event
	}
	thief := int(kg.field(event, focusFieldThief))
	if thief == os.Getpid() || !isGuarded(thief) {
		return event
	}
	kg.held.Add(1)
	if keyGuardVariant == "release" {
		go handBackKeyboard(thief)
	}
	return 0 // the person's app never hears that it lost key
}

// targetTapCallback drops the notice Mewla's own id lookup sends the target,
// which would end its menu tracking.
func targetTapCallback(proxy, typ, event, info uintptr) uintptr {
	typ &= 0xFFFFFFFF
	if typ == tapDisabledByTimeout || typ == tapDisabledByInput {
		kg.mu.Lock()
		var ports []uintptr
		for _, t := range kg.targets {
			ports = append(ports, t.port)
		}
		kg.mu.Unlock()
		for _, p := range ports {
			reenable(p)
		}
		return event
	}
	if event == 0 || typ != focusNoticeType || kg.field(event, focusFieldSubtype) != focusLostKey {
		return event
	}
	if int(kg.field(event, focusFieldThief)) != os.Getpid() {
		return event
	}
	kg.mu.Lock()
	inLookup := time.Now().Before(kg.lookup)
	kg.mu.Unlock()
	if inLookup {
		return 0
	}
	return event
}

// handBackKeyboard releases the theft the guarded app just made. The tap has
// seen the theft already, so it is the newest live one below the id Mewla's
// own theft takes.
func handBackKeyboard(thief int) {
	kg.mu.Lock()
	kg.lookup = time.Now().Add(lookupWindow)
	kg.mu.Unlock()
	var own int32
	if kg.steal(kg.connection(), stealOptions, &own) != 0 || own == 0 {
		kg.missed.Add(1)
		return
	}
	kg.releaseID(own)
	for back := int32(1); back <= idSlack+1; back++ {
		if id := own - back; id != 0 && kg.releaseID(id) == 0 {
			return
		}
	}
	kg.missed.Add(1)
	log.Printf("screen: key-focus guard could not hand back a keyboard taken by pid %d", thief)
}

// startTap puts a tap on pid, on a run loop of its own thread.
func startTap(pid int, callback uintptr) *keyTap {
	return startTapMask(pid, uint64(1)<<focusNoticeType, false, callback)
}

// startTapMask puts a tap on pid for the event types in mask, or with
// session a listen-only tap on the whole login session (pid is then 0).
func startTapMask(pid int, mask uint64, session bool, callback uintptr) *keyTap {
	type made struct{ port, loop uintptr }
	ready := make(chan made, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// kCGHeadInsertEventTap = 0, kCGEventTapOptionDefault = 0.
		var port uintptr
		if session {
			// kCGSessionEventTap = 1, head insert, listen only = 1.
			if kg.tapSession != nil {
				port = kg.tapSession(1, 0, 1, mask, callback, 0)
			}
		} else {
			port = kg.tapCreate(int32(pid), 0, 0, mask, callback, 0)
		}
		if port == 0 {
			ready <- made{}
			return
		}
		source := kg.sourceFor(0, port, 0)
		if source == 0 {
			kg.invalidate(port)
			kg.release(port)
			ready <- made{}
			return
		}
		loop := kg.loopCurrent()
		kg.loopAdd(loop, source, kg.mode)
		kg.tapEnable(port, true)
		ready <- made{port, loop}
		kg.loopRun()
		kg.invalidate(port)
		kg.release(source)
		kg.release(port)
	}()
	select {
	case m := <-ready:
		if m.port == 0 {
			return nil
		}
		return &keyTap{pid: pid, port: m.port, loop: m.loop}
	case <-time.After(2 * time.Second):
		return nil
	}
}

func stopTap(t *keyTap) {
	if t == nil {
		return
	}
	kg.tapEnable(t.port, false)
	kg.loopStop(t.loop)
}

var keyGuardWarned atomic.Bool

type macKeyGuard struct {
	target       int
	held, missed int64
}

// GuardKey arms the guard for target against front's key window.
func (b *macBackend) GuardKey(front, target int) KeyGuard {
	if why := loadKeyGuard(); why != "" {
		if keyGuardWarned.CompareAndSwap(false, true) && keyGuardVariant != "off" {
			log.Printf("screen: no key-focus guard: %s", why)
		}
		return nil
	}
	kg.mu.Lock()
	g := kg.guarded[target]
	if g == nil {
		g = &guardedApp{}
		kg.guarded[target] = g
	}
	g.leases++
	g.until = time.Now().Add(keyGuardTail)
	needFront := kg.front == nil || kg.front.pid != front
	_, haveTarget := kg.targets[target]
	kg.mu.Unlock()

	fail := func(what string) KeyGuard {
		kg.mu.Lock()
		g.leases--
		kg.mu.Unlock()
		if keyGuardWarned.CompareAndSwap(false, true) {
			log.Printf("screen: no key-focus guard: %s", what)
		}
		return nil
	}
	if needFront {
		tap := startTap(front, kg.frontCallback)
		if tap == nil {
			return fail("the window server gave Mewla no event tap on the person's app (needs the Accessibility permission)")
		}
		kg.mu.Lock()
		old := kg.front
		kg.front = tap
		kg.mu.Unlock()
		stopTap(old)
	}
	if keyGuardVariant == "release" && !haveTarget {
		if tap := startTap(target, kg.targetCallback); tap != nil {
			kg.mu.Lock()
			old := kg.targets[target]
			kg.targets[target] = tap
			kg.mu.Unlock()
			stopTap(old)
		}
	}
	return &macKeyGuard{target: target, held: kg.held.Load(), missed: kg.missed.Load()}
}

func (g *macKeyGuard) End() KeyGuardStats {
	stats := KeyGuardStats{Held: int(kg.held.Load() - g.held), Missed: int(kg.missed.Load() - g.missed)}
	kg.mu.Lock()
	if a := kg.guarded[g.target]; a != nil {
		a.leases--
		a.until = time.Now().Add(keyGuardTail)
	}
	kg.mu.Unlock()
	time.AfterFunc(keyGuardTail+time.Second, teardownKeyGuard)
	if stats.Held > 0 {
		log.Printf("screen: key-focus guard (%s) held back %d lost-key notice(s) for pid %d, %d missed", keyGuardVariant, stats.Held, g.target, stats.Missed)
	}
	return stats
}

// teardownKeyGuard removes the taps once nothing has been guarded for a
// while, so Mewla does not sit on the person's app between tasks.
func teardownKeyGuard() {
	kg.mu.Lock()
	now := time.Now()
	for pid, g := range kg.guarded {
		if g.leases <= 0 && !now.Before(g.until) {
			delete(kg.guarded, pid)
			if t := kg.targets[pid]; t != nil {
				delete(kg.targets, pid)
				defer stopTap(t)
			}
		}
	}
	if len(kg.guarded) == 0 && kg.front != nil {
		front := kg.front
		kg.front = nil
		defer stopTap(front)
	}
	kg.mu.Unlock()
}

var _ keyGuarder = (*macBackend)(nil)
