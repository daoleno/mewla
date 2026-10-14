//go:build darwin && !screenfake

package screen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The macOS backend captures the main display with ScreenCaptureKit and
// injects input with CGEvent, through purego so release builds stay
// cross-compiled without cgo. It needs Screen Recording and Accessibility.

func newBackend() Backend { return &macBackend{stale: map[string]bool{}} }

// macBackend tracks permissions granted while Mewla runs that this process
// still can't use: macOS applies some grants only to a new process.
type macBackend struct {
	mu    sync.Mutex
	stale map[string]bool
}

const (
	permScreenRecording = "screen_recording"
	permAccessibility   = "accessibility"
)

// mac holds the system functions, loaded once.
var mac struct {
	once sync.Once
	err  error

	CGPreflightScreenCaptureAccess func() bool
	CGRequestScreenCaptureAccess   func() bool
	AXIsProcessTrusted             func() bool
	AXIsProcessTrustedWithOptions  func(options objc.ID) bool
	CGMainDisplayID                func() uint32

	CGEventSourceCreate             func(state int32) uintptr
	CGEventCreateMouseEvent         func(source uintptr, kind uint32, at cgPoint, button uint32) uintptr
	CGEventCreateKeyboardEvent      func(source uintptr, code uint16, down bool) uintptr
	CGEventCreateScrollWheelEvent2  func(source uintptr, units uint32, count uint32, wheel1, wheel2, wheel3 int32) uintptr
	CGEventSetFlags                 func(event uintptr, flags uint64)
	CGEventSetLocation              func(event uintptr, at cgPoint)
	CGEventSetIntegerValueField     func(event uintptr, field uint32, value int64)
	CGEventKeyboardSetUnicodeString func(event uintptr, length uint64, text *uint16)
	CGEventPost                     func(tap uint32, event uintptr)
	CFRelease                       func(ref uintptr)

	CMSampleBufferGetImageBuffer func(sample uintptr) uintptr
	CVPixelBufferRetain          func(buffer uintptr) uintptr
	CVPixelBufferRelease         func(buffer uintptr)
	CVPixelBufferLockBaseAddress func(buffer uintptr, flags uint64) int32
	CVPixelBufferUnlockBase      func(buffer uintptr, flags uint64) int32
	CVPixelBufferGetBaseAddress  func(buffer uintptr) unsafe.Pointer
	CVPixelBufferGetBytesPerRow  func(buffer uintptr) uint64
	CVPixelBufferGetWidth        func(buffer uintptr) uint64
	CVPixelBufferGetHeight       func(buffer uintptr) uint64

	dispatchQueueCreate func(label string, attr uintptr) uintptr
	poolPush            func() uintptr
	poolPop             func(pool uintptr)
	// msgSendCMTime sends a selector whose one argument is a CMTime.
	msgSendCMTime func(id objc.ID, sel objc.SEL, value cmTime)
}

type cgPoint struct{ X, Y float64 }

type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

func loadMac() error {
	mac.once.Do(func() { mac.err = loadMacFunctions() })
	return mac.err
}

func loadMacFunctions() error {
	open := func(path string) (uintptr, error) {
		lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return 0, fmt.Errorf("load %s: %w", path, err)
		}
		return lib, nil
	}
	const frameworks = "/System/Library/Frameworks/"
	libs := map[string]uintptr{}
	for _, path := range []string{
		frameworks + "CoreFoundation.framework/CoreFoundation",
		frameworks + "Foundation.framework/Foundation",
		frameworks + "CoreGraphics.framework/CoreGraphics",
		frameworks + "ApplicationServices.framework/ApplicationServices",
		frameworks + "CoreMedia.framework/CoreMedia",
		frameworks + "CoreVideo.framework/CoreVideo",
		frameworks + "ScreenCaptureKit.framework/ScreenCaptureKit",
		"/usr/lib/libSystem.B.dylib",
		"/usr/lib/libobjc.A.dylib",
	} {
		lib, err := open(path)
		if err != nil {
			return err
		}
		libs[path[strings.LastIndex(path, "/")+1:]] = lib
	}
	var missing []string
	bind := func(fn any, lib, name string) {
		if _, err := purego.Dlsym(libs[lib], name); err != nil {
			missing = append(missing, name)
			return
		}
		purego.RegisterLibFunc(fn, libs[lib], name)
	}
	bind(&mac.CGPreflightScreenCaptureAccess, "CoreGraphics", "CGPreflightScreenCaptureAccess")
	bind(&mac.CGRequestScreenCaptureAccess, "CoreGraphics", "CGRequestScreenCaptureAccess")
	bind(&mac.CGMainDisplayID, "CoreGraphics", "CGMainDisplayID")
	bind(&mac.CGEventSourceCreate, "CoreGraphics", "CGEventSourceCreate")
	bind(&mac.CGEventCreateMouseEvent, "CoreGraphics", "CGEventCreateMouseEvent")
	bind(&mac.CGEventCreateKeyboardEvent, "CoreGraphics", "CGEventCreateKeyboardEvent")
	bind(&mac.CGEventCreateScrollWheelEvent2, "CoreGraphics", "CGEventCreateScrollWheelEvent2")
	bind(&mac.CGEventSetFlags, "CoreGraphics", "CGEventSetFlags")
	bind(&mac.CGEventSetLocation, "CoreGraphics", "CGEventSetLocation")
	bind(&mac.CGEventSetIntegerValueField, "CoreGraphics", "CGEventSetIntegerValueField")
	bind(&mac.CGEventKeyboardSetUnicodeString, "CoreGraphics", "CGEventKeyboardSetUnicodeString")
	bind(&mac.CGEventPost, "CoreGraphics", "CGEventPost")
	bind(&mac.AXIsProcessTrusted, "ApplicationServices", "AXIsProcessTrusted")
	bind(&mac.AXIsProcessTrustedWithOptions, "ApplicationServices", "AXIsProcessTrustedWithOptions")
	bind(&mac.CFRelease, "CoreFoundation", "CFRelease")
	bind(&mac.CMSampleBufferGetImageBuffer, "CoreMedia", "CMSampleBufferGetImageBuffer")
	bind(&mac.CVPixelBufferRetain, "CoreVideo", "CVPixelBufferRetain")
	bind(&mac.CVPixelBufferRelease, "CoreVideo", "CVPixelBufferRelease")
	bind(&mac.CVPixelBufferLockBaseAddress, "CoreVideo", "CVPixelBufferLockBaseAddress")
	bind(&mac.CVPixelBufferUnlockBase, "CoreVideo", "CVPixelBufferUnlockBaseAddress")
	bind(&mac.CVPixelBufferGetBaseAddress, "CoreVideo", "CVPixelBufferGetBaseAddress")
	bind(&mac.CVPixelBufferGetBytesPerRow, "CoreVideo", "CVPixelBufferGetBytesPerRow")
	bind(&mac.CVPixelBufferGetWidth, "CoreVideo", "CVPixelBufferGetWidth")
	bind(&mac.CVPixelBufferGetHeight, "CoreVideo", "CVPixelBufferGetHeight")
	bind(&mac.dispatchQueueCreate, "libSystem.B.dylib", "dispatch_queue_create")
	bind(&mac.poolPush, "libobjc.A.dylib", "objc_autoreleasePoolPush")
	bind(&mac.poolPop, "libobjc.A.dylib", "objc_autoreleasePoolPop")
	bind(&mac.msgSendCMTime, "libobjc.A.dylib", "objc_msgSend")
	if len(missing) > 0 {
		// CGEventCreateScrollWheelEvent2 and the capture-access functions
		// arrived in macOS 13 and earlier; anything older lacks them.
		return fmt.Errorf("this version of macOS lacks %s", strings.Join(missing, ", "))
	}
	if objc.GetClass("SCStream") == 0 {
		return errors.New("this version of macOS has no ScreenCaptureKit")
	}
	return nil
}

// withPool runs fn inside an autorelease pool, for Objective-C calls made
// from Go threads. A pool must be popped on the thread that pushed it, so the
// goroutine stays on one thread throughout.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := mac.poolPush()
	defer mac.poolPop(pool)
	fn()
}

var (
	selAlloc    = objc.RegisterName("alloc")
	selNew      = objc.RegisterName("new")
	selRetain   = objc.RegisterName("retain")
	selRelease  = objc.RegisterName("release")
	selCount    = objc.RegisterName("count")
	selObjectAt = objc.RegisterName("objectAtIndex:")
)

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s)
}

func nsErrorText(err objc.ID) string {
	if err == 0 {
		return "unknown error"
	}
	desc := err.Send(objc.RegisterName("localizedDescription"))
	return objc.Send[string](desc, objc.RegisterName("UTF8String"))
}

// ProcessPermissions reports this process's own view of each permission;
// `mewla screen preflight` prints it for the daemon.
func ProcessPermissions() map[string]bool {
	if loadMac() != nil {
		return nil
	}
	return map[string]bool{
		permScreenRecording: mac.CGPreflightScreenCaptureAccess(),
		permAccessibility:   mac.AXIsProcessTrusted(),
	}
}

// livePermissions asks a fresh child process when this one sees a permission
// as missing: macOS keeps answering from the state the process started with.
func livePermissions(ctx context.Context) (own, live map[string]bool) {
	own = ProcessPermissions()
	if own[permScreenRecording] && own[permAccessibility] {
		return own, own
	}
	exe, err := os.Executable()
	if err != nil {
		return own, own
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "screen", "preflight").Output()
	if err != nil || json.Unmarshal(out, &live) != nil {
		return own, own
	}
	for id, granted := range live {
		if granted && !own[id] {
			log.Printf("screen: %s is granted but not yet effective in this process", id)
		}
	}
	return own, live
}

func (b *macBackend) Capability(ctx context.Context) Capability {
	if err := loadMac(); err != nil {
		return Capability{Reason: "Screen needs macOS 13 or later (" + err.Error() + ")"}
	}
	own, live := livePermissions(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	permission := func(id, name, guidance string) Permission {
		p := Permission{ID: id, Name: name, Granted: live[id], CanOpen: true, Guidance: guidance}
		// Input posted by a process that isn't trusted yet is dropped
		// silently, so a stale Accessibility grant always needs a restart.
		// Capture reports its own failure; see Start.
		if b.stale[id] || (id == permAccessibility && live[id] && !own[id]) {
			p.Granted, p.RestartRequired = false, true
			p.Guidance = "Allowed. Restart Mewla on this computer to apply it."
		}
		return p
	}
	return Capability{Supported: true, Permissions: []Permission{
		permission(permScreenRecording, "Screen Recording",
			"In System Settings → Privacy & Security → Screen & System Audio Recording, turn on mewla (or the terminal app that runs it)."),
		permission(permAccessibility, "Accessibility",
			"In System Settings → Privacy & Security → Accessibility, turn on mewla (or the terminal app that runs it)."),
	}}
}

func (b *macBackend) RequestPermissions(context.Context) {
	if loadMac() != nil {
		return
	}
	if !mac.CGPreflightScreenCaptureAccess() {
		mac.CGRequestScreenCaptureAccess()
	}
	if !mac.AXIsProcessTrusted() {
		withPool(func() {
			yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
			options := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionaryWithObject:forKey:"), yes, nsString("AXTrustedCheckOptionPrompt"))
			mac.AXIsProcessTrustedWithOptions(options)
		})
	}
}

func (b *macBackend) OpenSettings(ctx context.Context, permission string) error {
	pane := map[string]string{permScreenRecording: "Privacy_ScreenCapture", permAccessibility: "Privacy_Accessibility"}[permission]
	if pane == "" {
		return fmt.Errorf("unknown permission %q", permission)
	}
	return exec.CommandContext(ctx, "open", "x-apple.systempreferences:com.apple.preference.security?"+pane).Run()
}

// Confirm shows a dialog on this Mac; the text travels as an argument, never
// as AppleScript source.
func (b *macBackend) Confirm(ctx context.Context, device string) (bool, error) {
	text := fmt.Sprintf("Allow “%s” to see and control this Mac with Mewla Screen?", device)
	out, err := exec.CommandContext(ctx, "osascript",
		"-e", "on run argv",
		"-e", `display dialog (item 1 of argv) with title "Mewla Screen" buttons {"Don't Allow", "Allow"} default button "Don't Allow" cancel button "Don't Allow" with icon caution giving up after 120`,
		"-e", "end run", text).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			return false, nil // Don't Allow
		}
		return false, err
	}
	return strings.Contains(string(out), "button returned:Allow") && !strings.Contains(string(out), "gave up:true"), nil
}

// The stream output class, registered once; outputs maps each instance to
// its session.
var (
	outputClass     objc.Class
	outputClassErr  error
	outputClassOnce sync.Once
	outputsMu       sync.Mutex
	outputs         = map[objc.ID]*macSession{}
)

func registerOutputClass() error {
	outputClassOnce.Do(func() {
		var protocols []*objc.Protocol
		for _, name := range []string{"SCStreamOutput", "SCStreamDelegate"} {
			if p := objc.GetProtocol(name); p != nil {
				protocols = append(protocols, p)
			}
		}
		outputClass, outputClassErr = objc.RegisterClass("MewlaScreenOutput", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
			{
				Cmd: objc.RegisterName("stream:didOutputSampleBuffer:ofType:"),
				Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, sample uintptr, kind int) {
					if kind != 0 { // SCStreamOutputTypeScreen
						return
					}
					outputsMu.Lock()
					s := outputs[self]
					outputsMu.Unlock()
					if s != nil {
						s.deliver(sample)
					}
				},
			},
			{
				Cmd: objc.RegisterName("stream:didStopWithError:"),
				Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, err objc.ID) {
					outputsMu.Lock()
					s := outputs[self]
					outputsMu.Unlock()
					if s != nil {
						s.stopped(errors.New("screen capture stopped: " + nsErrorText(err)))
					}
				},
			},
		})
	})
	return outputClassErr
}

func (b *macBackend) Start(ctx context.Context) (Session, error) {
	if err := loadMac(); err != nil {
		return nil, ErrUnsupported
	}
	if !b.Capability(ctx).Ready() {
		return nil, ErrPermissions
	}
	if err := registerOutputClass(); err != nil {
		return nil, fmt.Errorf("register stream output: %w", err)
	}
	WakeDisplay()

	type shareable struct{ content, err objc.ID }
	got := make(chan shareable, 1)
	handler := objc.NewBlock(func(_ objc.Block, content, err objc.ID) {
		if content != 0 {
			content.Send(selRetain)
		}
		got <- shareable{content, err}
	})
	defer handler.Release()
	objc.ID(objc.GetClass("SCShareableContent")).Send(objc.RegisterName("getShareableContentWithCompletionHandler:"), handler)
	var result shareable
	select {
	case result = <-got:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if result.content == 0 {
		reason := nsErrorText(result.err)
		if !mac.CGPreflightScreenCaptureAccess() {
			// Granted for a new process, refused to this one.
			b.mu.Lock()
			b.stale[permScreenRecording] = true
			b.mu.Unlock()
			log.Printf("screen: capture refused until restart: %s", reason)
			return nil, ErrPermissions
		}
		return nil, fmt.Errorf("list displays: %s", reason)
	}
	defer result.content.Send(selRelease)

	s := &macSession{changed: make(chan struct{}), done: make(chan struct{})}
	var startErr error
	withPool(func() { startErr = s.start(ctx, result.content) })
	if startErr != nil {
		s.Close()
		return nil, startErr
	}
	return s, nil
}

type macSession struct {
	stream, output, source objc.ID
	source0                uintptr
	input                  *macInput

	mu      sync.Mutex
	seq     uint64
	latest  uintptr // retained CVPixelBuffer
	err     error
	changed chan struct{}
	done    chan struct{}
	closed  bool
	image   *image.RGBA
}

func (s *macSession) start(ctx context.Context, content objc.ID) error {
	displays := content.Send(objc.RegisterName("displays"))
	main := mac.CGMainDisplayID()
	var display objc.ID
	for i := uint64(0); i < objc.Send[uint64](displays, selCount); i++ {
		d := displays.Send(selObjectAt, i)
		if objc.Send[uint32](d, objc.RegisterName("displayID")) == main {
			display = d
		}
	}
	if display == 0 {
		return errors.New("the main display isn't available for capture")
	}
	// Capture in points: a Retina screen at its logical size keeps frames
	// small, and framebuffer pixels map 1:1 to event coordinates.
	width := objc.Send[int64](display, objc.RegisterName("width"))
	height := objc.Send[int64](display, objc.RegisterName("height"))
	s.input = newMacInput(1)

	empty := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("array"))
	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(selAlloc).Send(objc.RegisterName("initWithDisplay:excludingWindows:"), display, empty)
	defer filter.Send(selRelease)
	config := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(selNew)
	defer config.Send(selRelease)
	config.Send(objc.RegisterName("setWidth:"), uint64(width))
	config.Send(objc.RegisterName("setHeight:"), uint64(height))
	config.Send(objc.RegisterName("setPixelFormat:"), uint32(0x42475241)) // 'BGRA'
	config.Send(objc.RegisterName("setShowsCursor:"), true)
	config.Send(objc.RegisterName("setQueueDepth:"), int64(4))
	mac.msgSendCMTime(config, objc.RegisterName("setMinimumFrameInterval:"), cmTime{Value: 1, Timescale: 30, Flags: 1})

	s.output = objc.ID(outputClass).Send(selNew)
	outputsMu.Lock()
	outputs[s.output] = s
	outputsMu.Unlock()

	s.stream = objc.ID(objc.GetClass("SCStream")).Send(selAlloc).Send(objc.RegisterName("initWithFilter:configuration:delegate:"), filter, config, s.output)
	if s.stream == 0 {
		return errors.New("create screen stream")
	}
	queue := mac.dispatchQueueCreate("mewla.screen", 0)
	var addErr objc.ID
	if !objc.Send[bool](s.stream, objc.RegisterName("addStreamOutput:type:sampleHandlerQueue:error:"), s.output, int64(0), queue, unsafe.Pointer(&addErr)) {
		return fmt.Errorf("add stream output: %s", nsErrorText(addErr))
	}
	started := make(chan objc.ID, 1)
	handler := objc.NewBlock(func(_ objc.Block, err objc.ID) {
		if err != 0 {
			err.Send(selRetain)
		}
		started <- err
	})
	defer handler.Release()
	s.stream.Send(objc.RegisterName("startCaptureWithCompletionHandler:"), handler)
	select {
	case err := <-started:
		if err != 0 {
			defer err.Send(selRelease)
			return fmt.Errorf("start screen capture: %s", nsErrorText(err))
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	s.source0 = mac.CGEventSourceCreate(0) // combined session state
	return nil
}

// deliver keeps the newest frame; it runs on the capture queue.
func (s *macSession) deliver(sample uintptr) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("screen: capture callback: %v", r)
		}
	}()
	buffer := mac.CMSampleBufferGetImageBuffer(sample)
	if buffer == 0 {
		return // an idle or blank frame carries no image
	}
	mac.CVPixelBufferRetain(buffer)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		mac.CVPixelBufferRelease(buffer)
		return
	}
	previous := s.latest
	s.latest = buffer
	s.seq++
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	if previous != 0 {
		mac.CVPixelBufferRelease(previous)
	}
}

func (s *macSession) stopped(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *macSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	for {
		s.mu.Lock()
		if s.err != nil {
			err := s.err
			s.mu.Unlock()
			return Frame{}, err
		}
		if s.seq > after && s.latest != 0 {
			buffer, seq := s.latest, s.seq
			mac.CVPixelBufferRetain(buffer)
			s.mu.Unlock()
			img := s.copy(buffer)
			mac.CVPixelBufferRelease(buffer)
			return Frame{Seq: seq, Image: img}, nil
		}
		wait := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-s.done:
			return Frame{}, errors.New("screen capture closed")
		case <-wait:
		}
	}
}

func (s *macSession) copy(buffer uintptr) *image.RGBA {
	const readOnly = 1
	mac.CVPixelBufferLockBaseAddress(buffer, readOnly)
	defer mac.CVPixelBufferUnlockBase(buffer, readOnly)
	w, h := int(mac.CVPixelBufferGetWidth(buffer)), int(mac.CVPixelBufferGetHeight(buffer))
	stride := int(mac.CVPixelBufferGetBytesPerRow(buffer))
	if s.image == nil || s.image.Rect.Dx() != w || s.image.Rect.Dy() != h {
		s.image = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	base := mac.CVPixelBufferGetBaseAddress(buffer)
	if base == nil {
		return s.image
	}
	copyBGRA(s.image, unsafe.Slice((*byte)(base), stride*h), stride)
	return s.image
}

// CGEventType values.
var (
	macDownTypes = [3]uint32{1, 3, 25} // left, right, other
	macUpTypes   = [3]uint32{2, 4, 26} // left, right, other
	macDragTypes = [3]uint32{6, 7, 27} // left, right, other
)

const (
	cgEventMouseMoved      = 5
	cgMouseClickStateField = 1
	cgHIDEventTap          = 0
	cgScrollUnitLine       = 1
)

func (s *macSession) post(events []macEvent) {
	for _, e := range events {
		var event uintptr
		switch e.Kind {
		case macMouseMove:
			event = mac.CGEventCreateMouseEvent(s.source0, cgEventMouseMoved, cgPoint{e.X, e.Y}, 0)
		case macMouseDown, macMouseUp, macMouseDrag:
			types := map[macEventKind][3]uint32{macMouseDown: macDownTypes, macMouseUp: macUpTypes, macMouseDrag: macDragTypes}[e.Kind]
			event = mac.CGEventCreateMouseEvent(s.source0, types[e.Button], cgPoint{e.X, e.Y}, uint32(e.Button))
			if event != 0 && e.Clicks > 0 {
				mac.CGEventSetIntegerValueField(event, cgMouseClickStateField, int64(e.Clicks))
			}
		case macScroll:
			event = mac.CGEventCreateScrollWheelEvent2(s.source0, cgScrollUnitLine, 2, e.Lines[0], e.Lines[1], 0)
			if event != 0 {
				mac.CGEventSetLocation(event, cgPoint{e.X, e.Y})
			}
		case macKey:
			event = mac.CGEventCreateKeyboardEvent(s.source0, e.KeyCode, e.Down)
			if event != 0 && e.Text != 0 {
				// The character itself, so the computer's input source can't
				// turn ASCII punctuation into another form.
				text := utf16Units(e.Text)
				mac.CGEventKeyboardSetUnicodeString(event, uint64(len(text)), &text[0])
			}
			if event != 0 {
				// Keys always carry the modifier state, including none.
				mac.CGEventSetFlags(event, e.Flags)
			}
		case macText:
			text := utf16Units(e.Text)
			for _, down := range []bool{true, false} {
				if ev := mac.CGEventCreateKeyboardEvent(s.source0, 0, down); ev != 0 {
					mac.CGEventKeyboardSetUnicodeString(ev, uint64(len(text)), &text[0])
					mac.CGEventSetFlags(ev, e.Flags)
					mac.CGEventPost(cgHIDEventTap, ev)
					mac.CFRelease(ev)
				}
			}
			continue
		}
		if event == 0 {
			continue
		}
		if e.Kind != macKey && e.Flags != 0 {
			mac.CGEventSetFlags(event, e.Flags)
		}
		mac.CGEventPost(cgHIDEventTap, event)
		mac.CFRelease(event)
	}
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xd800 + r>>10), uint16(0xdc00 + r&0x3ff)}
}

func (s *macSession) Pointer(x, y int, buttons uint8) error {
	s.post(s.input.pointer(x, y, buttons))
	return nil
}

func (s *macSession) Key(keysym uint32, down bool) error {
	s.post(s.input.key(keysym, down))
	return nil
}

func (s *macSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	latest := s.latest
	s.latest = 0
	close(s.done)
	s.mu.Unlock()

	// A stream that never started, or already stopped with an error, may
	// never call the stop handler; past the bound the stream and its output
	// are leaked rather than released while capture might still use them.
	stopped := true
	if s.stream != 0 {
		done := make(chan struct{})
		handler := objc.NewBlock(func(_ objc.Block, _ objc.ID) { close(done) })
		s.stream.Send(objc.RegisterName("stopCaptureWithCompletionHandler:"), handler)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			stopped = false
			log.Printf("screen: capture did not confirm stop")
		}
		handler.Release()
		if stopped {
			s.stream.Send(selRelease)
		}
	}
	if s.output != 0 {
		outputsMu.Lock()
		delete(outputs, s.output)
		outputsMu.Unlock()
		if stopped {
			s.output.Send(selRelease)
		}
	}
	if s.source0 != 0 {
		mac.CFRelease(s.source0)
	}
	if latest != 0 {
		mac.CVPixelBufferRelease(latest)
	}
	return nil
}
