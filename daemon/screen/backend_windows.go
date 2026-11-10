//go:build windows && !screenfake

package screen

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/daoleno/mewla/daemon/vdisplay"
	"golang.org/x/sys/windows"
)

// On Windows an agent uses the person's own screen, as on a Mac: Mewla runs
// in the signed-in session (its logon task), where GDI shows the screen,
// SendInput is the keyboard and pointer and UI Automation reads and acts on
// apps' elements. Nothing needs a permission. Low-level hooks tell the
// person's own keys and clicks from the input Mewla sends, and a secure
// desktop (the lock screen, a UAC prompt) says the screen is locked.

func newBackend() Backend {
	// Coordinates are physical pixels everywhere: the picture, the input and
	// UI Automation's rectangles agree whatever the display's scale.
	_, _, _ = procSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	return &windowsBackend{uia: newUIA()}
}

func platformBackend(*vdisplay.Service) Backend { return newBackend() }

type windowsBackend struct {
	uia *uia

	hooksOnce sync.Once
	input     [inputKinds]atomic.Int64 // unix nanoseconds of the person's last input of each kind
}

func computerName() string {
	name, _ := os.Hostname()
	return name
}

// ProcessPermissions has nothing to report: Windows asks for none.
func ProcessPermissions() map[string]bool { return nil }

// errNoSession is why a Mewla outside the signed-in session (a service, or
// started over SSH) can't see or use the screen.
var errNoSession = errors.New("Mewla is running outside the signed-in Windows session (as a service or over SSH), where it can't see or use the screen. It does once it runs at sign-in")

func inInteractiveSession() bool {
	var session uint32
	r, _, _ := procProcessIdToSessionId.Call(uintptr(windows.GetCurrentProcessId()), uintptr(unsafe.Pointer(&session)))
	return r != 0 && session != 0
}

func (b *windowsBackend) Capability(context.Context) Capability {
	if !inInteractiveSession() {
		return Capability{Reason: errNoSession.Error()}
	}
	return Capability{Supported: true}
}

func (*windowsBackend) RequestPermissions(context.Context) {}

func (*windowsBackend) OpenSettings(context.Context, string) error {
	return errors.New("Windows needs no permission for Screen")
}

// Confirm asks the person at this computer, in a message box over everything,
// whether a device may view and control it.
func (b *windowsBackend) Confirm(ctx context.Context, device string) (bool, error) {
	if !inInteractiveSession() {
		return false, errNoSession
	}
	text, _ := windows.UTF16PtrFromString(fmt.Sprintf("Allow %s to view and control this computer's screen with Mewla?", device))
	title, _ := windows.UTF16PtrFromString("Mewla")
	const flags = 0x4 | 0x20 | 0x40000 | 0x10000 | 0x1000 // MB_YESNO | MB_ICONQUESTION | MB_TOPMOST | MB_SETFOREGROUND | MB_SYSTEMMODAL
	answer := make(chan uintptr, 1)
	go func() {
		r, _, _ := procMessageBoxTimeoutW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), flags, 0, 60000)
		answer <- r
	}()
	select {
	case r := <-answer:
		return r == 6, nil // IDYES
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (b *windowsBackend) Start(ctx context.Context) (Session, error) {
	if !inInteractiveSession() {
		return nil, errNoSession
	}
	b.startHooks()
	w, h := screenSize()
	if w <= 0 || h <= 0 {
		return nil, errors.New("this computer has no display")
	}
	return &windowsSession{backend: b}, nil
}

func screenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	h, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	return int(w), int(h)
}

// Presence is whether the screen is locked and when the person last used it.
func (b *windowsBackend) Presence() Presence {
	var p Presence
	p.Locked = locked()
	for k := range p.Input {
		if at := b.input[k].Load(); at != 0 {
			p.Input[k] = time.Unix(0, at)
		}
	}
	return p
}

// locked says whether the person's desktop isn't the one taking input: the
// lock screen, the sign-in screen or a UAC prompt has it.
func locked() bool {
	const desktopReadObjects = 0x0001
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopReadObjects)
	if h == 0 {
		return true
	}
	defer procCloseDesktop.Call(h)
	var name [64]uint16
	var needed uint32
	r, _, _ := procGetUserObjectInformationW.Call(h, 2, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed))) // UOI_NAME
	if r == 0 {
		return false
	}
	return !strings.EqualFold(windows.UTF16ToString(name[:]), "Default")
}

// startHooks starts the low-level keyboard and mouse hooks, on a thread of
// their own with the message loop hooks need. They start with the first
// session (an agent's or a viewer's), not while Mewla only reports status:
// a computer nobody's agent or viewer has used is never hooked. Input Mewla sent itself is
// flagged injected and doesn't count as the person's.
func (b *windowsBackend) startHooks() {
	b.hooksOnce.Do(func() {
		if !inInteractiveSession() {
			return
		}
		go b.hookThread()
	})
}

var (
	hookBackend  atomic.Pointer[windowsBackend]
	keyboardHook = syscall.NewCallback(keyboardProc)
	mouseHook    = syscall.NewCallback(mouseProc)
)

func (b *windowsBackend) hookThread() {
	runtime.LockOSThread()
	hookBackend.Store(b)
	module, _, _ := procGetModuleHandleW.Call(0)
	k, _, _ := procSetWindowsHookExW.Call(13, keyboardHook, module, 0) // WH_KEYBOARD_LL
	m, _, _ := procSetWindowsHookExW.Call(14, mouseHook, module, 0)    // WH_MOUSE_LL
	if k == 0 && m == 0 {
		return
	}
	var msg [48]byte
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
	}
}

// kbdHook is a KBDLLHOOKSTRUCT; mouseHook an MSLLHOOKSTRUCT.
type kbdHook struct{ vk, scan, flags, time uint32 }
type mouseHookInfo struct {
	x, y  int32
	data  uint32
	flags uint32
	time  uint32
}

func keyboardProc(code int, wparam uintptr, lparam *kbdHook) uintptr {
	if code >= 0 && (wparam == 0x100 || wparam == 0x104) { // WM_KEYDOWN, WM_SYSKEYDOWN
		if lparam.flags&0x10 == 0 { // LLKHF_INJECTED
			if b := hookBackend.Load(); b != nil {
				b.input[InputKey].Store(time.Now().UnixNano())
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, uintptr(unsafe.Pointer(lparam)))
	return r
}

func mouseProc(code int, wparam uintptr, lparam *mouseHookInfo) uintptr {
	if code >= 0 {
		if lparam.flags&0x1 == 0 { // LLMHF_INJECTED
			if b := hookBackend.Load(); b != nil {
				now := time.Now().UnixNano()
				switch wparam {
				case 0x200: // WM_MOUSEMOVE
					b.input[InputPointer].Store(now)
				case 0x201, 0x204, 0x207, 0x20B: // button downs
					b.input[InputClick].Store(now)
				case 0x20A, 0x20E: // wheels
					b.input[InputScroll].Store(now)
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, uintptr(unsafe.Pointer(lparam)))
	return r
}

// windowsSession captures the primary display and sends input to it.
type windowsSession struct {
	backend *windowsBackend

	mu      sync.Mutex
	seq     uint64
	sum     [32]byte
	have    bool
	buttons uint8
	mods    map[uint32]bool    // modifier keysyms held down
	sent    map[uint32]charKey // characters pressed, to let go the same way
}

// Frame takes pictures of the primary display until one differs from the
// frame numbered after.
func (s *windowsSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	for {
		img, err := captureScreen()
		if err == nil {
			sum := sha256.Sum256(img.Pix)
			s.mu.Lock()
			if !s.have || sum != s.sum {
				s.seq++
				s.sum, s.have = sum, true
			}
			seq := s.seq
			s.mu.Unlock()
			if seq > after {
				return Frame{Seq: seq, Image: img, At: time.Now()}, nil
			}
		} else if locked() {
			// The secure desktop can't be captured; wait for it to go.
		} else {
			return Frame{}, err
		}
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// captureScreen copies the primary display with GDI, as RGBA.
func captureScreen() (*image.RGBA, error) {
	w, h := screenSize()
	if w <= 0 || h <= 0 {
		return nil, errors.New("this computer has no display")
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("the screen can't be read now")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("the screen can't be read now")
	}
	defer procDeleteDC.Call(memDC)
	header := struct {
		Size                          uint32
		Width, Height                 int32
		Planes, BitCount              uint16
		Compression, SizeImage        uint32
		XPels, YPels, Used, Important int32
	}{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	var bits *byte
	bitmap, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, errors.New("the screen can't be read now")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)
	const srccopy, captureblt = 0x00CC0020, 0x40000000
	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, 0, 0, srccopy|captureblt); r == 0 {
		return nil, errors.New("the screen can't be read now")
	}
	src := unsafe.Slice(bits, w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return img, nil
}

// input is a Win32 INPUT (40 bytes on 64-bit Windows).
type input struct {
	kind uint32
	_    uint32
	data [32]byte
}

func mouseInput(dx, dy int32, data int32, flags uint32) input {
	in := input{kind: 0}
	*(*int32)(unsafe.Pointer(&in.data[0])) = dx
	*(*int32)(unsafe.Pointer(&in.data[4])) = dy
	*(*int32)(unsafe.Pointer(&in.data[8])) = data
	*(*uint32)(unsafe.Pointer(&in.data[12])) = flags
	return in
}

func keyInput(vk, scan uint16, flags uint32) input {
	in := input{kind: 1}
	*(*uint16)(unsafe.Pointer(&in.data[0])) = vk
	*(*uint16)(unsafe.Pointer(&in.data[2])) = scan
	*(*uint32)(unsafe.Pointer(&in.data[4])) = flags
	return in
}

func sendInput(events ...input) error {
	if len(events) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if int(n) != len(events) {
		if locked() {
			return ErrScreenLocked
		}
		return fmt.Errorf("Windows refused the input (%v); an app running as administrator, or a prompt, may be in front", err)
	}
	return nil
}

// Pointer moves the pointer to (x, y) on the primary display and presses or
// lets go of buttons and the wheel as the RFB mask says.
func (s *windowsSession) Pointer(x, y int, buttons uint8) error {
	w, h := screenSize()
	if w <= 1 || h <= 1 {
		return errors.New("this computer has no display")
	}
	x, y = max(0, min(x, w-1)), max(0, min(y, h-1))
	const (
		move     = 0x0001
		absolute = 0x8000
		wheel    = 0x0800
		hwheel   = 0x1000
	)
	nx := int32((x*65535 + (w-1)/2) / (w - 1))
	ny := int32((y*65535 + (h-1)/2) / (h - 1))
	events := []input{mouseInput(nx, ny, 0, move|absolute)}
	s.mu.Lock()
	prev := s.buttons
	s.buttons = buttons & 0x7
	s.mu.Unlock()
	for i, flags := range [3][2]uint32{{0x0002, 0x0004}, {0x0020, 0x0040}, {0x0008, 0x0010}} { // left, middle, right: down, up
		bit := uint8(1) << i
		switch {
		case buttons&bit != 0 && prev&bit == 0:
			events = append(events, mouseInput(nx, ny, 0, flags[0]|absolute))
		case buttons&bit == 0 && prev&bit != 0:
			events = append(events, mouseInput(nx, ny, 0, flags[1]|absolute))
		}
	}
	if buttons&8 != 0 {
		events = append(events, mouseInput(0, 0, 120, wheel))
	}
	if buttons&16 != 0 {
		events = append(events, mouseInput(0, 0, -120, wheel))
	}
	if buttons&32 != 0 {
		events = append(events, mouseInput(0, 0, -120, hwheel))
	}
	if buttons&64 != 0 {
		events = append(events, mouseInput(0, 0, 120, hwheel))
	}
	return sendInput(events...)
}

// Key presses or lets go of an X11 keysym. A character the keyboard layout
// has a key for is that key, with Shift added when the character needs it
// and the agent isn't holding it: apps take real keys everywhere, shortcuts
// included. Other characters (another script, an emoji) are sent as the
// character itself.
func (s *windowsSession) Key(keysym uint32, down bool) error {
	const keyup, unicode, extended = 0x0002, 0x0004, 0x0001
	var flags uint32
	if !down {
		flags |= keyup
	}
	s.mu.Lock()
	if s.mods == nil {
		s.mods, s.sent = map[uint32]bool{}, map[uint32]charKey{}
	}
	if vk, ext, ok := keysymVK(keysym); ok {
		if isModifier(keysym) {
			if down {
				s.mods[keysym] = true
			} else {
				delete(s.mods, keysym)
			}
		}
		s.mu.Unlock()
		if ext {
			flags |= extended
		}
		scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0)
		return sendInput(keyInput(vk, uint16(scan), flags))
	}
	r := winKeysymRune(keysym)
	if r < 0 {
		s.mu.Unlock()
		return fmt.Errorf("no key on Windows for keysym 0x%x", keysym)
	}
	// A key is let go the way it was pressed.
	k, pressed := s.sent[keysym]
	if down || !pressed {
		k = charKey{}
		if r < 0x10000 {
			if scan, _, _ := procVkKeyScanW.Call(uintptr(r)); int16(scan) != -1 && (scan>>8)&^1 == 0 {
				k = charKey{vk: uint16(scan & 0xff), shift: scan&0x100 != 0 && !s.mods[0xffe1] && !s.mods[0xffe2]}
			}
		}
	}
	if down {
		s.sent[keysym] = k
	} else {
		delete(s.sent, keysym)
	}
	s.mu.Unlock()
	if !down {
		// Apps such as Notepad lose keys that come back to back: give each
		// one a moment once it is let go.
		defer time.Sleep(keyPace)
	}
	if k.vk != 0 {
		code, _, _ := procMapVirtualKeyW.Call(uintptr(k.vk), 0)
		key := keyInput(k.vk, uint16(code), flags)
		if !k.shift {
			return sendInput(key)
		}
		shiftScan, _, _ := procMapVirtualKeyW.Call(0x10, 0)
		if down {
			return sendInput(keyInput(0x10, uint16(shiftScan), 0), key)
		}
		return sendInput(key, keyInput(0x10, uint16(shiftScan), keyup))
	}
	units := windows.StringToUTF16(string(r))
	units = units[:len(units)-1]
	events := make([]input, 0, len(units))
	for _, u := range units {
		events = append(events, keyInput(0, u, flags|unicode))
	}
	return sendInput(events...)
}

// keyPace is the pause after each typed character.
var keyPace = 20 * time.Millisecond

// charKey is how a character's key was pressed: a virtual key, with Shift
// added, or (vk 0) the character itself.
type charKey struct {
	vk    uint16
	shift bool
}

func (s *windowsSession) Close() error { return nil }

// KeyTo sends a key to one app. Windows sends keys only to the window in
// front, and bringing one forward takes the person's keyboard, so an app
// behind is refused with what to do instead.
func (s *windowsSession) KeyTo(pid int, keysym uint32, down bool) error {
	front, _ := s.backend.FrontPID(context.Background())
	if front != pid {
		return errors.New("On Windows, keys go only to the app in front: set its text with element set_value, or bring_forward it when nobody is using the computer and send the keys then")
	}
	return s.Key(keysym, down)
}

// isModifier says whether a keysym is shift, control, alt or the Windows key.
func isModifier(keysym uint32) bool {
	switch keysym {
	case 0xffe1, 0xffe2, 0xffe3, 0xffe4, 0xffe9, 0xffea, 0xffeb, 0xffec, 0xffe7, 0xffe8:
		return true
	}
	return false
}

// keysymVK maps the keysyms of keys (not characters) to virtual keys, and
// whether the key is an extended one.
func keysymVK(keysym uint32) (uint16, bool, bool) {
	switch keysym {
	case 0xff08:
		return 0x08, false, true // BackSpace
	case 0xff09:
		return 0x09, false, true // Tab
	case 0xff0d, 0xff8d:
		return 0x0D, keysym == 0xff8d, true // Return, KP_Enter
	case 0xff13:
		return 0x13, false, true // Pause
	case 0xff1b:
		return 0x1B, false, true // Escape
	case 0xffff:
		return 0x2E, true, true // Delete
	case 0xff50:
		return 0x24, true, true // Home
	case 0xff51:
		return 0x25, true, true // Left
	case 0xff52:
		return 0x26, true, true // Up
	case 0xff53:
		return 0x27, true, true // Right
	case 0xff54:
		return 0x28, true, true // Down
	case 0xff55:
		return 0x21, true, true // Page_Up
	case 0xff56:
		return 0x22, true, true // Page_Down
	case 0xff57:
		return 0x23, true, true // End
	case 0xff63:
		return 0x2D, true, true // Insert
	case 0xff61:
		return 0x2C, false, true // Print
	case 0xff67:
		return 0x5D, true, true // Menu
	case 0xffe5:
		return 0x14, false, true // Caps_Lock
	case 0xffe1:
		return 0x10, false, true // Shift_L
	case 0xffe2:
		return 0x10, false, true // Shift_R
	case 0xffe3:
		return 0x11, false, true // Control_L
	case 0xffe4:
		return 0x11, true, true // Control_R
	case 0xffe9, 0xffe7:
		return 0x12, false, true // Alt_L, Meta_L
	case 0xffea, 0xffe8:
		return 0x12, true, true // Alt_R, Meta_R
	case 0xffeb:
		return 0x5B, true, true // Super_L
	case 0xffec:
		return 0x5C, true, true // Super_R
	case 0x20:
		return 0x20, false, true // space
	}
	if keysym >= 0xffbe && keysym <= 0xffd5 { // F1-F24
		return uint16(0x70 + keysym - 0xffbe), false, true
	}
	return 0, false, false
}

// keysymRune is the character a keysym types, or -1.
func winKeysymRune(keysym uint32) rune {
	switch {
	case keysym >= 0x20 && keysym <= 0xff:
		return rune(keysym)
	case keysym >= 0x01000000 && keysym <= 0x0110ffff:
		return rune(keysym - 0x01000000)
	}
	return -1
}

// Apps lists the apps with a window on the screen, front to back, and the
// minimized ones as hidden.
func (b *windowsBackend) Apps(ctx context.Context) ([]App, error) {
	onScreen, all := topWindows()
	front := 0
	if hwnd, _, _ := procGetForegroundWindow.Call(); hwnd != 0 {
		front = windowPID(windows.HWND(hwnd))
	}
	return appsFromWindows(onScreen, all, front, os.Getpid(), func(int) bundleInfo { return bundleInfo{} }), nil
}

// windowList collects top-level windows during EnumWindows.
var (
	enumMu      sync.Mutex
	enumWindows []windows.HWND
	enumProc    = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		enumWindows = append(enumWindows, windows.HWND(hwnd))
		return 1
	})
)

func appWindows() []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumWindows = nil
	_, _, _ = procEnumWindows.Call(enumProc, 0)
	return append([]windows.HWND(nil), enumWindows...)
}

// topWindows are the apps' own windows: visible, unowned, not tool windows
// and not cloaked (another desktop's, or a suspended app's). Those on the
// screen come first, front to back; minimized ones are only in all.
func topWindows() (onScreen, all []windowInfo) {
	names := map[int]string{}
	for _, hwnd := range appWindows() {
		if v, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); v == 0 {
			continue
		}
		if owner, _, _ := procGetWindow.Call(uintptr(hwnd), 4); owner != 0 { // GW_OWNER
			continue
		}
		exStyle, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), ^uintptr(19)) // GWL_EXSTYLE (-20)
		if exStyle&0x80 != 0 {                                                   // WS_EX_TOOLWINDOW
			continue
		}
		var cloaked uint32
		if r, _, _ := procDwmGetWindowAttribute.Call(uintptr(hwnd), 14, uintptr(unsafe.Pointer(&cloaked)), 4); r == 0 && cloaked != 0 {
			continue
		}
		pid := windowPID(hwnd)
		if pid <= 0 {
			continue
		}
		name, ok := names[pid]
		if !ok {
			name = processName(pid)
			names[pid] = name
		}
		title := windowTitle(hwnd)
		if strings.EqualFold(name, "ApplicationFrameHost") && title != "" {
			name = title // a Store app's frame: the app is what its title says
		}
		var r rect
		_, _, _ = procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
		info := windowInfo{PID: pid, Name: name, ID: uint32(hwnd), Bounds: image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))}
		if iconic, _, _ := procIsIconic.Call(uintptr(hwnd)); iconic == 0 {
			info.OnScreen = true
			onScreen = append(onScreen, info)
		}
		all = append(all, info)
	}
	return onScreen, all
}

func windowPID(hwnd windows.HWND) int {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	return int(pid)
}

func windowTitle(hwnd windows.HWND) string {
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// processName is a process's program, without .exe: Notepad.
func processName(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	base := filepath.Base(windows.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// windowsOf are an app's windows on the screen, front to back.
func windowsOf(pid int) []windows.HWND {
	onScreen, _ := topWindows()
	var out []windows.HWND
	for _, w := range onScreen {
		if w.PID == pid {
			out = append(out, windows.HWND(uintptr(w.ID)))
		}
	}
	return out
}

func (b *windowsBackend) ReadApp(ctx context.Context, pid int, limit int) (ElementTree, error) {
	hwnds := windowsOf(pid)
	if len(hwnds) == 0 {
		return ElementTree{PID: pid}, nil
	}
	tree, err := b.uia.read(hwnds, limit)
	if err != nil {
		return ElementTree{}, err
	}
	tree.PID = pid
	tree.App = processName(pid)
	tree.Window = windowTitle(hwnds[0])
	return tree, nil
}

func (b *windowsBackend) Release(tree ElementTree) { b.uia.release(tree) }

func (b *windowsBackend) ActOnElement(ctx context.Context, e Element, action, value string) error {
	return b.uia.act(e, action, value)
}

func (b *windowsBackend) ReadValue(ctx context.Context, e Element, limit int) (string, int, error) {
	return b.uia.value(e, limit)
}

func (b *windowsBackend) ElementHash(e Element) uintptr { return b.uia.hash(e) }

func (b *windowsBackend) SameElement(x, y Element) bool { return b.uia.same(x, y) }

func (b *windowsBackend) FrontPID(context.Context) (int, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0, nil
	}
	return windowPID(windows.HWND(hwnd)), nil
}

// RaiseApp brings an app's front window forward. Windows lets only the app
// the person last used take the front; an Alt press sent first makes Mewla
// that app, as Windows' own tools do.
func (b *windowsBackend) RaiseApp(ctx context.Context, pid int) error {
	_, all := topWindows()
	for _, w := range all {
		if w.PID != pid {
			continue
		}
		hwnd := uintptr(w.ID)
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
			_, _, _ = procShowWindow.Call(hwnd, 9) // SW_RESTORE
		}
		_ = sendInput(keyInput(0x12, 0, 0), keyInput(0x12, 0, 0x0002))
		if r, _, _ := procSetForegroundWindow.Call(hwnd); r == 0 {
			return errors.New("Windows didn't let the app come to the front")
		}
		return nil
	}
	return fmt.Errorf("no window of pid %d is open", pid)
}

// OpenApp starts an installed app by the name the Start menu shows it under
// or its program's name. It opens apps only: a shortcut in the Start menu
// that leads to a program, or a program in App Paths or on the PATH.
func (b *windowsBackend) OpenApp(ctx context.Context, name string) error {
	target, err := windowsAppTarget(name)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		_ = windows.CoInitializeEx(0, 0x2|0x4) // COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE
		defer windows.CoUninitialize()
		verb, _ := windows.UTF16PtrFromString("open")
		file, _ := windows.UTF16PtrFromString(target)
		home, _ := os.UserHomeDir()
		dir, _ := windows.UTF16PtrFromString(home)
		r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, uintptr(unsafe.Pointer(dir)), 1) // SW_SHOWNORMAL
		if r <= 32 {
			done <- fmt.Errorf("Windows couldn't open %s (error %d)", name, r)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// windowsAliases are apps whose program has another name.
var windowsAliases = map[string]string{
	"calculator": "calc", "paint": "mspaint", "command prompt": "cmd", "file explorer": "explorer",
	"wordpad": "write", "snipping tool": "snippingtool", "task manager": "taskmgr", "registry editor": "regedit",
}

// windowsAppTarget is what to open for an app's name: its Start menu
// shortcut, or its program.
func windowsAppTarget(name string) (string, error) {
	if err := checkAppName(name); err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	var roots []string
	if dir := os.Getenv("APPDATA"); dir != "" {
		roots = append(roots, filepath.Join(dir, `Microsoft\Windows\Start Menu\Programs`))
	}
	if dir := os.Getenv("ProgramData"); dir != "" {
		roots = append(roots, filepath.Join(dir, `Microsoft\Windows\Start Menu\Programs`))
	}
	found := ""
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || found != "" {
				return nil
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".lnk") && strings.ToLower(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))) == want {
				found = path
			}
			return nil
		})
		if found != "" {
			return found, nil
		}
	}
	program := want
	if alias, ok := windowsAliases[want]; ok {
		program = alias
	}
	if path := appPath(program); path != "" {
		return path, nil
	}
	if path, err := lookPathExe(program); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("no app named %q is installed on this computer", name)
}

// appPath is a program registered in App Paths.
func appPath(program string) string {
	for _, root := range []windows.Handle{windows.HKEY_CURRENT_USER, windows.HKEY_LOCAL_MACHINE} {
		var key windows.Handle
		sub, _ := windows.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\App Paths\` + program + ".exe")
		if windows.RegOpenKeyEx(root, sub, 0, windows.KEY_READ, &key) != nil {
			continue
		}
		var buf [windows.MAX_PATH]uint16
		n := uint32(len(buf) * 2)
		var kind uint32
		err := windows.RegQueryValueEx(key, nil, nil, &kind, (*byte)(unsafe.Pointer(&buf[0])), &n)
		windows.RegCloseKey(key)
		if err == nil {
			path := strings.Trim(windows.UTF16ToString(buf[:]), `"`)
			if path, err = windowsExpand(path); err == nil {
				if _, statErr := os.Stat(path); statErr == nil {
					return path
				}
			}
		}
	}
	return ""
}

func windowsExpand(s string) (string, error) {
	return registryExpand(s), nil
}

func registryExpand(s string) string {
	return os.Expand(strings.NewReplacer("%", "$").Replace(s), func(v string) string {
		return os.Getenv(strings.Trim(v, "$"))
	})
}

// lookPathExe finds program.exe on the PATH, never in the current folder.
func lookPathExe(program string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || dir == "." {
			continue
		}
		path := filepath.Join(dir, program+".exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
