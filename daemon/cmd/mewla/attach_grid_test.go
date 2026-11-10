package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/rivo/uniseg"
)

func TestLayoutGridFitsOrLimitsTiles(t *testing.T) {
	cases := []struct {
		n, w, h        int
		cols, rows, sh int
	}{
		{0, 200, 50, 0, 0, 0},
		{1, 200, 50, 1, 1, 1},
		{4, 200, 50, 2, 2, 4}, // no empty tile
		{6, 200, 50, 3, 2, 6}, // 200 columns hold five 36-wide tiles
		{3, 120, 40, 3, 1, 3}, // one row of three, not two plus a hole
		{9, 80, 24, 2, 3, 6},  // 80x24: two tiles across, three high, the newest six
		{20, 80, 24, 2, 3, 6}, //
		{3, 30, 10, 1, 1, 1},  // smaller than one tile: one tile at the terminal's size
	}
	for _, c := range cases {
		l := layoutGrid(c.n, c.w, c.h)
		if l.Cols != c.cols || l.Rows != c.rows || l.Shown != c.sh {
			t.Errorf("layoutGrid(%d,%d,%d) = %+v, want %dx%d showing %d", c.n, c.w, c.h, l, c.cols, c.rows, c.sh)
		}
		if l.Shown > 0 && (l.TileW*l.Cols+gridGap*(l.Cols-1) > c.w || l.TileH*l.Rows > c.h-1) {
			t.Errorf("layoutGrid(%d,%d,%d) = %+v overflows the terminal", c.n, c.w, c.h, l)
		}
		if l.Shown > 0 && c.w >= gridMinTileW && c.h-1 >= gridMinTileH && (l.TileW < gridMinTileW || l.TileH < gridMinTileH) {
			t.Errorf("layoutGrid(%d,%d,%d) = %+v: tiles under the minimum", c.n, c.w, c.h, l)
		}
	}
}

func TestGridLayoutHitTestAndMove(t *testing.T) {
	l := layoutGrid(6, 120, 40) // 3 across, 2 down
	if l.Cols != 3 || l.Rows != 2 {
		t.Fatalf("layout %+v", l)
	}
	for i := 0; i < l.Shown; i++ {
		row, col := l.origin(i)
		if got := l.tileAt(row, col); got != i {
			t.Errorf("tile %d origin (%d,%d) hits %d", i, row, col, got)
		}
		if got := l.tileAt(row+l.TileH-1, col+l.TileW-1); got != i {
			t.Errorf("tile %d far corner hits %d", i, got)
		}
	}
	_, col := l.origin(1)
	if got := l.tileAt(1, col-1); got != -1 { // the gap between tiles
		t.Errorf("gap hits %d", got)
	}
	if got := l.tileAt(40, 1); got != -1 || l.tileAt(0, 5) != -1 || l.tileAt(1, 500) != -1 {
		t.Errorf("status line or outside hit a tile")
	}
	if l.move(0, keyRight) != 1 || l.move(1, keyLeft) != 0 || l.move(0, keyDown) != 3 || l.move(4, keyUp) != 1 {
		t.Errorf("moves wrong")
	}
	if l.move(0, keyLeft) != 0 || l.move(0, keyUp) != 0 || l.move(5, keyRight) != 5 || l.move(5, keyDown) != 5 {
		t.Errorf("moves leave the grid")
	}
	// Down from a column whose last row is short lands on the last tile.
	short := gridLayout{Cols: 3, Rows: 2, TileW: 36, TileH: 8, Shown: 5}
	if got := short.move(2, keyDown); got != 4 {
		t.Errorf("down into an empty slot moved to %d, want 4", got)
	}
	if got := short.move(4, keyDown); got != 4 {
		t.Errorf("down from the last tile moved to %d", got)
	}
}

func TestCutStyledKeepsColorsDropsOtherSequences(t *testing.T) {
	row := "\x1b[0m\x1b[31mred\x1b[0m \x1b[2Jplain\x1b]0;title\x07 text\x1b[1;1Hmore"
	got := cutStyled(row, 12)
	if !strings.Contains(got, "\x1b[31mred") {
		t.Fatalf("color lost: %q", got)
	}
	if strings.Contains(got, "\x1b[2J") || strings.Contains(got, "title") || strings.Contains(got, "\x1b[1;1H") {
		t.Fatalf("a non-color sequence survived: %q", got)
	}
	if plain := plainText(got); plain != "red plain te" {
		t.Fatalf("visible text %q", plain)
	}
	if w := uniseg.StringWidth(plainText(cutStyled("short", 12))); w != 12 {
		t.Fatalf("a short row is %d wide, want 12", w)
	}
	if !strings.HasSuffix(strings.TrimRight(got, " "), "\x1b[0m") {
		t.Fatalf("a cut row must end by resetting colors: %q", got)
	}
}

func TestCutStyledReadsCursorForwardAsEmptyCells(t *testing.T) {
	// How the host writes "● Refactoring attachLoop so" with default-colored blanks.
	row := "\x1b[5C\x1b[0m\x1b[38;2;255;255;255m●\x1b[1C\x1b[0mRefactoring \x1b[0m\x1b[38;2;1;2;3mattachLoop\x1b[1C\x1b[0mso\x1b[C!"
	if got := plainText(cutStyled(row, 60)); strings.TrimRight(got, " ") != "     ● Refactoring attachLoop so !" {
		t.Fatalf("%q", got)
	}
	if got := plainText(cutStyled(row, 8)); got != "     ● R" {
		t.Fatalf("cut: %q", got)
	}
	if got := plainText(cutStyled("\x1b[200Cx", 10)); got != strings.Repeat(" ", 10) {
		t.Fatalf("a long run is cut at the width: %q", got)
	}
	// A blank row written only as cursor moves still counts as blank.
	screen := gridScreen{rows: []string{"text", "\x1b[40C", ""}}
	if got := strings.Join(screen.view(1), "|"); got != "text" {
		t.Fatalf("view: %q", got)
	}
}

func TestCutStyledWideCharactersNeverOverflow(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 5, 7} {
		got := cutStyled("前面讲的 ab 平铺 ✓", width)
		if w := uniseg.StringWidth(plainText(got)); w != width {
			t.Errorf("width %d: %d columns in %q", width, w, got)
		}
	}
}

func TestGridScreenViewShowsTheEndOfTheScreen(t *testing.T) {
	screen := newGridScreen(sessionhost.Capture{Text: "one\r\ntwo\r\nthree\r\nfour\r\n\r\n\r\n", CursorY: 3})
	if got := strings.Join(screen.view(2), "|"); got != "three|four" {
		t.Fatalf("view(2) = %q", got)
	}
	if got := strings.Join(screen.view(6), "|"); got != "one|two|three|four||" {
		t.Fatalf("view(6) = %q", got)
	}
	// The cursor below the last text keeps the prompt row in view.
	prompt := newGridScreen(sessionhost.Capture{Text: "a\r\nb", CursorY: 4})
	if got := strings.Join(prompt.view(2), "|"); got != "|" {
		t.Fatalf("cursor row: %q", got)
	}
	if got := newGridScreen(sessionhost.Capture{}).view(3); len(got) != 3 {
		t.Fatalf("empty screen: %v", got)
	}
}

func gridInfos(n int) []sessionhost.Info {
	var infos []sessionhost.Info
	for i := 0; i < n; i++ {
		info := hostInfo(fmt.Sprintf("mh-%02d", i), fmt.Sprintf("Worker%02d", i), "claude", time.Duration(i+1)*time.Minute, "idle", nil)
		info.OutputSeq = uint64(10 + i)
		infos = append(infos, info)
	}
	return infos
}

func TestTileLinesAreExactlyTheTileSize(t *testing.T) {
	entry := hostAttachEntries(gridInfos(1), nil)[0]
	screen := newGridScreen(sessionhost.Capture{Text: "\x1b[32mgreen line\x1b[0m\r\n" + strings.Repeat("x", 200), CursorY: 1})
	for _, selected := range []bool{false, true} {
		for _, w := range []int{12, 36, 60} {
			lines := tileLines(entry, 3, screen, w, 8, selected, termStyle{})
			if len(lines) != 8 {
				t.Fatalf("%d lines", len(lines))
			}
			for i, line := range lines {
				if got := uniseg.StringWidth(plainText(line)); got != w {
					t.Errorf("w=%d selected=%v line %d is %d wide: %q", w, selected, i, got, plainText(line))
				}
			}
		}
	}
	head := plainText(tileLines(entry, 3, screen, 60, 8, true, termStyle{})[0])
	if !strings.HasPrefix(head, "›3 Worker00") || !strings.Contains(head, "claude · idle") {
		t.Fatalf("selected header %q", head)
	}
	if head := plainText(tileLines(entry, 10, screen, 60, 8, false, termStyle{})[0]); strings.Contains(head, "10") {
		t.Fatalf("only 1-9 are numbered: %q", head)
	}
	if lines := tileLines(entry, 1, gridScreen{}, 40, 4, false, termStyle{}); !strings.Contains(plainText(lines[1]), "waiting for the screen") {
		t.Fatalf("a tile with no screen yet: %q", lines[1])
	}
}

func TestGridStatusSaysWhatIsLeftOut(t *testing.T) {
	g := newGridState(2)
	g.update(gridInfos(9))
	layout := layoutGrid(len(g.entries), 80, 24)
	status := gridStatus(g, layout, 200)
	for _, want := range []string{"newest 6 of 9 shown", "3 more", "2 in tmux not tiled", "Ctrl-] back"} {
		if !strings.Contains(status, want) {
			t.Errorf("status lacks %q: %s", want, status)
		}
	}
	g.update(gridInfos(2))
	if status := gridStatus(g, layoutGrid(2, 200, 50), 200); strings.Contains(status, "newest") {
		t.Errorf("nothing is left out but the status says so: %s", status)
	}
}

func TestParseGridKeys(t *testing.T) {
	keys := parseGridKeys([]byte("\x1b[A\x1b[B\x1b[C\x1b[D\x1bOA\t\x1b[Z\r3q\x1c\x03\x1b[<0;15;4M\x1b[<0;15;4m\x1b[<2;5;5M\x1b[<64;5;5Mz"))
	var got []string
	for _, k := range keys {
		switch k.kind {
		case gridMove:
			got = append(got, fmt.Sprintf("move%d", k.move))
		case gridClick:
			got = append(got, fmt.Sprintf("click%d,%d", k.row, k.col))
		case gridNumber:
			got = append(got, fmt.Sprintf("n%d", k.n))
		default:
			got = append(got, fmt.Sprintf("k%d", k.kind))
		}
	}
	want := []string{
		fmt.Sprintf("move%d", keyUp), fmt.Sprintf("move%d", keyDown), fmt.Sprintf("move%d", keyRight), fmt.Sprintf("move%d", keyLeft), fmt.Sprintf("move%d", keyUp),
		fmt.Sprintf("k%d", gridNext), fmt.Sprintf("k%d", gridPrev), fmt.Sprintf("k%d", gridOpen), "n3",
		fmt.Sprintf("k%d", gridLeave), fmt.Sprintf("k%d", gridLeave), fmt.Sprintf("k%d", gridLeave),
		"click4,15", // the press only: not the release, the right button or the wheel
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("keys\n got %v\nwant %v", got, want)
	}
}

func TestPickerTabAsksForTheGrid(t *testing.T) {
	entries := listedAttachEntries(testAttachEntries())
	var out bytes.Buffer
	chosen, grid, err := runPicker(strings.NewReader("\t"), &out, entries, func() (int, int) { return 100, 20 }, termStyle{}, func() time.Time { return attachNow })
	if err != nil || chosen != nil || !grid {
		t.Fatalf("Tab: %v %v %v", chosen, grid, err)
	}
	if !strings.Contains(out.String(), "Tab grid") {
		t.Fatalf("the picker does not say Tab opens the grid")
	}
}

// fakeGridHost is a session host with a few Sessions whose screens we set.
type fakeGridHost struct {
	mu       sync.Mutex
	infos    []sessionhost.Info
	text     map[string]string
	captures map[string]int
}

func (h *fakeGridHost) List(context.Context) ([]sessionhost.Info, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]sessionhost.Info(nil), h.infos...), nil
}

func (h *fakeGridHost) Capture(_ context.Context, p sessionhost.CaptureParams) (sessionhost.Capture, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captures[p.ID]++
	if !p.Styled || p.Scope != sessionhost.ScopeScreen {
		return sessionhost.Capture{}, fmt.Errorf("the grid must read the styled screen")
	}
	for _, info := range h.infos {
		if info.ID == p.ID {
			return sessionhost.Capture{Text: h.text[p.ID], Cols: 200, Rows: 50, OutputSeq: info.OutputSeq}, nil
		}
	}
	return sessionhost.Capture{}, fmt.Errorf("no session")
}

func (h *fakeGridHost) write(id, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text[id] = text
	for i := range h.infos {
		if h.infos[i].ID == id {
			h.infos[i].OutputSeq++
		}
	}
}

func newFakeGridHost(n int) *fakeGridHost {
	h := &fakeGridHost{infos: gridInfos(n), text: map[string]string{}, captures: map[string]int{}}
	for _, info := range h.infos {
		h.text[info.ID] = "screen of " + info.ID
	}
	return h
}

func runGridWith(t *testing.T, host *fakeGridHost, input []string, size func() (int, int)) (string, string) {
	t.Helper()
	typed := make(chan []byte)
	var out lockedBuffer
	done := make(chan string, 1)
	go func() {
		id, err := runGridView(context.Background(), host, newGridState(0), typed, &out, size, nil, termStyle{}, 5*time.Millisecond)
		if err != nil {
			id = "error: " + err.Error()
		}
		done <- id
	}()
	for _, chunk := range input {
		deadline := time.Now().Add(5 * time.Second)
		// Wait for the first frame before typing, so keys meet a drawn grid.
		for !strings.Contains(out.String(), "screen of") && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		select {
		case typed <- []byte(chunk):
		case <-time.After(5 * time.Second):
			t.Fatal("the grid does not read keys")
		}
	}
	select {
	case id := <-done:
		return id, out.String()
	case <-time.After(5 * time.Second):
		t.Fatal("the grid never ended")
	}
	return "", ""
}

func TestGridViewTilesEverySessionAndOpensTheOneChosen(t *testing.T) {
	size := func() (int, int) { return 120, 40 }
	host := newFakeGridHost(5) // newest first: mh-00 ... mh-04
	if id, screen := runGridWith(t, host, []string{"3"}, size); id != "mh-02" {
		t.Fatalf("number 3 opened %q", id)
	} else {
		for i := 0; i < 5; i++ {
			if !strings.Contains(screen, fmt.Sprintf("screen of mh-%02d", i)) || !strings.Contains(screen, fmt.Sprintf("Worker%02d", i)) {
				t.Errorf("Session %d is not on the grid", i)
			}
		}
		if !strings.HasPrefix(screen, enterAttach+gridMouseOn) {
			t.Errorf("the grid must take a screen and turn the mouse on")
		}
		if strings.Contains(screen, "\x1b[?1049l") {
			t.Errorf("the grid left the screen before it ended")
		}
	}
	// Arrows then Enter, in whatever shape five tiles take here.
	five := layoutGrid(5, 120, 40)
	if id, _ := runGridWith(t, newFakeGridHost(5), []string{"\x1b[C\x1b[B", "\r"}, size); id != fmt.Sprintf("mh-%02d", five.move(five.move(0, keyRight), keyDown)) {
		t.Fatalf("right, down, Enter opened %q in %+v", id, five)
	}
	if id, _ := runGridWith(t, newFakeGridHost(5), []string{"\t\t", "\r"}, size); id != "mh-02" {
		t.Fatalf("Tab Tab Enter opened %q", id)
	}
	if id, _ := runGridWith(t, newFakeGridHost(5), []string{"\x1b[Z", "\r"}, size); id != "mh-04" {
		t.Fatalf("Shift-Tab from the first opened %q", id)
	}
	if id, _ := runGridWith(t, newFakeGridHost(5), []string{"\r"}, size); id != "mh-00" {
		t.Fatalf("Enter opened %q, want the newest", id)
	}
	// A click in the second tile opens it.
	l := layoutGrid(5, 120, 40)
	row, col := l.origin(1)
	if id, _ := runGridWith(t, newFakeGridHost(5), []string{fmt.Sprintf("\x1b[<0;%d;%dM", col+3, row+2)}, size); id != "mh-01" {
		t.Fatalf("click opened %q", id)
	}
	// Leaving and keys for tiles that are not there open nothing.
	for _, key := range []string{"q", "\x1c", "\x03", "\x1b"} {
		if id, _ := runGridWith(t, newFakeGridHost(5), []string{key}, size); id != "" {
			t.Fatalf("%q opened %q", key, id)
		}
	}
	if id, _ := runGridWith(t, newFakeGridHost(2), []string{"9", "q"}, size); id != "" {
		t.Fatalf("9 with two Sessions opened %q", id)
	}
}

func TestGridViewReadsOnlyScreensThatMoved(t *testing.T) {
	host := newFakeGridHost(3)
	typed := make(chan []byte)
	var out lockedBuffer
	done := make(chan struct{})
	go func() {
		runGridView(context.Background(), host, newGridState(0), typed, &out, func() (int, int) { return 120, 40 }, nil, termStyle{}, 5*time.Millisecond)
		close(done)
	}()
	waitOutput(t, &out, "screen of mh-02")
	time.Sleep(60 * time.Millisecond) // many polls with nothing new
	host.mu.Lock()
	idle := host.captures["mh-00"]
	host.mu.Unlock()
	if idle != 1 {
		t.Fatalf("an unchanged screen was read %d times", idle)
	}
	host.write("mh-01", "fresh output")
	waitOutput(t, &out, "fresh output")
	host.mu.Lock()
	if host.captures["mh-01"] != 2 || host.captures["mh-00"] != 1 {
		t.Fatalf("captures %v", host.captures)
	}
	host.mu.Unlock()
	typed <- []byte("q")
	<-done
}

func TestGridViewRetilesOnResizeAndTracksSessions(t *testing.T) {
	host := newFakeGridHost(6)
	var mu sync.Mutex
	cols, rows := 200, 50
	size := func() (int, int) { mu.Lock(); defer mu.Unlock(); return cols, rows }
	typed := make(chan []byte)
	var out lockedBuffer
	done := make(chan struct{})
	go func() {
		runGridView(context.Background(), host, newGridState(0), typed, &out, size, nil, termStyle{}, 5*time.Millisecond)
		close(done)
	}()
	waitOutput(t, &out, "Worker05")
	before := len(out.String())
	mu.Lock()
	cols, rows = 80, 24 // six tiles no longer fit: three are left out
	mu.Unlock()
	waitOutput(t, &out, "\x1b[15;41H6 Worker05") // 80x24: two across, three down, the sixth in the last slot
	if len(out.String()) <= before {
		t.Fatalf("nothing was drawn after the resize")
	}
	mu.Lock()
	cols, rows = 60, 16 // one across, two down: the status line names the rest
	mu.Unlock()
	waitOutput(t, &out, "newest 2 of 6 shown, 4 more need a bigger terminal")
	typed <- []byte("q")
	<-done
}

func TestGridStateFollowsTheHost(t *testing.T) {
	g := newGridState(0)
	g.update(gridInfos(3))
	if len(g.entries) != 3 || g.sel != "mh-00" {
		t.Fatalf("start: %d entries, selected %q", len(g.entries), g.sel)
	}
	g.sel = "mh-01"
	g.screens["mh-01"] = gridScreen{have: true, seq: 11}
	g.screens["mh-02"] = gridScreen{have: true, seq: 99}
	layout := layoutGrid(3, 120, 40)
	if got := strings.Join(g.stale(layout), ","); got != "mh-00,mh-02" {
		t.Fatalf("stale = %s (mh-01 is current, mh-02's seq moved, mh-00 was never read)", got)
	}
	// A Session ends: its screen goes, the choice stays where it is.
	infos := gridInfos(3)
	infos[2].Alive = false
	g.update(infos)
	if len(g.entries) != 2 || g.sel != "mh-01" {
		t.Fatalf("after an end: %d entries, selected %q", len(g.entries), g.sel)
	}
	// The chosen Session ends: the newest takes the choice. A new one joins.
	infos = append(gridInfos(1), hostInfo("mh-new", "Fresh", "codex", 0, "working", nil))
	g.update(infos)
	if g.sel != "mh-new" || g.entries[0].ID != "mh-new" || len(g.entries) != 2 {
		t.Fatalf("after the chosen one ended: %+v selected %q", g.entries, g.sel)
	}
	g.update(nil)
	if g.sel != "" || len(g.entries) != 0 {
		t.Fatalf("empty host: %+v %q", g.entries, g.sel)
	}
}
