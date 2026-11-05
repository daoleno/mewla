//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"image"
	"math"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The input iPhone Mirroring takes from behind other windows, and reading
// the text on its picture. Clicks are bgclick.go's recipe; the rest follows
// what the mirror is known to take (trycua/cua issues 4919 and 4920, MIT):
// keys reach the iPhone only while the window is key for its app, and only
// as real key codes, so each batch of keys first makes the window key
// without raising it, then posts key codes with their modifiers as flag
// changes. Swipes are sent in the way SwipeHow names.

// More CGEvent values.
const (
	cgEventKeyDown       = 10
	cgEventKeyUp         = 11
	cgEventFlagsChanged  = 12
	cgScrollUnitPixel    = 0
	fieldScrollPhase     = 99  // kCGScrollWheelEventScrollPhase
	fieldMomentumPhase   = 123 // kCGScrollWheelEventMomentumPhase
	fieldScrollContinous = 88  // kCGScrollWheelEventIsContinuous
	scrollPhaseBegan     = 1
	scrollPhaseChanged   = 2
	scrollPhaseEnded     = 4
)

// modifierKeys are the key codes of the modifiers a stroke's flags hold.
var modifierKeys = []struct {
	flag uint64
	code uint16
}{{macFlagCommand, 55}, {macFlagShift, 56}, {macFlagOption, 58}, {macFlagControl, 59}}

var mirrorSyms struct {
	once    sync.Once
	setType func(event uintptr, kind uint32)
}

func loadMirrorSyms() {
	mirrorSyms.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		if _, err := purego.Dlsym(lib, "CGEventSetType"); err == nil {
			purego.RegisterLibFunc(&mirrorSyms.setType, lib, "CGEventSetType")
		}
	})
}

// mirrorApp is where macOS 15 and later keep iPhone Mirroring.
const mirrorApp = "/System/Applications/iPhone Mirroring.app"

// MirrorInstalled says whether this Mac has iPhone Mirroring.
func (b *macBackend) MirrorInstalled() bool {
	_, err := os.Stat(mirrorApp)
	return err == nil
}

// HoldTo presses one point of an app's window behind others for hold.
func (s *macSession) HoldTo(pid int, window AppWindow, x, y int, hold time.Duration) error {
	return s.clickTo(pid, window, x, y, 0, 1, clickStyle{hold: hold})
}

// stamp gives an event the routing fields of a background event for window.
func (k *skyLightSyms) stamp(event uintptr, pid int, window AppWindow, local fpoint) {
	k.setField(event, fieldTargetPID, int64(pid))
	k.setField(event, fieldWindowNumber, int64(window.ID))
	k.setField(event, fieldWindowUnder, int64(window.ID))
	k.setField(event, fieldWindowUnderCan, int64(window.ID))
	k.setWindowLoc(event, local.X, local.Y)
}

// SwipeTo moves a finger from x, y (framebuffer pixels on the window) by
// dx, dy, in the way how says, to an app behind other windows.
func (s *macSession) SwipeTo(pid int, window AppWindow, x, y, dx, dy int, how SwipeHow) error {
	k := loadSkyLight()
	if k.why != "" {
		return errors.New(k.why)
	}
	if err := k.focusWithoutRaise(pid, window.ID); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	source := mac.CGEventSourceCreate(1)
	defer func() {
		if source != 0 {
			mac.CFRelease(source)
		}
	}()
	windowLocal := windowLocalStamp(productVersion())
	at := func(px, py int) (fpoint, fpoint) {
		gx, gy := s.input.point(image.Pt(px, py))
		g := fpoint{gx, gy}
		if !windowLocal {
			return g, g
		}
		return g, fpoint{float64(px-window.Bounds.Min.X) * s.input.scale, float64(py-window.Bounds.Min.Y) * s.input.scale}
	}
	// Ten moves over about a third of a second, as a finger does.
	const moves = 10
	switch how {
	case SwipeDrag:
		group := time.Now().UnixNano() & 0x7fffffff
		post := func(kind uint32, px, py int, pause time.Duration) error {
			g, l := at(px, py)
			event := mac.CGEventCreateMouseEvent(source, kind, cgPoint{g.X, g.Y}, 0)
			if event == 0 {
				return errors.New("couldn't make the swipe event")
			}
			k.setField(event, fieldClickState, 1)
			k.setField(event, fieldClickGroup, group)
			k.stamp(event, pid, window, l)
			mac.CGEventSetFlags(event, 0)
			k.post(int32(pid), event)
			mac.CFRelease(event)
			time.Sleep(pause)
			return nil
		}
		if err := post(macDownTypes[0], x, y, 30*time.Millisecond); err != nil {
			return err
		}
		for i := 1; i <= moves; i++ {
			if err := post(macDragTypes[0], x+dx*i/moves, y+dy*i/moves, 30*time.Millisecond); err != nil {
				return err
			}
		}
		return post(macUpTypes[0], x+dx, y+dy, 0)
	case SwipeScroll, SwipeWheel:
		// The content follows the finger: a finger moving up scrolls by a
		// negative delta, as a trackpad with natural scrolling sends.
		g, l := at(x, y)
		total := fpoint{float64(dx) * s.input.scale, float64(dy) * s.input.scale}
		sent := fpoint{}
		for i := 0; i <= moves+1; i++ {
			var step fpoint
			phase := int64(scrollPhaseChanged)
			switch {
			case i == 0:
				phase = scrollPhaseBegan
			case i == moves+1:
				phase = scrollPhaseEnded
			default:
				want := fpoint{total.X * float64(i) / moves, total.Y * float64(i) / moves}
				step = fpoint{math.Round(want.X - sent.X), math.Round(want.Y - sent.Y)}
				sent = fpoint{sent.X + step.X, sent.Y + step.Y}
			}
			var event uintptr
			if how == SwipeWheel {
				if i == 0 || i == moves+1 {
					continue
				}
				// Lines, roughly a tenth of the stroke each.
				event = mac.CGEventCreateScrollWheelEvent2(source, cgScrollUnitLine, 2, int32(math.Copysign(math.Ceil(math.Abs(step.Y)/10), step.Y)), int32(math.Copysign(math.Ceil(math.Abs(step.X)/10), step.X)), 0)
			} else {
				event = mac.CGEventCreateScrollWheelEvent2(source, cgScrollUnitPixel, 2, int32(step.Y), int32(step.X), 0)
			}
			if event == 0 {
				return errors.New("couldn't make the scroll event")
			}
			mac.CGEventSetLocation(event, cgPoint{g.X, g.Y})
			if how == SwipeScroll {
				k.setField(event, fieldScrollContinous, 1)
				k.setField(event, fieldScrollPhase, phase)
				k.setField(event, fieldMomentumPhase, 0)
			}
			k.stamp(event, pid, window, l)
			mac.CGEventSetFlags(event, 0)
			k.post(int32(pid), event)
			mac.CFRelease(event)
			time.Sleep(30 * time.Millisecond)
		}
		return nil
	}
	return errors.New("unknown way to swipe: " + string(how))
}

// makeKeyFor makes window the key window of its app without raising it.
func (k *skyLightSyms) makeKeyFor(pid int, window AppWindow) error {
	psn, ok := k.psnForWindow(window.ID, pid)
	if !ok {
		return errors.New("couldn't find the app behind that window")
	}
	if err := k.postRecordTo(psn, focusRecord(window.ID, 1)); err != nil {
		return err
	}
	for _, kind := range []byte{1, 2} {
		if err := k.postRecordTo(psn, makeKeyRecord(window.ID, kind)); err != nil {
			return err
		}
	}
	return nil
}

// StrokesTo sends key strokes to an app behind other windows, its window
// made key first.
func (s *macSession) StrokesTo(pid int, window AppWindow, strokes []KeyStroke) error {
	k := loadSkyLight()
	if k.why != "" {
		return errors.New(k.why)
	}
	post := eventPostToPid()
	if post == nil {
		return errors.New("this Mac can't send keys to one app")
	}
	loadMirrorSyms()
	if err := k.makeKeyFor(pid, window); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	send := func(code uint16, down bool, flags uint64, text rune, kind uint32) error {
		ev := mac.CGEventCreateKeyboardEvent(source, code, down)
		if ev == 0 {
			return errors.New("couldn't make the key event")
		}
		if kind == cgEventFlagsChanged && mirrorSyms.setType != nil {
			mirrorSyms.setType(ev, cgEventFlagsChanged)
		}
		mac.CGEventSetFlags(ev, flags)
		if text != 0 && flags&(macFlagCommand|macFlagControl|macFlagOption) == 0 {
			units := utf16Units(text)
			mac.CGEventKeyboardSetUnicodeString(ev, uint64(len(units)), &units[0])
		}
		post(int32(pid), ev)
		mac.CFRelease(ev)
		return nil
	}
	for _, stroke := range strokes {
		// Modifiers go down one by one, as flag changes, and come up in reverse.
		var held uint64
		for _, m := range modifierKeys {
			if stroke.Flags&m.flag != 0 {
				held |= m.flag
				if err := send(m.code, true, held, 0, cgEventFlagsChanged); err != nil {
					return err
				}
			}
		}
		if err := send(stroke.Code, true, held, stroke.Text, cgEventKeyDown); err != nil {
			return err
		}
		if err := send(stroke.Code, false, held, stroke.Text, cgEventKeyUp); err != nil {
			return err
		}
		for i := len(modifierKeys) - 1; i >= 0; i-- {
			m := modifierKeys[i]
			if held&m.flag != 0 {
				held &^= m.flag
				if err := send(m.code, false, held, 0, cgEventFlagsChanged); err != nil {
					return err
				}
			}
		}
		time.Sleep(12 * time.Millisecond)
	}
	return nil
}

const pasteboardText = "public.utf8-plain-text"

// pasteboardSaved is one type of one pasteboard item, retained until put back.
type pasteboardSaved struct{ item, kind, data objc.ID }

// PasteTo pastes text into an app behind other windows: the text goes on
// the pasteboard for ⌘V, marked so clipboard managers don't keep it, and
// what the pasteboard held, every item and type, comes back after, unless
// the person copied something meanwhile.
func (s *macSession) PasteTo(pid int, window AppWindow, text string) error {
	if err := loadMac(); err != nil {
		return err
	}
	selGeneral := objc.RegisterName("generalPasteboard")
	selChange := objc.RegisterName("changeCount")
	var saved []pasteboardSaved
	var ours int64
	withPool(func() {
		board := objc.ID(objc.GetClass("NSPasteboard")).Send(selGeneral)
		items := board.Send(objc.RegisterName("pasteboardItems"))
		if items != 0 {
			for i := uint64(0); i < objc.Send[uint64](items, selCount); i++ {
				item := items.Send(selObjectAt, i)
				kinds := item.Send(objc.RegisterName("types"))
				for j := uint64(0); kinds != 0 && j < objc.Send[uint64](kinds, selCount); j++ {
					kind := kinds.Send(selObjectAt, j)
					data := item.Send(objc.RegisterName("dataForType:"), kind)
					if data == 0 {
						continue
					}
					saved = append(saved, pasteboardSaved{item: objc.ID(i), kind: kind.Send(selRetain), data: data.Send(selRetain)})
				}
			}
		}
		board.Send(objc.RegisterName("clearContents"))
		board.Send(objc.RegisterName("setString:forType:"), nsString(text), nsString(pasteboardText))
		empty := objc.ID(objc.GetClass("NSData")).Send(objc.RegisterName("data"))
		for _, marker := range []string{"org.nspasteboard.TransientType", "org.nspasteboard.ConcealedType"} {
			board.Send(objc.RegisterName("setData:forType:"), empty, nsString(marker))
		}
		ours = objc.Send[int64](board, selChange)
	})
	err := s.StrokesTo(pid, window, []KeyStroke{{Code: macANSIKeys['v'].code, Flags: macFlagCommand}})
	// The iPhone reads the pasteboard as it pastes.
	time.Sleep(600 * time.Millisecond)
	withPool(func() {
		defer func() {
			for _, sv := range saved {
				sv.kind.Send(selRelease)
				sv.data.Send(selRelease)
			}
		}()
		board := objc.ID(objc.GetClass("NSPasteboard")).Send(selGeneral)
		if objc.Send[int64](board, selChange) != ours {
			// The person copied something: it stays.
			return
		}
		board.Send(objc.RegisterName("clearContents"))
		if len(saved) == 0 {
			return
		}
		objects := objc.ID(objc.GetClass("NSMutableArray")).Send(objc.RegisterName("array"))
		var item objc.ID
		last := objc.ID(^uintptr(0))
		for _, sv := range saved {
			if sv.item != last {
				item = objc.ID(objc.GetClass("NSPasteboardItem")).Send(selAlloc).Send(objc.RegisterName("init"))
				objects.Send(objc.RegisterName("addObject:"), item)
				item.Send(selRelease)
				last = sv.item
			}
			item.Send(objc.RegisterName("setData:forType:"), sv.data, sv.kind)
		}
		board.Send(objc.RegisterName("writeObjects:"), objects)
	})
	return err
}

var (
	_ mirrorInput  = (*macSession)(nil)
	_ textReader   = (*macBackend)(nil)
	_ mirrorHolder = (*macBackend)(nil)
)

// Vision reads text: the accurate recognizer, Chinese and English.
var vision struct {
	once sync.Once
	err  error

	colorSpace   func() uintptr
	bitmap       func(data unsafe.Pointer, w, h, bits, row uint64, space uintptr, info uint32) uintptr
	bitmapData   func(ctx uintptr) unsafe.Pointer
	bitmapRow    func(ctx uintptr) uint64
	bitmapImage  func(ctx uintptr) uintptr
	release      func(ref uintptr)
	releaseSpace func(ref uintptr)
	releaseImage func(ref uintptr)
	releaseCtx   func(ref uintptr)
}

func loadVision() error {
	vision.once.Do(func() {
		if err := loadMac(); err != nil {
			vision.err = err
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/Vision.framework/Vision", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			vision.err = errors.New("this Mac has no Vision to read text")
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			vision.err = err
			return
		}
		purego.RegisterLibFunc(&vision.colorSpace, cg, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&vision.bitmap, cg, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&vision.bitmapData, cg, "CGBitmapContextGetData")
		purego.RegisterLibFunc(&vision.bitmapRow, cg, "CGBitmapContextGetBytesPerRow")
		purego.RegisterLibFunc(&vision.bitmapImage, cg, "CGBitmapContextCreateImage")
		purego.RegisterLibFunc(&vision.releaseSpace, cg, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&vision.releaseImage, cg, "CGImageRelease")
		purego.RegisterLibFunc(&vision.releaseCtx, cg, "CGContextRelease")
		if objc.GetClass("VNRecognizeTextRequest") == 0 {
			vision.err = errors.New("this Mac's Vision can't read text")
		}
	})
	return vision.err
}

// kCGImageAlphaPremultipliedLast: RGBA in memory, as image.RGBA holds it.
const cgImageAlphaPremultipliedLast = 1

// ReadText reads the lines of text on a picture, with their boxes in its
// pixels, top left first.
func (b *macBackend) ReadText(ctx context.Context, img *image.RGBA) ([]TextLine, error) {
	if err := loadVision(); err != nil {
		return nil, err
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w == 0 || h == 0 {
		return nil, nil
	}
	var lines []TextLine
	var readErr error
	withPool(func() {
		space := vision.colorSpace()
		defer vision.releaseSpace(space)
		bctx := vision.bitmap(nil, uint64(w), uint64(h), 8, 0, space, cgImageAlphaPremultipliedLast)
		if bctx == 0 {
			readErr = errors.New("couldn't make a picture to read")
			return
		}
		defer vision.releaseCtx(bctx)
		row := int(vision.bitmapRow(bctx))
		dst := unsafe.Slice((*byte)(vision.bitmapData(bctx)), row*h)
		for y := 0; y < h; y++ {
			copy(dst[y*row:y*row+w*4], img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y):])
		}
		cgImage := vision.bitmapImage(bctx)
		if cgImage == 0 {
			readErr = errors.New("couldn't make a picture to read")
			return
		}
		defer vision.releaseImage(cgImage)

		options := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionary"))
		handler := objc.ID(objc.GetClass("VNImageRequestHandler")).Send(selAlloc).Send(objc.RegisterName("initWithCGImage:options:"), cgImage, options)
		defer handler.Send(selRelease)
		request := objc.ID(objc.GetClass("VNRecognizeTextRequest")).Send(selAlloc).Send(objc.RegisterName("init"))
		defer request.Send(selRelease)
		request.Send(objc.RegisterName("setRecognitionLevel:"), int64(0)) // accurate
		request.Send(objc.RegisterName("setUsesLanguageCorrection:"), true)
		zh, en := nsString("zh-Hans"), nsString("en-US")
		pair := [2]objc.ID{zh, en}
		langs := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("arrayWithObjects:count:"), unsafe.Pointer(&pair[0]), uint64(2))
		request.Send(objc.RegisterName("setRecognitionLanguages:"), langs)
		requests := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("arrayWithObject:"), request)
		var nsErr objc.ID
		if !objc.Send[bool](handler, objc.RegisterName("performRequests:error:"), requests, unsafe.Pointer(&nsErr)) {
			readErr = errors.New("reading the text failed: " + nsErrorText(nsErr))
			return
		}
		results := request.Send(objc.RegisterName("results"))
		if results == 0 {
			return
		}
		n := objc.Send[uint64](results, selCount)
		for i := uint64(0); i < n; i++ {
			obs := results.Send(selObjectAt, i)
			candidates := obs.Send(objc.RegisterName("topCandidates:"), uint64(1))
			if candidates == 0 || objc.Send[uint64](candidates, selCount) == 0 {
				continue
			}
			best := candidates.Send(selObjectAt, uint64(0))
			str := best.Send(objc.RegisterName("string"))
			if str == 0 {
				continue
			}
			text := objc.Send[string](str, selUTF8String)
			confidence := objc.Send[float32](best, objc.RegisterName("confidence"))
			// boundingBox is a CGRect, normalized, from the bottom left;
			// boxed by key-value coding so no struct comes back by value.
			value := obs.Send(objc.RegisterName("valueForKey:"), nsString("boundingBox"))
			var r [4]float64
			if value != 0 {
				value.Send(objc.RegisterName("getValue:size:"), unsafe.Pointer(&r[0]), uint64(unsafe.Sizeof(r)))
			}
			box := image.Rect(
				int(r[0]*float64(w)), int((1-r[1]-r[3])*float64(h)),
				int((r[0]+r[2])*float64(w)+0.5), int((1-r[1])*float64(h)+0.5),
			).Intersect(image.Rect(0, 0, w, h))
			lines = append(lines, TextLine{Text: text, Box: box, Confidence: float64(confidence)})
		}
	})
	if readErr != nil {
		return nil, readErr
	}
	sortLines(lines)
	return lines, ctx.Err()
}
