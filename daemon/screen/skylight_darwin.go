//go:build darwin && !screenfake

package screen

import (
	"errors"
	"fmt"
	"image"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// SkyLight is the private framework under the window server. Its
// SLEventPostToPid delivers an event to one app as if it came through the
// window server, and SLPSPostEventRecordTo tells an app one of its windows is
// key without raising it. Together they let a click reach a window behind
// others without the pointer, the way trycua/cua's driver does (see
// bgclick.go). They are private: a macOS without them, or that changed them,
// only loses clicks behind other windows, which then go on the screen.
type skyLightSyms struct {
	once sync.Once
	why  string

	post          func(pid int32, event uintptr)
	setField      func(event uintptr, field uint32, value int64)
	setWindowLoc  func(event uintptr, x, y float64)
	postRecord    func(psn unsafe.Pointer, record unsafe.Pointer) int32
	windowOwner   func(cid, wid uint32, owner *uint32) int32
	connectionPSN func(cid uint32, psn unsafe.Pointer) int32
	mainCID       func() uint32
	processForPID func(pid int32, psn unsafe.Pointer) int32
}

var skyLight skyLightSyms

func loadSkyLight() *skyLightSyms {
	skyLight.once.Do(func() {
		sky, err := purego.Dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			skyLight.why = "this macOS has no SkyLight to click behind other windows"
			log.Printf("screen: clicks behind other windows unavailable: %v", err)
			return
		}
		libs := []uintptr{sky}
		for _, path := range []string{
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
			"/System/Library/Frameworks/CoreServices.framework/CoreServices",
		} {
			if lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
				libs = append(libs, lib)
			}
		}
		var missing []string
		bind := func(fn any, names ...string) bool {
			for _, lib := range libs {
				for _, name := range names {
					if _, err := purego.Dlsym(lib, name); err == nil {
						purego.RegisterLibFunc(fn, lib, name)
						return true
					}
				}
			}
			missing = append(missing, names[0])
			return false
		}
		bind(&skyLight.post, "SLEventPostToPid")
		bind(&skyLight.setField, "SLEventSetIntegerValueField")
		bind(&skyLight.setWindowLoc, "CGEventSetWindowLocation")
		bind(&skyLight.postRecord, "SLPSPostEventRecordTo")
		bind(&skyLight.mainCID, "CGSMainConnectionID", "SLSMainConnectionID")
		bind(&skyLight.windowOwner, "SLSGetWindowOwner")
		bind(&skyLight.connectionPSN, "SLSGetConnectionPSN")
		bind(&skyLight.processForPID, "GetProcessForPID")
		// The window-owner lookup has GetProcessForPID to fall back on.
		switch {
		case skyLight.post == nil:
			skyLight.why = "this macOS's SkyLight can't post to one app"
		case skyLight.setField == nil || skyLight.setWindowLoc == nil:
			skyLight.why = "this macOS's SkyLight can't route a click to one window"
		case skyLight.postRecord == nil:
			skyLight.why = "this macOS's SkyLight can't make a window key without raising it"
		case skyLight.processForPID == nil && (skyLight.windowOwner == nil || skyLight.connectionPSN == nil || skyLight.mainCID == nil):
			skyLight.why = "this macOS's SkyLight can't find the app behind a window"
		}
		if skyLight.why != "" {
			log.Printf("screen: clicks behind other windows unavailable: %s (missing %s)", skyLight.why, strings.Join(missing, ", "))
		}
	})
	return &skyLight
}

func skyLightPost() (func(pid int32, event uintptr), string) {
	s := loadSkyLight()
	if s.why != "" {
		return nil, s.why
	}
	return s.post, ""
}

var productVersion = sync.OnceValue(func() string {
	out, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})

func (s *macSession) BackgroundClicks() (bool, string) {
	post, why := skyLightPost()
	return post != nil, why
}

// psnForWindow is the process serial number of the app that owns a window,
// which the window server's records are addressed to.
func (k *skyLightSyms) psnForWindow(window uint32, pid int) ([8]byte, bool) {
	var psn [8]byte
	if k.windowOwner != nil && k.connectionPSN != nil && k.mainCID != nil {
		var owner uint32
		if k.windowOwner(k.mainCID(), window, &owner) == 0 && owner != 0 &&
			k.connectionPSN(owner, unsafe.Pointer(&psn[0])) == 0 {
			return psn, true
		}
	}
	if k.processForPID != nil && k.processForPID(int32(pid), unsafe.Pointer(&psn[0])) == 0 {
		return psn, true
	}
	return psn, false
}

// postRecordTo sends the window server's record to the app with psn.
func (k *skyLightSyms) postRecordTo(psn [8]byte, record [recordSize]byte) error {
	status := k.postRecord(unsafe.Pointer(&psn[0]), unsafe.Pointer(&record[0]))
	switch status {
	case 0:
		return nil
	case statusInvalidConnection:
		return errors.New("the window server gave Mewla no connection to that app (status 1002), so the app can't be made ready for the click; Mewla may be running outside its app bundle")
	}
	return fmt.Errorf("the window server refused the app's focus record (status %d)", status)
}

// focusWithoutRaise tells the app with pid that its window is the one in
// focus, without raising it, switching Space or telling the person's front
// app anything: only the focus record goes, to the target. (A defocus record
// to the front app made its key window resign, and the person's typing was
// dropped until they clicked it again.)
func (k *skyLightSyms) focusWithoutRaise(pid int, window uint32) error {
	psn, ok := k.psnForWindow(window, pid)
	if !ok {
		return errors.New("couldn't find the app behind that window")
	}
	return k.postRecordTo(psn, focusRecord(window, 1))
}

// ClickTo clicks one window of the app with pid, by framebuffer pixel,
// without moving the pointer or bringing the app forward.
func (s *macSession) ClickTo(pid int, window AppWindow, x, y, button, count int) error {
	return s.clickTo(pid, window, x, y, button, count, clickStyle{})
}

// clickStyle leaves parts of the recipe out, for the tests that find which
// parts an app needs. The zero value is the whole recipe.
type clickStyle struct {
	noFocus     bool
	noPrimer    bool
	noWindowLoc bool
	flipStamp   bool // stamp the global point where the window one is due, and the reverse
	// hold keeps the button down this long before the release: a long press.
	hold time.Duration
}

func (s *macSession) clickTo(pid int, window AppWindow, x, y, button, count int, style clickStyle) error {
	k := loadSkyLight()
	if k.why != "" {
		return errors.New(k.why)
	}
	if !style.noFocus {
		if err := k.focusWithoutRaise(pid, window.ID); err != nil {
			return err
		}
		// AppKit updates which window is key as it handles the record.
		time.Sleep(50 * time.Millisecond)
	}
	px, py := s.input.point(image.Pt(x, y))
	lx := float64(x-window.Bounds.Min.X) * s.input.scale
	ly := float64(y-window.Bounds.Min.Y) * s.input.scale
	local := windowLocalStamp(productVersion())
	if style.flipStamp {
		local = !local
	}
	plan := clickPlan(fpoint{px, py}, fpoint{lx, ly}, count, local)
	if style.hold > 0 {
		plan[len(plan)-2].Pause = style.hold
	}
	group := time.Now().UnixNano() & 0x7fffffff
	// The person's held modifiers sit in the combined session state; the
	// source is the hardware's and every event states its flags itself.
	source := mac.CGEventSourceCreate(1)
	defer func() {
		if source != 0 {
			mac.CFRelease(source)
		}
	}()
	for _, step := range plan {
		if style.noPrimer && step.Phase == 1 || style.noPrimer && step.At.X < 0 {
			continue
		}
		var kind uint32
		switch step.Kind {
		case clickMove:
			kind = cgEventMouseMoved
		case clickDown:
			kind = macDownTypes[button]
		case clickUp:
			kind = macUpTypes[button]
		}
		event := mac.CGEventCreateMouseEvent(source, kind, cgPoint{step.At.X, step.At.Y}, uint32(button))
		if event == 0 {
			return errors.New("couldn't make the click event")
		}
		set := func(field uint32, value int64) { k.setField(event, field, value) }
		set(fieldGesturePhase, step.Phase)
		set(fieldClickState, step.State)
		set(fieldButtonNumber, int64(button))
		set(fieldSubtype, step.Subtype)
		set(fieldTargetPID, int64(pid))
		set(fieldWindowNumber, int64(window.ID))
		set(fieldWindowUnder, int64(window.ID))
		set(fieldWindowUnderCan, int64(window.ID))
		set(fieldClickGroup, group)
		if !style.noWindowLoc {
			k.setWindowLoc(event, step.Stamp.X, step.Stamp.Y)
		}
		mac.CGEventSetFlags(event, 0)
		k.post(int32(pid), event)
		mac.CFRelease(event)
		time.Sleep(step.Pause)
	}
	return nil
}

var _ appClicker = (*macSession)(nil)

// keyStyle varies how keys reach an app that is not in front, for the test
// that finds what an app takes.
type keyStyle struct {
	focus    bool // tell the app one of its windows has the focus first
	makeKey  bool // and make it the app's key window (the record pair)
	skyLight bool // post through the window server instead of CGEventPostToPid
}

// typeTo types ASCII text into the app with pid, key by key, in a style.
func (s *macSession) typeTo(pid int, window AppWindow, text string, style keyStyle) error {
	k := loadSkyLight()
	if style.focus || style.makeKey || style.skyLight {
		if k.why != "" {
			return errors.New(k.why)
		}
	}
	if style.focus || style.makeKey {
		psn, ok := k.psnForWindow(window.ID, pid)
		if !ok {
			return errors.New("couldn't find the app behind that window")
		}
		if err := k.postRecordTo(psn, focusRecord(window.ID, 1)); err != nil {
			return err
		}
		if style.makeKey {
			for _, kind := range []byte{1, 2} {
				if err := k.postRecordTo(psn, makeKeyRecord(window.ID, kind)); err != nil {
					return err
				}
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	post := eventPostToPid()
	if style.skyLight {
		post = k.post
	}
	if post == nil {
		return errors.New("this Mac can't send keys to one app")
	}
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	for _, r := range text {
		key, ok := macANSIKeys[r]
		if !ok {
			continue
		}
		for _, down := range []bool{true, false} {
			ev := mac.CGEventCreateKeyboardEvent(source, key.code, down)
			if ev == 0 {
				return errors.New("couldn't make the key event")
			}
			var flags uint64
			if key.shift {
				flags = macFlagShift
			}
			mac.CGEventSetFlags(ev, flags)
			if down && flags == 0 {
				units := utf16Units(r)
				mac.CGEventKeyboardSetUnicodeString(ev, uint64(len(units)), &units[0])
			}
			post(int32(pid), ev)
			mac.CFRelease(ev)
		}
		time.Sleep(8 * time.Millisecond)
	}
	return nil
}
