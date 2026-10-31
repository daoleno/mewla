package screen

import (
	"fmt"
	"image"
	"strings"
)

// appRow is one line of an app read: an element an agent is shown.
type appRow struct {
	id    int
	line  string // the whole line, indented
	title string
	value string
	// window is a top-level element of the read: an app window.
	window bool
}

// appRows are the elements of a read that are worth a line, in reading
// order, however many: the element's id, role, name, value, actions, state
// and the point to click.
func appRows(tree ElementTree, screen image.Rectangle, toAgent func(image.Point) (int, int)) []appRow {
	var rows []appRow
	for i, e := range tree.Elements {
		title, value := oneLine(e.Title), oneLine(e.Value)
		if e.Role == "AXSecureTextField" {
			value = ""
		}
		special := false
		for _, action := range e.Actions {
			special = special || !commonActions[action]
		}
		if title == "" && value == "" && !actionableRoles[e.Role] && !special && e.Role != "AXWindow" {
			continue
		}
		line := strings.Repeat("  ", min(e.Depth, 8)) + fmt.Sprintf("[%d] %s", tree.id(i), strings.TrimPrefix(e.Role, "AX"))
		if title != "" {
			line += ` "` + title + `"`
		}
		if value != "" {
			line += ` = "` + value + `"`
		}
		if len(e.Actions) > 0 {
			names := make([]string, len(e.Actions))
			for j, action := range e.Actions {
				names[j] = strings.TrimPrefix(action, "AX")
			}
			line += " {" + strings.Join(names, ", ") + "}"
		}
		if !e.Enabled {
			line += " (disabled)"
		}
		if e.Focused {
			line += " (focused)"
		}
		if !e.Frame.Empty() && e.Frame.Overlaps(screen) {
			x, y := toAgent(image.Pt((e.Frame.Min.X+e.Frame.Max.X)/2, (e.Frame.Min.Y+e.Frame.Max.Y)/2))
			line += fmt.Sprintf(" at (%d, %d)", x, y)
		}
		rows = append(rows, appRow{id: tree.id(i), line: line, title: title, value: value, window: e.Depth == 0})
	}
	return rows
}

// A change is answered as a difference only while that is smaller than the
// whole read: up to diffMaxRows lines, and no more than half the app's rows.
const diffMaxRows = 30

// windowIDs are the ids of an app's top-level elements, its windows.
func windowIDs(tree ElementTree) map[int]bool {
	ids := map[int]bool{}
	for i, e := range tree.Elements {
		if e.Depth == 0 {
			ids[tree.id(i)] = true
		}
	}
	return ids
}

// focusedElement is the first focused element of a read, or -1.
func focusedElement(tree ElementTree) int {
	for i, e := range tree.Elements {
		if e.Focused {
			return i
		}
	}
	return -1
}

func describeElement(tree ElementTree, i int) string {
	e := tree.Elements[i]
	text := fmt.Sprintf("[%d] %s", tree.id(i), strings.TrimPrefix(e.Role, "AX"))
	if title := oneLine(e.Title); title != "" {
		text += ` "` + title + `"`
	}
	return text
}

// diffAnswer says what changed between two reads of one app, by element id:
// rows added, gone and changed, and where the focus went. ok is false when a
// difference would mislead or save little, and why says so: the caller then
// gives the whole read.
func diffAnswer(before, after ElementTree, beforeRows, afterRows []appRow) (text string, stats readStats, why string, ok bool) {
	switch {
	case before.PID != after.PID:
		return "", readStats{}, "it is another app", false
	case before.Truncated || after.Truncated:
		// An element missing from a partial read may only be unread.
		return "", readStats{}, "the app has more than was read, so a comparison could miss things", false
	}
	was, now := windowIDs(before), windowIDs(after)
	if len(was) != len(now) {
		return "", readStats{}, "a window opened or closed", false
	}
	for id := range was {
		if !now[id] {
			return "", readStats{}, "a window opened or closed", false
		}
	}
	old := make(map[int]appRow, len(beforeRows))
	for _, row := range beforeRows {
		old[row.id] = row
	}
	current := make(map[int]bool, len(afterRows))
	var lines []string
	for _, row := range afterRows {
		current[row.id] = true
		prev, existed := old[row.id]
		switch {
		case !existed:
			lines = append(lines, "+ "+strings.TrimLeft(row.line, " "))
		case prev.line != row.line:
			line := "~ " + strings.TrimLeft(row.line, " ")
			switch {
			case prev.value != row.value:
				line += fmt.Sprintf(` (was = "%s")`, prev.value)
			case prev.title != row.title:
				line += fmt.Sprintf(` (was "%s")`, prev.title)
			}
			lines = append(lines, line)
		}
	}
	for _, row := range beforeRows {
		if !current[row.id] {
			lines = append(lines, "- "+strings.TrimLeft(row.line, " "))
		}
	}
	if len(lines) > diffMaxRows || len(lines)*2 > len(afterRows) {
		return "", readStats{}, "much of it changed", false
	}
	var b strings.Builder
	if len(lines) == 0 {
		b.WriteString("Nothing app_state shows changed.\n")
	} else {
		b.WriteString("Changed since your last read (+ new, ~ changed, - gone):\n")
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
	}
	// Focus is in the rows too; said once, it is found without searching.
	was0, now0 := focusedElement(before), focusedElement(after)
	switch {
	case now0 >= 0 && was0 >= 0 && after.id(now0) == before.id(was0):
		fmt.Fprintf(&b, "Focus: still %s.\n", describeElement(after, now0))
	case now0 >= 0 && was0 >= 0:
		fmt.Fprintf(&b, "Focus: now %s (was %s).\n", describeElement(after, now0), describeElement(before, was0))
	case now0 >= 0:
		fmt.Fprintf(&b, "Focus: now %s.\n", describeElement(after, now0))
	case was0 >= 0:
		fmt.Fprintf(&b, "Focus: none now (was %s).\n", describeElement(before, was0))
	}
	b.WriteString("Rows not listed are as before and keep their ids; app_state reads the whole app.\n")
	return b.String(), readStats{rows: len(lines)}, "", true
}
