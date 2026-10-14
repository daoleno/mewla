package screen

import (
	"image"
	"time"
)

// macInput turns RFB pointer and key events into macOS input events. It holds
// no platform code, so the translation is tested everywhere; the darwin
// backend only posts the events it returns.
type macInput struct {
	// scale is display points per framebuffer pixel.
	scale float64
	now   func() time.Time

	buttons   uint8
	at        image.Point
	lastDown  time.Time
	lastAt    image.Point
	lastBtn   int
	clicks    int
	modifiers map[uint32]uint64 // held modifier keysym -> flag
	pressed   map[uint32]uint16 // held keysym -> key code sent on press
}

type macEventKind int

const (
	macMouseMove macEventKind = iota
	macMouseDown
	macMouseUp
	macMouseDrag
	macScroll
	macKey
	macText
)

// Mouse buttons in CGMouseButton numbering.
const (
	macButtonLeft  = 0
	macButtonRight = 1
	macButtonOther = 2
)

// CGEventFlags for the modifiers.
const (
	macFlagShift   uint64 = 0x20000
	macFlagControl uint64 = 0x40000
	macFlagOption  uint64 = 0x80000
	macFlagCommand uint64 = 0x100000
)

type macEvent struct {
	Kind    macEventKind
	X, Y    float64 // points, for mouse events
	Button  int
	Clicks  int
	Lines   [2]int32 // scroll: vertical (up positive), horizontal (left positive)
	KeyCode uint16
	Down    bool
	Flags   uint64
	Text    rune
}

// doubleClick bounds match macOS defaults closely enough for remote use.
const (
	macDoubleClickTime  = 500 * time.Millisecond
	macDoubleClickSlop  = 4
	macScrollLinesPerUp = 3
)

func newMacInput(scale float64) *macInput {
	return &macInput{scale: scale, now: time.Now, modifiers: map[uint32]uint64{}, pressed: map[uint32]uint16{}}
}

func (m *macInput) flags() uint64 {
	var flags uint64
	for _, flag := range m.modifiers {
		flags |= flag
	}
	return flags
}

func (m *macInput) point(p image.Point) (float64, float64) {
	return float64(p.X) * m.scale, float64(p.Y) * m.scale
}

// rfbButtons maps RFB mask bits 1, 2 and 4 to left, middle and right.
var rfbButtons = [3]struct {
	mask   uint8
	button int
}{{1, macButtonLeft}, {2, macButtonOther}, {4, macButtonRight}}

// pointer handles one RFB PointerEvent.
func (m *macInput) pointer(x, y int, mask uint8) []macEvent {
	var events []macEvent
	at := image.Pt(x, y)
	px, py := m.point(at)
	flags := m.flags()
	if at != m.at {
		m.at = at
		event := macEvent{Kind: macMouseMove, X: px, Y: py, Flags: flags}
		for _, b := range rfbButtons {
			if m.buttons&b.mask != 0 {
				event = macEvent{Kind: macMouseDrag, X: px, Y: py, Button: b.button, Clicks: m.clicks, Flags: flags}
				break
			}
		}
		events = append(events, event)
	}
	for _, b := range rfbButtons {
		was, is := m.buttons&b.mask != 0, mask&b.mask != 0
		switch {
		case !was && is:
			now := m.now()
			if b.button == m.lastBtn && now.Sub(m.lastDown) <= macDoubleClickTime &&
				abs(at.X-m.lastAt.X) <= macDoubleClickSlop && abs(at.Y-m.lastAt.Y) <= macDoubleClickSlop {
				m.clicks++
			} else {
				m.clicks = 1
			}
			m.lastDown, m.lastAt, m.lastBtn = now, at, b.button
			events = append(events, macEvent{Kind: macMouseDown, X: px, Y: py, Button: b.button, Clicks: m.clicks, Flags: flags})
		case was && !is:
			events = append(events, macEvent{Kind: macMouseUp, X: px, Y: py, Button: b.button, Clicks: m.clicks, Flags: flags})
		}
	}
	// Wheel "buttons" scroll once per press: 8 up, 16 down, 32 left, 64 right.
	for _, w := range []struct {
		mask  uint8
		lines [2]int32
	}{{8, [2]int32{macScrollLinesPerUp, 0}}, {16, [2]int32{-macScrollLinesPerUp, 0}}, {32, [2]int32{0, macScrollLinesPerUp}}, {64, [2]int32{0, -macScrollLinesPerUp}}} {
		if m.buttons&w.mask == 0 && mask&w.mask != 0 {
			events = append(events, macEvent{Kind: macScroll, X: px, Y: py, Lines: w.lines, Flags: flags})
		}
	}
	m.buttons = mask
	return events
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// macModifierKeys maps modifier keysyms to their key code and flag. Super and
// Meta are the Command key; Alt is Option.
var macModifierKeys = map[uint32]struct {
	code uint16
	flag uint64
}{
	0xffe1: {56, macFlagShift}, 0xffe2: {60, macFlagShift},
	0xffe3: {59, macFlagControl}, 0xffe4: {62, macFlagControl},
	0xffe9: {58, macFlagOption}, 0xffea: {61, macFlagOption},
	0xffe7: {55, macFlagCommand}, 0xffe8: {54, macFlagCommand},
	0xffeb: {55, macFlagCommand}, 0xffec: {54, macFlagCommand},
}

// macFunctionKeys maps X11 function keysyms to macOS virtual key codes.
var macFunctionKeys = map[uint32]uint16{
	0xff08: 51,                                                                // BackSpace -> Delete
	0xff09: 48,                                                                // Tab
	0xff0d: 36,                                                                // Return
	0xff1b: 53,                                                                // Escape
	0xffff: 117,                                                               // Delete -> Forward Delete
	0xff50: 115,                                                               // Home
	0xff51: 123,                                                               // Left
	0xff52: 126,                                                               // Up
	0xff53: 124,                                                               // Right
	0xff54: 125,                                                               // Down
	0xff55: 116,                                                               // Page Up
	0xff56: 121,                                                               // Page Down
	0xff57: 119,                                                               // End
	0xff63: 114,                                                               // Insert -> Help
	0xff8d: 76,                                                                // KP_Enter
	0xffe5: 57,                                                                // Caps Lock
	0xffbe: 122, 0xffbf: 120, 0xffc0: 99, 0xffc1: 118, 0xffc2: 96, 0xffc3: 97, // F1-F6
	0xffc4: 98, 0xffc5: 100, 0xffc6: 101, 0xffc7: 109, 0xffc8: 103, 0xffc9: 111, // F7-F12
}

// macANSIKeys maps ASCII to the ANSI (US) key code and whether Shift is part
// of the character.
var macANSIKeys = func() map[rune]struct {
	code  uint16
	shift bool
} {
	keys := map[rune]struct {
		code  uint16
		shift bool
	}{}
	plain := map[rune]uint16{
		'a': 0, 's': 1, 'd': 2, 'f': 3, 'h': 4, 'g': 5, 'z': 6, 'x': 7, 'c': 8, 'v': 9,
		'b': 11, 'q': 12, 'w': 13, 'e': 14, 'r': 15, 'y': 16, 't': 17, '1': 18, '2': 19,
		'3': 20, '4': 21, '6': 22, '5': 23, '=': 24, '9': 25, '7': 26, '-': 27, '8': 28,
		'0': 29, ']': 30, 'o': 31, 'u': 32, '[': 33, 'i': 34, 'p': 35, 'l': 37, 'j': 38,
		'\'': 39, 'k': 40, ';': 41, '\\': 42, ',': 43, '/': 44, 'n': 45, 'm': 46, '.': 47,
		'`': 50, ' ': 49,
	}
	for r, code := range plain {
		keys[r] = struct {
			code  uint16
			shift bool
		}{code, false}
		if r >= 'a' && r <= 'z' {
			keys[r-'a'+'A'] = struct {
				code  uint16
				shift bool
			}{code, true}
		}
	}
	for shifted, base := range map[rune]rune{
		'!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6', '&': '7', '*': '8',
		'(': '9', ')': '0', '_': '-', '+': '=', '{': '[', '}': ']', '|': '\\', ':': ';',
		'"': '\'', '<': ',', '>': '.', '?': '/', '~': '`',
	} {
		keys[shifted] = struct {
			code  uint16
			shift bool
		}{plain[base], true}
	}
	return keys
}()

// keysymRune returns the character of a printable keysym.
func keysymRune(keysym uint32) (rune, bool) {
	switch {
	case keysym >= 0x20 && keysym <= 0x7e, keysym >= 0xa0 && keysym <= 0xff:
		return rune(keysym), true
	case keysym >= 0x01000100 && keysym <= 0x0110ffff:
		return rune(keysym - 0x01000000), true
	}
	return 0, false
}

// key handles one RFB KeyEvent.
func (m *macInput) key(keysym uint32, down bool) []macEvent {
	if mod, ok := macModifierKeys[keysym]; ok {
		if down {
			m.modifiers[keysym] = mod.flag
		} else {
			delete(m.modifiers, keysym)
		}
		return []macEvent{{Kind: macKey, KeyCode: mod.code, Down: down, Flags: m.flags()}}
	}
	if !down {
		code, ok := m.pressed[keysym]
		if !ok {
			return nil
		}
		delete(m.pressed, keysym)
		return []macEvent{{Kind: macKey, KeyCode: code, Down: false, Flags: m.flags()}}
	}
	if code, ok := macFunctionKeys[keysym]; ok {
		m.pressed[keysym] = code
		return []macEvent{{Kind: macKey, KeyCode: code, Down: true, Flags: m.flags()}}
	}
	r, ok := keysymRune(keysym)
	if !ok {
		return nil
	}
	if key, ok := macANSIKeys[r]; ok {
		flags := m.flags()
		if key.shift {
			flags |= macFlagShift
		}
		m.pressed[keysym] = key.code
		return []macEvent{{Kind: macKey, KeyCode: key.code, Down: true, Flags: flags}}
	}
	// Anything else is typed as text; a press is the whole keystroke.
	return []macEvent{{Kind: macText, Text: r, Flags: m.flags()}}
}
