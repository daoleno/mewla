package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/rivo/uniseg"
)

// backKey (Ctrl-]) returns from a Session opened out of the grid to the grid.
const backKey = 0x1d

const (
	gridMinTileW = 36 // narrowest tile, in columns
	gridMinTileH = 7  // shortest tile: a header and six rows of screen
	gridGap      = 1  // columns between tiles
	gridMouseOn  = "\x1b[?1000h\x1b[?1006h"
	gridMouseOff = "\x1b[?1006l\x1b[?1000l"
)

// gridLayout is how many tiles a terminal holds and where they go.
type gridLayout struct {
	Cols, Rows   int
	TileW, TileH int
	Shown        int // tiles drawn: the newest ones
}

// layoutGrid fits n tiles into a width x height terminal, the last row being
// the status line. Tiles are never smaller than gridMinTileW x gridMinTileH:
// more sessions than fit show the newest ones and the rest are left out.
func layoutGrid(n, width, height int) gridLayout {
	usable := height - 1
	maxCols := max((width+gridGap)/(gridMinTileW+gridGap), 1)
	maxRows := max(usable/gridMinTileH, 1)
	shown := min(n, maxCols*maxRows)
	if shown <= 0 {
		return gridLayout{}
	}
	// The fewest empty tiles wins, then the tile shape nearest to 3 columns
	// per row (a terminal cell is about twice as tall as it is wide).
	bestCols, bestEmpty, bestShape := 1, 1<<30, 1e9
	for cols := 1; cols <= min(maxCols, shown); cols++ {
		rows := (shown + cols - 1) / cols
		if rows > maxRows {
			continue
		}
		tileW := (width - (cols-1)*gridGap) / cols
		tileH := usable / rows
		empty := cols*rows - shown
		shape := float64(tileW)/float64(tileH) - 3.2
		if shape < 0 {
			shape = -shape
		}
		if empty < bestEmpty || empty == bestEmpty && shape < bestShape {
			bestCols, bestEmpty, bestShape = cols, empty, shape
		}
	}
	rows := (shown + bestCols - 1) / bestCols
	return gridLayout{
		Cols: bestCols, Rows: rows, Shown: shown,
		TileW: (width - (bestCols-1)*gridGap) / bestCols,
		TileH: usable / rows,
	}
}

// origin is the 1-based terminal row and column of tile i's top-left corner.
func (l gridLayout) origin(i int) (row, col int) {
	return (i/l.Cols)*l.TileH + 1, (i%l.Cols)*(l.TileW+gridGap) + 1
}

// tileAt is the tile under a 1-based terminal position, or -1.
func (l gridLayout) tileAt(row, col int) int {
	if l.Cols == 0 || row < 1 || col < 1 {
		return -1
	}
	r := (row - 1) / l.TileH
	c := (col - 1) / (l.TileW + gridGap)
	if r >= l.Rows || c >= l.Cols || (col-1)%(l.TileW+gridGap) >= l.TileW {
		return -1
	}
	if i := r*l.Cols + c; i < l.Shown {
		return i
	}
	return -1
}

// move is the tile reached from i by one arrow press, or i at an edge.
func (l gridLayout) move(i int, key pickerKeyKind) int {
	next := i
	switch key {
	case keyUp:
		next = i - l.Cols
	case keyDown:
		next = i + l.Cols
	case keyLeft:
		next = i - 1
	case keyRight:
		next = i + 1
	}
	// Down from a column whose last row is short lands on the last tile.
	if key == keyDown && next >= l.Shown && i/l.Cols < (l.Shown-1)/l.Cols {
		next = l.Shown - 1
	}
	if next < 0 || next >= l.Shown {
		return i
	}
	return next
}

// escapeLen is the length of the escape sequence that starts s.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	case 'O', '(', ')', '*', '+', '#':
		return min(3, len(s))
	default:
		return 2
	}
	return len(s)
}

// cutStyled is a row of styled screen text cut to exactly width columns: its
// colors are kept, every other escape sequence is dropped, and it ends in a
// reset so a tile never colors its neighbour.
func cutStyled(row string, width int) string {
	var b strings.Builder
	used := 0
	for i := 0; i < len(row); {
		c := row[i]
		if c == 0x1b {
			n := escapeLen(row[i:])
			seq := row[i : i+n]
			switch {
			case n > 2 && seq[1] == '[' && seq[n-1] == 'm':
				b.WriteString(seq)
			case n > 2 && seq[1] == '[' && seq[n-1] == 'C':
				// The host writes a run of empty cells as "cursor forward".
				skip := 1
				if v, err := strconv.Atoi(seq[2 : n-1]); err == nil && v > 0 {
					skip = v
				}
				skip = min(skip, width-used)
				b.WriteString(strings.Repeat(" ", skip))
				used += skip
			}
			i += n
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		cluster, _, w, _ := uniseg.FirstGraphemeClusterInString(row[i:], -1)
		if used+w > width {
			break
		}
		b.WriteString(cluster)
		used += w
		i += len(cluster)
	}
	b.WriteString("\x1b[0m")
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}

// plainText is s as it reads: no escape sequences, and a cursor-forward run
// (how the host writes empty cells) as spaces.
func plainText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			n := escapeLen(s[i:])
			if seq := s[i : i+n]; n > 2 && seq[1] == '[' && seq[n-1] == 'C' {
				skip := 1
				if v, err := strconv.Atoi(seq[2 : n-1]); err == nil && v > 0 {
					skip = v
				}
				b.WriteString(strings.Repeat(" ", skip))
			}
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// gridScreen is the last screen the host reported for a Session.
type gridScreen struct {
	rows    []string // styled rows of the screen, top first
	cursorY int
	seq     uint64
	have    bool
}

func newGridScreen(c sessionhost.Capture) gridScreen {
	text := strings.ReplaceAll(c.Text, "\r\n", "\n")
	return gridScreen{rows: strings.Split(text, "\n"), cursorY: c.CursorY, seq: c.OutputSeq, have: true}
}

// view is the h rows a tile shows: the screen's rows ending at its last
// non-blank row or the cursor, whichever is lower, since what a person
// watches in an agent's terminal is its end. A short screen shows from the top.
func (s gridScreen) view(h int) []string {
	if h <= 0 {
		return nil
	}
	start := s.viewStart(h)
	out := make([]string, 0, h)
	for i := start; i < start+h; i++ {
		if i < len(s.rows) {
			out = append(out, s.rows[i])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// viewStart is the first screen row view(h) shows.
func (s gridScreen) viewStart(h int) int {
	last := len(s.rows) - 1
	for last >= 0 && strings.TrimSpace(plainText(s.rows[last])) == "" {
		last--
	}
	end := max(last, s.cursorY)
	return max(end-h+1, 0)
}

// gridState is what the grid shows: the live Sessions, newest first, the
// screens last read, and which one is chosen.
type gridState struct {
	entries []attachEntry
	screens map[string]gridScreen
	infos   map[string]sessionhost.Info
	sel     string
	// outside counts live Sessions the grid cannot tile (still in tmux).
	outside int
}

func newGridState(outside int) *gridState {
	return &gridState{screens: map[string]gridScreen{}, infos: map[string]sessionhost.Info{}, outside: outside}
}

// update takes the host's current Sessions: new ones join, ended ones leave,
// and the chosen one stays chosen while it lives.
func (g *gridState) update(infos []sessionhost.Info) {
	g.infos = map[string]sessionhost.Info{}
	for _, info := range infos {
		g.infos[info.ID] = info
	}
	g.entries = listedAttachEntries(hostAttachEntries(infos, nil))
	for id := range g.screens {
		if _, live := g.infos[id]; !live {
			delete(g.screens, id)
		}
	}
	if g.index(g.sel) < 0 {
		g.sel = ""
		if len(g.entries) > 0 {
			g.sel = g.entries[0].ID
		}
	}
}

func (g *gridState) index(id string) int {
	for i, entry := range g.entries {
		if entry.ID == id {
			return i
		}
	}
	return -1
}

// stale lists the shown Sessions whose screen is older than the host's output.
func (g *gridState) stale(layout gridLayout) []string {
	var ids []string
	for i := 0; i < layout.Shown && i < len(g.entries); i++ {
		id := g.entries[i].ID
		if screen := g.screens[id]; !screen.have || screen.seq != g.infos[id].OutputSeq {
			ids = append(ids, id)
		}
	}
	return ids
}

// stateColor colors the state words that ask for a look.
func stateColor(style termStyle, state string) string {
	switch state {
	case "needs input":
		return style.wrap("1;33", state)
	case "working":
		return style.wrap("32", state)
	case "error", "exited":
		return style.wrap("31", state)
	}
	return style.dim(state)
}

// tileLines are the h lines of tile i, each exactly w columns: a header (the
// number to press, the name, agent and state) and the end of the screen.
func tileLines(entry attachEntry, number int, screen gridScreen, w, h int, selected bool, style termStyle) []string {
	label := " "
	if number >= 1 && number <= 9 {
		label = strconv.Itoa(number)
	}
	name := label + " " + entry.Name
	middle := " " + dash(entry.Agent) + " · "
	full := name + middle + entry.State
	var head string
	switch {
	case selected:
		// One reversed bar, with a marker for terminals that draw no colors.
		head = style.wrap("7", padTo("›"+cutTo(full, w-1, false), w))
	case utf8.RuneCountInString(full) <= w:
		head = padStyled(style.bold(name)+style.dim(middle)+stateColor(style, entry.State), w)
	default:
		head = padStyled(style.bold(cutTo(full, w, false)), w)
	}
	lines := make([]string, 0, h)
	lines = append(lines, head)
	body := screen.view(h - 1)
	for i := 0; i < h-1; i++ {
		row := ""
		if i < len(body) {
			row = body[i]
		}
		if !screen.have && i == 0 {
			row = "(waiting for the screen)"
		}
		lines = append(lines, cutStyled(row, w))
	}
	return lines
}

// padStyled pads text that may hold escape sequences to w columns.
func padStyled(text string, w int) string {
	if n := uniseg.StringWidth(plainText(text)); n < w {
		return text + strings.Repeat(" ", w-n)
	}
	return text
}

// gridStatus is the last line: what is left out, then the keys.
func gridStatus(g *gridState, layout gridLayout, width int) string {
	var parts []string
	if hidden := len(g.entries) - layout.Shown; hidden > 0 {
		parts = append(parts, fmt.Sprintf("newest %d of %d shown, %d more need a bigger terminal", layout.Shown, len(g.entries), hidden))
	}
	if g.outside > 0 {
		parts = append(parts, fmt.Sprintf("%d in tmux not tiled", g.outside))
	}
	parts = append(parts, "arrows/Tab move · Enter/1-9/click open · in a Session Ctrl-] back · q/Ctrl-\\ leave")
	return cutTo(strings.Join(parts, " · "), width, false)
}

// renderGrid draws every tile at its place and the status line. clear starts
// the screen over, for when the layout changed.
func renderGrid(g *gridState, width, height int, clear bool, style termStyle) (string, gridLayout) {
	layout := layoutGrid(len(g.entries), width, height)
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[0m")
	if clear {
		b.WriteString("\x1b[2J")
	}
	if layout.Shown == 0 {
		fmt.Fprintf(&b, "\x1b[1;1H\x1b[K%s", style.dim("No live Sessions."))
	}
	for i := 0; i < layout.Shown; i++ {
		entry := g.entries[i]
		row, col := layout.origin(i)
		for n, line := range tileLines(entry, i+1, g.screens[entry.ID], layout.TileW, layout.TileH, entry.ID == g.sel, style) {
			fmt.Fprintf(&b, "\x1b[%d;%dH%s\x1b[0m", row+n, col, line)
		}
	}
	fmt.Fprintf(&b, "\x1b[%d;1H\x1b[0m\x1b[2K%s", height, style.dim(gridStatus(g, layout, width)))
	return b.String(), layout
}

type gridKeyKind int

const (
	gridNone gridKeyKind = iota
	gridMove
	gridNext
	gridPrev
	gridOpen   // Enter: the chosen tile
	gridNumber // 1-9: tile n
	gridClick  // a mouse press at row, col
	gridLeave
)

type gridKey struct {
	kind     gridKeyKind
	move     pickerKeyKind
	n        int
	row, col int
}

// parseGridKeys reads one chunk of grid input: arrows, Tab, Enter, digits,
// leave keys and left mouse clicks (SGR reports).
func parseGridKeys(data []byte) []gridKey {
	var keys []gridKey
	for len(data) > 0 {
		b := data[0]
		switch {
		case b == 0x1b && len(data) >= 3 && (data[1] == '[' || data[1] == 'O'):
			n := escapeLen(string(data))
			seq := string(data[:n])
			switch {
			case seq == "\x1b[A" || seq == "\x1bOA":
				keys = append(keys, gridKey{kind: gridMove, move: keyUp})
			case seq == "\x1b[B" || seq == "\x1bOB":
				keys = append(keys, gridKey{kind: gridMove, move: keyDown})
			case seq == "\x1b[C" || seq == "\x1bOC":
				keys = append(keys, gridKey{kind: gridMove, move: keyRight})
			case seq == "\x1b[D" || seq == "\x1bOD":
				keys = append(keys, gridKey{kind: gridMove, move: keyLeft})
			case seq == "\x1b[Z":
				keys = append(keys, gridKey{kind: gridPrev})
			case strings.HasPrefix(seq, "\x1b[<") && seq[len(seq)-1] == 'M':
				fields := strings.Split(seq[3:len(seq)-1], ";")
				if len(fields) == 3 {
					button, _ := strconv.Atoi(fields[0])
					col, _ := strconv.Atoi(fields[1])
					row, _ := strconv.Atoi(fields[2])
					if button == 0 {
						keys = append(keys, gridKey{kind: gridClick, row: row, col: col})
					}
				}
			}
			data = data[n:]
		case b == 0x1b || b == 0x03 || b == 0x1c || b == 'q' || b == 'Q':
			keys = append(keys, gridKey{kind: gridLeave})
			data = data[1:]
		case b == '\t':
			keys = append(keys, gridKey{kind: gridNext})
			data = data[1:]
		case b == '\r' || b == '\n':
			keys = append(keys, gridKey{kind: gridOpen})
			data = data[1:]
		case b >= '1' && b <= '9':
			keys = append(keys, gridKey{kind: gridNumber, n: int(b - '0')})
			data = data[1:]
		default:
			data = data[1:]
		}
	}
	return keys
}

// gridHost is the part of the session host the grid reads.
type gridHost interface {
	List(ctx context.Context) ([]sessionhost.Info, error)
	Capture(ctx context.Context, params sessionhost.CaptureParams) (sessionhost.Capture, error)
}

// gridPoll is how often the grid asks the host what changed.
const gridPoll = 400 * time.Millisecond

// runGridView shows every live host Session as a tile and keeps them current
// until the person opens one (its id) or leaves (""). It never resizes or types
// into a Session: it reads each one's screen from the host's own terminal
// state, and only when the Session has written something new. The terminal's
// size is read again on every poll, so a resize re-tiles on any platform.
func runGridView(ctx context.Context, host gridHost, state *gridState, typed <-chan []byte, out io.Writer, size func() (int, int), resized <-chan os.Signal, style termStyle, poll time.Duration) (string, error) {
	io.WriteString(out, enterAttach+gridMouseOn)
	defer io.WriteString(out, gridMouseOff+"\x1b[?25h")
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var layout gridLayout
	lastW, lastH := -1, -1
	normSize := func() (int, int) {
		cols, rows := size()
		if cols <= 0 || rows <= 0 {
			return 80, 24
		}
		return cols, rows
	}
	signature := func() string {
		var b strings.Builder
		for _, entry := range state.entries {
			b.WriteString(entry.ID + "|" + entry.State + ";")
		}
		return b.String()
	}
	lastSignature := ""
	// refresh asks the host for its Sessions and the screens that moved since
	// they were last read; it reports whether anything on the grid changed.
	refresh := func() (bool, error) {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		infos, err := host.List(callCtx)
		if err != nil {
			return false, err
		}
		state.update(infos)
		cols, rows := normSize()
		layout = layoutGrid(len(state.entries), cols, rows)
		changed := signature() != lastSignature
		lastSignature = signature()
		for _, id := range state.stale(layout) {
			capture, err := host.Capture(callCtx, sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeScreen, Styled: true, Trim: true})
			if err != nil {
				continue // it ended since the list; the next poll drops it
			}
			state.screens[id] = newGridScreen(capture)
			changed = true
		}
		return changed, nil
	}
	draw := func(force bool) {
		cols, rows := normSize()
		resizedNow := force || cols != lastW || rows != lastH
		lastW, lastH = cols, rows
		var frame string
		frame, layout = renderGrid(state, cols, rows, resizedNow, style)
		io.WriteString(out, frame)
	}
	if _, err := refresh(); err != nil {
		return "", err
	}
	draw(true)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-resized:
			draw(true)
		case <-ticker.C:
			changed, err := refresh()
			if err != nil {
				return "", err
			}
			cols, rows := normSize()
			if changed || cols != lastW || rows != lastH {
				draw(false)
			}
		case data, ok := <-typed:
			if !ok {
				return "", nil
			}
			redraw := false
			for _, key := range parseGridKeys(data) {
				idx := state.index(state.sel)
				switch key.kind {
				case gridLeave:
					return "", nil
				case gridMove:
					if idx >= 0 {
						state.sel = state.entries[layout.move(idx, key.move)].ID
						redraw = true
					}
				case gridNext, gridPrev:
					if layout.Shown > 0 {
						step := 1
						if key.kind == gridPrev {
							step = layout.Shown - 1
						}
						state.sel = state.entries[(max(idx, 0)+step)%layout.Shown].ID
						redraw = true
					}
				case gridOpen:
					if idx >= 0 && idx < layout.Shown {
						return state.sel, nil
					}
				case gridNumber:
					if key.n <= layout.Shown {
						return state.entries[key.n-1].ID, nil
					}
				case gridClick:
					if i := layout.tileAt(key.row, key.col); i >= 0 {
						return state.entries[i].ID, nil
					}
				}
			}
			if redraw {
				draw(false)
			}
		}
	}
}

// runGrid tiles the live host Sessions and lets the person open them one at a
// time: Ctrl-] in an opened Session returns to the grid, Ctrl-\ leaves, and a
// Session that ends returns to the grid too. It reports why it ended.
func runGrid(ctx context.Context, client *sessionhost.Client, outside int, typed <-chan []byte, out io.Writer, size func() (int, int), resized <-chan os.Signal, style termStyle) (string, error) {
	state := newGridState(outside)
	for {
		id, err := runGridView(ctx, client, state, typed, out, size, resized, style, gridPoll)
		io.WriteString(out, leaveAttach)
		if err != nil || id == "" {
			return "", err
		}
		outcome, back, err := attachLoopTyped(ctx, client, id, typed, out, size, resized, backKey)
		if err != nil {
			return "", err
		}
		if !back && strings.HasPrefix(outcome, "[detached") {
			return outcome, nil
		}
		// An ended Session, or the key that goes back, shows the grid again.
		state.sel = id
		delete(state.screens, id)
	}
}
