package screen

import "strings"

// PageInput is one input event for an agent window's page, as Chrome's
// DevTools Input domain takes it (Input.dispatchMouseEvent,
// Input.dispatchKeyEvent). The browser's extension sends it through the
// debugger the window's tab is attached with: it reaches that tab only,
// moves no pointer and brings nothing forward, and Chrome takes it from a
// window behind others, which it does not take from the system's events.
type PageInput struct {
	Method string
	Params map[string]any
}

// DevTools modifier bits.
const (
	pageAlt   = 1
	pageCtrl  = 2
	pageMeta  = 4
	pageShift = 8
)

// pageModifier is the modifier bit and DevTools key of a modifier keysym.
func pageModifier(keysym uint32) (bit int, key string, vk int, ok bool) {
	switch keysym {
	case 0xffe1, 0xffe2:
		return pageShift, "Shift", 16, true
	case 0xffe3, 0xffe4:
		return pageCtrl, "Control", 17, true
	case 0xffe9, 0xffea:
		return pageAlt, "Alt", 18, true
	case 0xffe7, 0xffe8, 0xffeb, 0xffec: // Meta and Super: the Mac's Command
		return pageMeta, "Meta", 91, true
	}
	return 0, "", 0, false
}

// pageSpecial are the keys that type nothing, by keysym.
var pageSpecial = map[uint32]struct {
	key  string
	vk   int
	text string
}{
	0xff08: {"Backspace", 8, ""},
	0xff09: {"Tab", 9, ""},
	0xff0d: {"Enter", 13, "\r"},
	0xff8d: {"Enter", 13, "\r"},
	0xff1b: {"Escape", 27, ""},
	0xffff: {"Delete", 46, ""},
	0xff50: {"Home", 36, ""},
	0xff57: {"End", 35, ""},
	0xff51: {"ArrowLeft", 37, ""},
	0xff52: {"ArrowUp", 38, ""},
	0xff53: {"ArrowRight", 39, ""},
	0xff54: {"ArrowDown", 40, ""},
	0xff55: {"PageUp", 33, ""},
	0xff56: {"PageDown", 34, ""},
}

// pageEditCommands are the editing shortcuts a page on a Mac only gets as
// commands with their key (Command with the letter).
var pageEditCommands = map[rune]string{'a': "selectAll", 'c': "copy", 'v': "paste", 'x': "cut", 'z': "undo"}

// pageKey turns one RFB key event into a DevTools key event; mods are the
// modifiers held (updated for a modifier key). False for a key it does not
// know, which is not sent.
func pageKey(keysym uint32, down bool, mods *int) (PageInput, bool) {
	kind := "keyUp"
	if down {
		kind = "keyDown"
	}
	params := map[string]any{}
	if bit, key, vk, ok := pageModifier(keysym); ok {
		if down {
			*mods |= bit
		} else {
			*mods &^= bit
		}
		if down {
			kind = "rawKeyDown"
		}
		params["type"], params["key"], params["code"], params["windowsVirtualKeyCode"] = kind, key, key+"Left", vk
		params["modifiers"] = *mods
		return PageInput{Method: "Input.dispatchKeyEvent", Params: params}, true
	}
	params["modifiers"] = *mods
	if special, ok := pageSpecial[keysym]; ok {
		if down && special.text == "" {
			kind = "rawKeyDown"
		}
		params["type"], params["key"], params["code"], params["windowsVirtualKeyCode"] = kind, special.key, special.key, special.vk
		if down && special.text != "" {
			params["text"] = special.text
		}
		return PageInput{Method: "Input.dispatchKeyEvent", Params: params}, true
	}
	r, ok := keysymRune(keysym)
	if !ok {
		return PageInput{}, false
	}
	key := string(r)
	params["type"], params["key"] = kind, key
	upper := strings.ToUpper(key)
	switch {
	case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		params["code"], params["windowsVirtualKeyCode"] = "Key"+upper, int(upper[0])
	case r >= '0' && r <= '9':
		params["code"], params["windowsVirtualKeyCode"] = "Digit"+key, int(r)
	case r == ' ':
		params["code"], params["windowsVirtualKeyCode"] = "Space", 32
	}
	if down {
		if *mods&(pageCtrl|pageMeta) == 0 {
			params["text"] = key
		} else if command, ok := pageEditCommands[[]rune(strings.ToLower(key))[0]]; ok && *mods&pageMeta != 0 {
			params["commands"] = []string{command}
		}
		if params["text"] == nil {
			params["type"] = "rawKeyDown"
		}
	}
	return PageInput{Method: "Input.dispatchKeyEvent", Params: params}, true
}

// pageClick is a click (count rising for a double click) at window points.
func pageClick(x, y float64, button, count int) []PageInput {
	name := [...]string{"left", "middle", "right"}[min(max(button, 0), 2)]
	at := func(kind string) PageInput {
		return PageInput{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": kind, "x": x, "y": y, "button": name, "clickCount": count}}
	}
	return []PageInput{at("mousePressed"), at("mouseReleased")}
}

// pageDrag presses at (x, y), moves by (dx, dy) and lets go, in window points.
func pageDrag(x, y, dx, dy float64) []PageInput {
	event := func(kind string, x, y float64) PageInput {
		return PageInput{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": kind, "x": x, "y": y, "button": "left", "buttons": 1, "clickCount": 1}}
	}
	return []PageInput{event("mousePressed", x, y), event("mouseMoved", x+dx/2, y+dy/2), event("mouseMoved", x+dx, y+dy), event("mouseReleased", x+dx, y+dy)}
}

// pageWheel is one wheel notch at window points: positive dy scrolls down.
func pageWheel(x, y float64, dx, dy int) PageInput {
	return PageInput{Method: "Input.dispatchMouseEvent", Params: map[string]any{"type": "mouseWheel", "x": x, "y": y, "deltaX": dx * 100, "deltaY": dy * 100}}
}
