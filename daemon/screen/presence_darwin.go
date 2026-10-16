//go:build darwin && !screenfake

package screen

import (
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// Presence functions load apart from capture and input: if this macOS lacks
// one, agents only lose the lock check and the pause for someone at the
// computer, never Screen itself.
var presenceLib struct {
	once sync.Once
	ok   bool

	copySession   func() uintptr
	dictValue     func(dict, key uintptr) uintptr
	boolValue     func(ref uintptr) bool
	typeID        func(ref uintptr) uint64
	booleanTypeID func() uint64
	createString  func(alloc uintptr, text string, encoding uint32) uintptr
	release       func(ref uintptr)
	secondsSince  func(state int32, eventType uint32) float64
	lockedKey     uintptr
	onConsoleKey  uintptr
}

const (
	cfStringEncodingUTF8             = 0x08000100
	cgEventSourceStateHIDSystemState = 1
)

// personEvents are the CGEventTypes (CGEventTypes.h) that make each kind of
// input. Not kCGAnyInputEventType: that also counts touches and gestures,
// which a hand resting on a trackpad makes without end, and a program
// declaring activity to keep the display on.
var personEvents = [inputKinds][]uint32{
	InputKey:     {10, 12},      // key down, modifier change
	InputClick:   {1, 3, 25},    // left, right, other button down
	InputPointer: {5, 6, 7, 27}, // moved, dragged with each button
	InputScroll:  {22},          // scroll wheel or two-finger scroll
}

func loadPresence() bool {
	presenceLib.once.Do(func() {
		const frameworks = "/System/Library/Frameworks/"
		cf, err := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			log.Printf("screen: presence unavailable: %v", err)
			return
		}
		cg, err := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			log.Printf("screen: presence unavailable: %v", err)
			return
		}
		for _, fn := range []struct {
			ptr  any
			lib  uintptr
			name string
		}{
			{&presenceLib.copySession, cg, "CGSessionCopyCurrentDictionary"},
			{&presenceLib.secondsSince, cg, "CGEventSourceSecondsSinceLastEventType"},
			{&presenceLib.dictValue, cf, "CFDictionaryGetValue"},
			{&presenceLib.boolValue, cf, "CFBooleanGetValue"},
			{&presenceLib.typeID, cf, "CFGetTypeID"},
			{&presenceLib.booleanTypeID, cf, "CFBooleanGetTypeID"},
			{&presenceLib.createString, cf, "CFStringCreateWithCString"},
			{&presenceLib.release, cf, "CFRelease"},
		} {
			if _, err := purego.Dlsym(fn.lib, fn.name); err != nil {
				log.Printf("screen: presence unavailable: no %s", fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, fn.lib, fn.name)
		}
		// Kept for the life of the process.
		presenceLib.lockedKey = presenceLib.createString(0, "CGSSessionScreenIsLocked", cfStringEncodingUTF8)
		presenceLib.onConsoleKey = presenceLib.createString(0, "kCGSSessionOnConsoleKey", cfStringEncodingUTF8)
		presenceLib.ok = presenceLib.lockedKey != 0 && presenceLib.onConsoleKey != 0
	})
	return presenceLib.ok
}

// Presence reads the login session's lock state and the time since the
// last keyboard or pointer input. Whatever can't be read counts as unlocked
// and nobody at the computer, as before this check existed.
func (b *macBackend) Presence() Presence {
	if !loadPresence() {
		return Presence{}
	}
	var p Presence
	if session := presenceLib.copySession(); session != 0 {
		// The lock key is present only while locked; a session switched away
		// from the console (fast user switching) can't be used either.
		p.Locked = sessionFlag(session, presenceLib.lockedKey, false) || !sessionFlag(session, presenceLib.onConsoleKey, true)
		presenceLib.release(session)
	}
	now := time.Now()
	for kind, events := range personEvents {
		for _, event := range events {
			seconds := presenceLib.secondsSince(cgEventSourceStateHIDSystemState, event)
			if seconds < 0 || seconds >= 365*24*3600 {
				continue
			}
			if at := now.Add(-time.Duration(seconds * float64(time.Second))); at.After(p.Input[kind]) {
				p.Input[kind] = at
			}
		}
	}
	return p
}

func sessionFlag(session, key uintptr, missing bool) bool {
	value := presenceLib.dictValue(session, key)
	if value == 0 || presenceLib.typeID(value) != presenceLib.booleanTypeID() {
		return missing
	}
	return presenceLib.boolValue(value)
}
