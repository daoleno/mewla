package ghosttyvt

import (
	"fmt"
	"unicode/utf8"
)

// keyName is a key EncodeKey knows by name; keyText is a key that types a
// character.
type keyName int

const (
	keyText keyName = iota
	keyEnter
	keyEscape
	keyTab
	keySpace
	keyBackspace
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyDelete
	keyInsert
	keyF1 // keyF1 + n-1 is Fn, up to F12
)

const maxFunctionKey = 12

// keyPress is a parsed key: a named key or a character, with modifiers.
type keyPress struct {
	name  keyName
	text  rune // for keyText: the character the key types without modifiers
	ctrl  bool
	alt   bool
	shift bool
}

var namedKeys = map[string]keyPress{
	"Enter":    {name: keyEnter},
	"Escape":   {name: keyEscape},
	"Tab":      {name: keyTab},
	"BTab":     {name: keyTab, shift: true},
	"Space":    {name: keySpace},
	"BSpace":   {name: keyBackspace},
	"Up":       {name: keyUp},
	"Down":     {name: keyDown},
	"Left":     {name: keyLeft},
	"Right":    {name: keyRight},
	"Home":     {name: keyHome},
	"End":      {name: keyEnd},
	"PageUp":   {name: keyPageUp},
	"PgUp":     {name: keyPageUp},
	"PPage":    {name: keyPageUp},
	"PageDown": {name: keyPageDown},
	"PgDn":     {name: keyPageDown},
	"NPage":    {name: keyPageDown},
	"DC":       {name: keyDelete},
	"IC":       {name: keyInsert},
}

// parseKey reads a key in tmux's notation: an optional chain of C-, M- and
// S- modifier prefixes, then a key name (Enter, Escape, Tab, BTab, Space,
// BSpace, Up, Down, Left, Right, Home, End, PageUp/PPage, PageDown/NPage, DC,
// IC, F1-F12) or one character.
func parseKey(spec string) (keyPress, error) {
	rest := spec
	var press keyPress
	for len(rest) > 2 && rest[1] == '-' {
		switch rest[0] {
		case 'C', 'c':
			press.ctrl = true
		case 'M', 'm':
			press.alt = true
		case 'S', 's':
			press.shift = true
		default:
			return keyPress{}, fmt.Errorf("key %q: unknown modifier %q", spec, rest[:2])
		}
		rest = rest[2:]
	}
	if named, ok := namedKeys[rest]; ok {
		named.ctrl = named.ctrl || press.ctrl
		named.alt = named.alt || press.alt
		named.shift = named.shift || press.shift
		return named, nil
	}
	if len(rest) >= 2 && rest[0] == 'F' {
		var n int
		if _, err := fmt.Sscanf(rest, "F%d", &n); err == nil && n >= 1 && n <= maxFunctionKey && rest == fmt.Sprintf("F%d", n) {
			press.name = keyF1 + keyName(n-1)
			return press, nil
		}
	}
	r, size := utf8.DecodeRuneInString(rest)
	if rest == "" || size != len(rest) || r == utf8.RuneError || r < 0x20 || r == 0x7f {
		return keyPress{}, fmt.Errorf("key %q: not a key name or one printable character", spec)
	}
	press.name = keyText
	if r >= 'A' && r <= 'Z' {
		if press.ctrl {
			// C-A is C-a, as in tmux.
			r = r - 'A' + 'a'
		} else {
			press.shift = true
		}
	}
	press.text = r
	return press, nil
}

// unshifted is the character a text key types without shift.
func (k keyPress) unshifted() rune {
	if k.text >= 'A' && k.text <= 'Z' {
		return k.text - 'A' + 'a'
	}
	return k.text
}

// ValidKey reports whether EncodeKey would accept the key name.
func ValidKey(spec string) error {
	_, err := parseKey(spec)
	return err
}
