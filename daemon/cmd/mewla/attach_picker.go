package main

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type pickerKeyKind int

const (
	keyChar pickerKeyKind = iota
	keyUp
	keyDown
	keyPageUp
	keyPageDown
	keyEnter
	keyBackspace
	keyClear // Ctrl-U
	keyCancel
)

type pickerKey struct {
	kind pickerKeyKind
	char rune
}

// parsePickerKeys reads the keys in one chunk of terminal input: the arrow,
// page, Ctrl-P/Ctrl-N, Enter, Backspace, Ctrl-U and cancel keys, and typed
// characters.
func parsePickerKeys(data []byte) []pickerKey {
	var keys []pickerKey
	for len(data) > 0 {
		b := data[0]
		switch {
		case b == 0x1b:
			if len(data) >= 3 && (data[1] == '[' || data[1] == 'O') {
				rest := data[2:]
				n := 3
				switch rest[0] {
				case 'A':
					keys = append(keys, pickerKey{kind: keyUp})
				case 'B':
					keys = append(keys, pickerKey{kind: keyDown})
				case '5', '6':
					// ESC [ 5 ~ and ESC [ 6 ~
					kind := keyPageUp
					if rest[0] == '6' {
						kind = keyPageDown
					}
					keys = append(keys, pickerKey{kind: kind})
					if len(rest) >= 2 && rest[1] == '~' {
						n = 4
					}
				default:
					// Some other escape sequence: skip it whole.
					for n < len(data) && !(data[n-1] >= '@' && data[n-1] <= '~') {
						n++
					}
				}
				data = data[min(n, len(data)):]
				continue
			}
			keys = append(keys, pickerKey{kind: keyCancel})
			data = data[1:]
		case b == '\r' || b == '\n':
			keys = append(keys, pickerKey{kind: keyEnter})
			data = data[1:]
		case b == 0x03 || b == 0x04 || b == 0x1c: // Ctrl-C, Ctrl-D, Ctrl-\
			keys = append(keys, pickerKey{kind: keyCancel})
			data = data[1:]
		case b == 0x10: // Ctrl-P
			keys = append(keys, pickerKey{kind: keyUp})
			data = data[1:]
		case b == 0x0e: // Ctrl-N
			keys = append(keys, pickerKey{kind: keyDown})
			data = data[1:]
		case b == 0x15:
			keys = append(keys, pickerKey{kind: keyClear})
			data = data[1:]
		case b == 0x7f || b == 0x08:
			keys = append(keys, pickerKey{kind: keyBackspace})
			data = data[1:]
		case b < 0x20:
			data = data[1:]
		default:
			r, size := utf8.DecodeRune(data)
			if r != utf8.RuneError {
				keys = append(keys, pickerKey{kind: keyChar, char: r})
			}
			data = data[size:]
		}
	}
	return keys
}

// filterAttachEntries keeps the entries every space-separated word of query
// appears in (any case), over name, agent, state, cwd and id.
func filterAttachEntries(entries []attachEntry, query string) []attachEntry {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return entries
	}
	var out []attachEntry
	for _, entry := range entries {
		haystack := strings.ToLower(strings.Join([]string{entry.Name, entry.Agent, entry.State, entry.Cwd, entry.ID}, " "))
		match := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				match = false
				break
			}
		}
		if match {
			out = append(out, entry)
		}
	}
	return out
}

// pickerModel is the picker's state: the filter typed and the row chosen.
type pickerModel struct {
	entries []attachEntry
	query   []rune
	sel     int
	offset  int
}

func (m *pickerModel) shown() []attachEntry {
	return filterAttachEntries(m.entries, string(m.query))
}

// clamp keeps the chosen row inside the shown ones and the window around it.
func (m *pickerModel) clamp(rows int) {
	count := len(m.shown())
	if m.sel >= count {
		m.sel = count - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
	if rows < 1 {
		rows = 1
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+rows {
		m.offset = m.sel - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// apply handles one key and reports whether the picker is done, and whether it
// ended in a choice (the chosen entry) or a cancel.
func (m *pickerModel) apply(key pickerKey, rows int) (done bool, chosen *attachEntry) {
	switch key.kind {
	case keyUp:
		m.sel--
	case keyDown:
		m.sel++
	case keyPageUp:
		m.sel -= rows
	case keyPageDown:
		m.sel += rows
	case keyChar:
		m.query = append(m.query, key.char)
		m.sel, m.offset = 0, 0
	case keyBackspace:
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.sel, m.offset = 0, 0
		}
	case keyClear:
		m.query = nil
		m.sel, m.offset = 0, 0
	case keyCancel:
		return true, nil
	case keyEnter:
		shown := m.shown()
		if len(shown) == 0 {
			return false, nil
		}
		m.clamp(rows)
		entry := shown[m.sel]
		return true, &entry
	}
	m.clamp(rows)
	return false, nil
}

// cutTo shortens text to width columns, ending it with … when cut. keepEnd
// cuts from the left instead, for paths, whose end matters most.
func cutTo(text string, width int, keepEnd bool) string {
	runes := []rune(text)
	if width <= 0 {
		return ""
	}
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	if keepEnd {
		return "…" + string(runes[len(runes)-width+1:])
	}
	return string(runes[:width-1]) + "…"
}

func padTo(text string, width int) string {
	if n := utf8.RuneCountInString(text); n < width {
		return text + strings.Repeat(" ", width-n)
	}
	return text
}

// pickerRows is how many Session rows fit under the two header lines.
func pickerRows(height int) int { return max(height-3, 1) }

// renderPicker draws the picker for a screen of width x height: a title with
// the keys, the filter, then one line per shown Session (name, agent, state,
// cwd, age), the chosen one marked. Lines end in \r\n for a raw terminal.
func renderPicker(m *pickerModel, width, height int, style termStyle, now time.Time) string {
	if width < 20 {
		width = 20
	}
	rows := pickerRows(height)
	m.clamp(rows)
	shown := m.shown()

	nameW, agentW, stateW, ageW := 4, 5, 5, 3
	for _, entry := range m.entries {
		nameW = max(nameW, min(utf8.RuneCountInString(entry.Name), 28))
		agentW = max(agentW, min(utf8.RuneCountInString(dash(entry.Agent)), 10))
		stateW = max(stateW, utf8.RuneCountInString(entry.State))
		ageW = max(ageW, utf8.RuneCountInString(humanAge(entry.Created, now)))
	}
	// Two columns of marker, two spaces between columns.
	cwdW := width - (2 + nameW + 2 + agentW + 2 + stateW + 2 + 2 + ageW)
	if cwdW < 8 {
		cwdW = 8
	}
	// A column no wider than its longest path keeps the age next to it.
	longest := 1
	for _, entry := range m.entries {
		longest = max(longest, utf8.RuneCountInString(dash(homeShortened(entry.Cwd))))
	}
	cwdW = min(cwdW, longest)

	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")
	b.WriteString(style.title("Attach to a Session") + style.dim(cutTo("   ↑↓ move · type to filter · Enter attach · Esc cancel", width-len("■ Attach to a Session"), false)))
	b.WriteString("\r\n")
	b.WriteString("› " + string(m.query))
	cursor := 2 + len(m.query)
	b.WriteString("\r\n")
	if len(shown) == 0 {
		if len(m.entries) == 0 {
			b.WriteString(style.dim("No live Sessions."))
		} else {
			b.WriteString(style.dim("No Session matches."))
		}
		b.WriteString("\r\n")
	}
	end := min(len(shown), m.offset+rows)
	for i := m.offset; i < end; i++ {
		entry := shown[i]
		line := padTo(cutTo(entry.Name, nameW, false), nameW) + "  " +
			padTo(cutTo(dash(entry.Agent), agentW, false), agentW) + "  " +
			padTo(entry.State, stateW) + "  " +
			padTo(cutTo(dash(homeShortened(entry.Cwd)), cwdW, true), cwdW) + "  " +
			humanAge(entry.Created, now)
		line = cutTo(line, width-2, false)
		if i == m.sel {
			b.WriteString(style.wrap("7", "› "+line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\r\n")
	}
	// Put the cursor at the end of the filter.
	fmt.Fprintf(&b, "\x1b[2;%dH", cursor+1)
	return b.String()
}

// runPicker lets the person pick one of entries and reports it; nil when they
// cancel or input ends. It reads input itself, one chunk at a time, and leaves
// nothing reading when it returns, so what is typed next belongs to the caller.
func runPicker(in io.Reader, out io.Writer, entries []attachEntry, size func() (int, int), style termStyle, now func() time.Time) (*attachEntry, error) {
	m := &pickerModel{entries: entries}
	io.WriteString(out, enterAttach)
	defer io.WriteString(out, leaveAttach)
	buf := make([]byte, 256)
	for {
		cols, rows := size()
		if cols <= 0 || rows <= 0 {
			cols, rows = 80, 24
		}
		io.WriteString(out, renderPicker(m, cols, rows, style, now()))
		n, err := in.Read(buf)
		for _, key := range parsePickerKeys(buf[:n]) {
			if done, chosen := m.apply(key, pickerRows(rows)); done {
				return chosen, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, err
		}
	}
}
