package vdisplay

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var namedKeys = map[string]uint32{
	"enter": 0xff0d, "return": 0xff0d, "tab": 0xff09, "escape": 0xff1b, "esc": 0xff1b,
	"backspace": 0xff08, "delete": 0xffff, "space": 0x20,
	"left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"home": 0xff50, "end": 0xff57, "pageup": 0xff55, "pagedown": 0xff56,
}

var modifierKeys = map[string]uint32{
	"ctrl": 0xffe3, "control": 0xffe3, "shift": 0xffe1, "alt": 0xffe9, "super": 0xffeb, "meta": 0xffeb,
}

// Chord turns "ctrl+shift+t" or "Enter" into the modifiers to hold and the
// key to press.
func Chord(spec string) (mods []uint32, key uint32, err error) {
	parts := strings.Split(strings.TrimSpace(spec), "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return nil, 0, fmt.Errorf("no key in %q", spec)
	}
	for _, p := range parts[:len(parts)-1] {
		sym, ok := modifierKeys[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			return nil, 0, fmt.Errorf("%q is not a modifier (ctrl, shift, alt, super)", p)
		}
		mods = append(mods, sym)
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	lower := strings.ToLower(last)
	if sym, ok := namedKeys[lower]; ok {
		return mods, sym, nil
	}
	if strings.HasPrefix(lower, "f") {
		if n, e := strconv.Atoi(lower[1:]); e == nil && n >= 1 && n <= 24 {
			return mods, 0xffbe + uint32(n-1), nil
		}
	}
	if r := []rune(last); len(r) == 1 {
		return mods, Keysym(r[0]), nil
	}
	return nil, 0, fmt.Errorf("unknown key %q", last)
}

// Keysym is the X keysym that types a character.
func Keysym(r rune) uint32 {
	if r == '\n' {
		return 0xff0d
	}
	if r == '\t' {
		return 0xff09
	}
	if r < 0x100 {
		return uint32(r)
	}
	return 0x01000000 + uint32(r)
}

// TypeText types text one character at a time.
func (c *Client) TypeText(text string) error {
	for _, r := range text {
		sym := Keysym(r)
		if err := c.Key(sym, true); err != nil {
			return err
		}
		if err := c.Key(sym, false); err != nil {
			return err
		}
		// Apps collapse the same key repeated back to back (kcalc lost the
		// second 9 of 99): give each press a moment.
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

// Press presses a chord such as "ctrl+a".
func (c *Client) Press(spec string) error {
	mods, key, err := Chord(spec)
	if err != nil {
		return err
	}
	for _, m := range mods {
		if err := c.Key(m, true); err != nil {
			return err
		}
	}
	if err := c.Key(key, true); err != nil {
		return err
	}
	if err := c.Key(key, false); err != nil {
		return err
	}
	for i := len(mods) - 1; i >= 0; i-- {
		if err := c.Key(mods[i], false); err != nil {
			return err
		}
	}
	return nil
}
