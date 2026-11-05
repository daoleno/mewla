package iphone

import (
	"strings"
	"unicode"

	"github.com/daoleno/mewla/daemon/phone"
)

// The iPhone's keyboard, read as text, is a few dozen single letters in rows
// at the bottom of the screen. Its keys are for fingers; an agent types with
// the text op, so they are left out of the reading (in a keyboard window the
// phone package does not show) and the reading says the keyboard is open.
const (
	// keyboardTop is where on the screen, from the top, the keyboard may begin.
	keyboardTop = 0.5
	// keyboardKeys is how many key-like lines make a keyboard.
	keyboardKeys = 8
	// windowTypeInputMethod and windowTypeApplication are the phone package's window types.
	windowTypeInputMethod = 2
	windowTypeApplication = 1
)

var keyRows = []string{"qwertyuiop", "asdfghjkl", "zxcvbnm", "1234567890"}

// keyLike says whether a line of text reads as keys of a keyboard: one or
// two letters or digits, or a row of single letters.
func keyLike(text string) bool {
	t := strings.ToLower(strings.Join(strings.Fields(text), ""))
	if t == "" {
		return false
	}
	runes := []rune(t)
	if len(runes) <= 2 {
		for _, r := range runes {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		}
		return runes[0] < unicode.MaxASCII
	}
	for _, row := range keyRows {
		if len(t) >= 3 && strings.Contains(row, t) {
			return true
		}
	}
	return false
}

// ToScreen turns the text read off a w x h picture into the phone package's
// reading of a screen, scale picture pixels to one screen pixel.
func ToScreen(lines []Line, w, h int, scale float64) *phone.Screen {
	if scale <= 0 {
		scale = 1
	}
	sw, sh := int(float64(w)/scale+0.5), int(float64(h)/scale+0.5)
	screen := &phone.Screen{W: sw, H: sh}
	app := phone.Window{ID: 1, Type: windowTypeApplication, Active: true, Pkg: "iPhone", Bounds: [4]int{0, 0, sw, sh}}
	keyboard := phone.Window{ID: 2, Type: windowTypeInputMethod, Pkg: "keyboard", Bounds: [4]int{0, sh / 2, sw, sh}}
	var keys []phone.Node
	for _, l := range lines {
		text := strings.TrimSpace(l.Text)
		if text == "" || l.Box.Empty() {
			continue
		}
		b := l.Box
		node := phone.Node{
			C: "Text", T: text, F: phone.FlagEnabled,
			B: [4]int{int(float64(b.Min.X)/scale + 0.5), int(float64(b.Min.Y)/scale + 0.5), int(float64(b.Max.X)/scale + 0.5), int(float64(b.Max.Y)/scale + 0.5)},
		}
		if float64(b.Min.Y) >= float64(h)*keyboardTop && keyLike(text) {
			keys = append(keys, node)
			continue
		}
		app.Nodes = append(app.Nodes, node)
	}
	if len(keys) >= keyboardKeys {
		keyboard.Nodes = keys
		screen.Windows = append(screen.Windows, keyboard)
	} else {
		// A few short words at the bottom are the app's, not keys.
		app.Nodes = mergeByPlace(app.Nodes, keys)
	}
	if len(app.Nodes) > 0 {
		screen.Windows = append([]phone.Window{app}, screen.Windows...)
	}
	return screen
}

// mergeByPlace puts extra nodes among nodes in reading order.
func mergeByPlace(nodes, extra []phone.Node) []phone.Node {
	if len(extra) == 0 {
		return nodes
	}
	out := make([]phone.Node, 0, len(nodes)+len(extra))
	i, j := 0, 0
	for i < len(nodes) || j < len(extra) {
		switch {
		case j == len(extra):
			out = append(out, nodes[i])
			i++
		case i == len(nodes):
			out = append(out, extra[j])
			j++
		case before(extra[j].B, nodes[i].B):
			out = append(out, extra[j])
			j++
		default:
			out = append(out, nodes[i])
			i++
		}
	}
	return out
}

func before(a, b [4]int) bool {
	if a[1] != b[1] {
		return a[1] < b[1]
	}
	return a[0] < b[0]
}
