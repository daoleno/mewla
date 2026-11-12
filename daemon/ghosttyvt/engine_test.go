//go:build ghosttyvt && cgo

package ghosttyvt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func newTerminal(t *testing.T, cols, rows int, opts ...func(*Options)) *Terminal {
	t.Helper()
	options := Options{Cols: cols, Rows: rows}
	for _, opt := range opts {
		opt(&options)
	}
	term, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(term.Close)
	return term
}

func capture(t *testing.T, term *Terminal, opts CaptureOptions) string {
	t.Helper()
	text, err := term.Capture(opts)
	if err != nil {
		t.Fatalf("Capture(%+v): %v", opts, err)
	}
	return text
}

func TestAvailableAndVersion(t *testing.T) {
	if !Available() {
		t.Fatal("Available() = false in a ghosttyvt build")
	}
	if v := Version(); !strings.HasPrefix(v, "1.") {
		t.Fatalf("Version() = %q, want libghostty-vt 1.x", v)
	}
}

func TestNewRejectsBadSizes(t *testing.T) {
	for _, size := range [][2]int{{0, 10}, {10, 0}, {1001, 10}, {-1, -1}} {
		if _, err := New(Options{Cols: size[0], Rows: size[1]}); err == nil {
			t.Fatalf("New(%dx%d) succeeded", size[0], size[1])
		}
	}
}

func TestCaptureScopes(t *testing.T) {
	term := newTerminal(t, 10, 3)
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%02d", i+1)
	}
	term.Write([]byte(strings.Join(lines, "\r\n")))

	if got, want := capture(t, term, CaptureOptions{Scope: ScopeScreen, Trim: true}), "L18\nL19\nL20"; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := capture(t, term, CaptureOptions{Scope: ScopeRecent, History: 2, Trim: true}), "L16\nL17\nL18\nL19\nL20"; got != want {
		t.Fatalf("recent = %q, want %q", got, want)
	}
	if got, want := capture(t, term, CaptureOptions{Scope: ScopeRecent, History: 500, Trim: true}), strings.Join(lines, "\n"); got != want {
		t.Fatalf("recent beyond history = %q, want every line", got)
	}
	if got, want := capture(t, term, CaptureOptions{Scope: ScopeAll, Trim: true}), strings.Join(lines, "\n"); got != want {
		t.Fatalf("all = %q, want %q", got, want)
	}
}

func TestUnwrapJoinsWrappedRows(t *testing.T) {
	term := newTerminal(t, 10, 5)
	long := strings.Repeat("a", 25)
	term.Write([]byte(long))
	if got := capture(t, term, CaptureOptions{Scope: ScopeAll, Unwrap: true, Trim: true}); got != long {
		t.Fatalf("unwrapped = %q, want %q", got, long)
	}
	if got, want := capture(t, term, CaptureOptions{Scope: ScopeAll, Trim: true}), "aaaaaaaaaa\naaaaaaaaaa\naaaaa"; got != want {
		t.Fatalf("wrapped = %q, want %q", got, want)
	}
}

func TestQueriesAreAnswered(t *testing.T) {
	term := newTerminal(t, 80, 24)
	cases := []struct {
		name, query, want string
	}{
		{"primary device attributes", "\x1b[c", "\x1b[?62;22c"},
		{"cursor position", "ab\x1b[6n", "\x1b[1;3R"},
		{"xtversion", "\x1b[>q", "\x1bP>|" + DefaultXTVersion + "\x1b\\"},
	}
	for _, tc := range cases {
		out := term.Write([]byte(tc.query))
		if string(out.Reply) != tc.want {
			t.Errorf("%s: reply %q, want %q", tc.name, out.Reply, tc.want)
		}
	}
	out := term.Write([]byte("\x1b]7501;?\x07"))
	if !bytes.HasPrefix(out.Reply, []byte("\x1b]7501;?")) {
		t.Errorf("program status query: reply %q, want an OSC 7501 support answer", out.Reply)
	}
}

func TestXTVersionIsConfigurable(t *testing.T) {
	term := newTerminal(t, 80, 24, func(o *Options) { o.XTVersion = "other 2" })
	if got, want := string(term.Write([]byte("\x1b[>q")).Reply), "\x1bP>|other 2\x1b\\"; got != want {
		t.Fatalf("reply %q, want %q", got, want)
	}
}

func TestProgramStatusReports(t *testing.T) {
	term := newTerminal(t, 80, 24)
	message := base64.StdEncoding.EncodeToString([]byte("approve Write: hello.txt"))
	out := term.Write([]byte("\x1b]7501;state=working:app=claude-code\x07" +
		"\x1b]7501;state=blocked:kind=permission:app=claude-code:msg=" + message + "\x07"))
	if len(out.Status) != 2 {
		t.Fatalf("status reports = %+v, want 2", out.Status)
	}
	if got := out.Status[0]; got.State != StatusWorking || got.App != "claude-code" {
		t.Fatalf("first report = %+v", got)
	}
	if got := out.Status[1]; got.State != StatusBlocked || got.Kind != StatusKindPermission || got.Message != "approve Write: hello.txt" {
		t.Fatalf("second report = %+v", got)
	}
	if out.Status[1].State.String() != "blocked" || out.Status[1].Kind.String() != "permission" {
		t.Fatalf("names = %s/%s", out.Status[1].State, out.Status[1].Kind)
	}
	// Reports belong to the Write that carried them.
	if again := term.Write([]byte("x")); len(again.Status) != 0 {
		t.Fatalf("reports repeated: %+v", again.Status)
	}
}

func TestModesTitleAndBell(t *testing.T) {
	term := newTerminal(t, 80, 24)
	if state := term.State(); state.Alt || state.BracketedPaste || state.Cols != 80 || state.Rows != 24 {
		t.Fatalf("initial state = %+v", state)
	}
	out := term.Write([]byte("\x1b[?1049h\x1b[?2004h\x1b]0;my title\x07\x07"))
	if !out.TitleChanged || out.Bells != 1 {
		t.Fatalf("output = %+v, want a title change and one bell", out)
	}
	state := term.State()
	if !state.Alt || !state.BracketedPaste || state.Title != "my title" || !state.Ground {
		t.Fatalf("state = %+v", state)
	}
	term.Write([]byte("\x1b[?1049l\x1b[?2004l"))
	if state := term.State(); state.Alt || state.BracketedPaste {
		t.Fatalf("state after reset modes = %+v", state)
	}
	term.Write([]byte("\x1b["))
	if term.State().Ground {
		t.Fatal("Ground is true in the middle of a CSI sequence")
	}
}

func TestResize(t *testing.T) {
	term := newTerminal(t, 80, 24)
	if _, err := term.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if state := term.State(); state.Cols != 100 || state.Rows != 30 {
		t.Fatalf("state after resize = %+v", state)
	}
	if _, err := term.Resize(0, 30); err == nil {
		t.Fatal("Resize(0, 30) succeeded")
	}
}

func TestStyledCapture(t *testing.T) {
	term := newTerminal(t, 20, 3)
	term.Write([]byte("\x1b[31mred\x1b[0m plain"))
	plain := capture(t, term, CaptureOptions{Scope: ScopeScreen, Trim: true})
	if plain != "red plain" {
		t.Fatalf("plain = %q", plain)
	}
	styled := capture(t, term, CaptureOptions{Scope: ScopeScreen, Trim: true, Styled: true})
	if !strings.Contains(styled, "\x1b[") || !strings.Contains(styled, "red") || !strings.Contains(styled, "plain") {
		t.Fatalf("styled = %q, want SGR around the text", styled)
	}
}

// sameView fails unless a viewer shows what the terminal shows: the screen
// with its styles, the cursor and the modes a viewer acts on.
func sameView(t *testing.T, viewer, term *Terminal) {
	t.Helper()
	opts := CaptureOptions{Scope: ScopeScreen, Styled: true}
	if got, want := capture(t, viewer, opts), capture(t, term, opts); got != want {
		t.Fatalf("viewer screen = %q, want %q", got, want)
	}
	got, want := viewer.State(), term.State()
	if got.CursorX != want.CursorX || got.CursorY != want.CursorY || got.CursorVisible != want.CursorVisible ||
		got.Alt != want.Alt || got.BracketedPaste != want.BracketedPaste || got.MouseTracking != want.MouseTracking {
		t.Fatalf("viewer state = %+v, want %+v", got, want)
	}
}

func replay(t *testing.T, term *Terminal) []byte {
	t.Helper()
	out, err := term.Replay()
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return out
}

func TestReplayRebuildsTheScreen(t *testing.T) {
	term := newTerminal(t, 30, 6)
	term.Write([]byte("history 1\r\nhistory 2\r\n\x1b[1;32mgreen\x1b[0m\r\nmore\r\nrows\r\nto\r\nscroll\r\n\x1b[?2004h\x1b[?1000h\x1b[?1h\x1b[?25l\x1b[3;5Hmoved\x1b[4m"))
	out := replay(t, term)
	if bytes.Contains(out, []byte("\x1b]4;")) {
		t.Fatalf("replay sets the palette: %q", out)
	}
	if bytes.Contains(out, []byte("history 1")) {
		t.Fatalf("replay carries the history: %q", out)
	}

	viewer := newTerminal(t, 30, 6)
	viewer.Write(out)
	sameView(t, viewer, term)
	// Modes the viewer cannot report back are proven by what they do next:
	// cursor key mode and the pen style the program left.
	for _, key := range []string{"Up", "Left"} {
		got, err := viewer.EncodeKey(key)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := term.EncodeKey(key)
		if !bytes.Equal(got, want) {
			t.Fatalf("viewer encodes %s as %q, want %q", key, got, want)
		}
	}
	viewer.Write([]byte("next"))
	term.Write([]byte("next"))
	sameView(t, viewer, term)
}

func TestReplayKeepsThePrimaryScreenUnderTheAlternate(t *testing.T) {
	for _, mode := range []string{"1049", "1047", "47"} {
		t.Run(mode, func(t *testing.T) {
			term := newTerminal(t, 20, 4)
			term.Write([]byte("shell 1\r\nshell 2\r\n\x1b[31m$ \x1b[0mvim"))
			term.Write([]byte("\x1b[?" + mode + "h\x1b[H\x1b[2Jalt top\x1b[4;1Halt bottom\x1b[2;3H"))

			viewer := newTerminal(t, 20, 4)
			viewer.Write(replay(t, term))
			sameView(t, viewer, term)

			leave := []byte("\x1b[?" + mode + "l")
			viewer.Write(leave)
			term.Write(leave)
			sameView(t, viewer, term)
		})
	}
}

// Ground is false inside an escape sequence and inside a multi-byte
// character: a viewer may start reading output only between the two.
func TestGroundCoversSequencesAndCharacters(t *testing.T) {
	term := newTerminal(t, 20, 3)
	for _, step := range []struct {
		write  string
		ground bool
	}{{"a\xe2\x94", false}, {"\x80", true}, {"\x1b[3", false}, {"1m", true}, {"\x1b]0;ti", false}, {"tle\x07", true}} {
		term.Write([]byte(step.write))
		if got := term.State().Ground; got != step.ground {
			t.Fatalf("after %q: Ground = %v, want %v", step.write, got, step.ground)
		}
	}
}

func TestHistoryScope(t *testing.T) {
	term := newTerminal(t, 10, 3)
	if got := capture(t, term, CaptureOptions{Scope: ScopeHistory}); got != "" {
		t.Fatalf("history of a new terminal = %q, want none", got)
	}
	term.Write([]byte("\x1b[31mredredredredred\r\nstill red\x1b[0m plain\r\nrow3\r\nrow4\r\nrow5"))
	// Five rows went above a three-row screen: "redredredr", "edred",
	// "still red ", "plain" and "row3" were pushed up as the screen scrolled.
	all := capture(t, term, CaptureOptions{Scope: ScopeHistory, Styled: true})
	rows := strings.Split(all, "\r\n")
	if len(rows) != term.State().ScrollbackRows {
		t.Fatalf("history rows = %q, want %d of them", rows, term.State().ScrollbackRows)
	}
	for _, row := range rows[:3] {
		// Each row carries its own style, so rows stand alone.
		if !strings.HasPrefix(row, "\x1b[0m\x1b[38;5;1m") {
			t.Fatalf("history row %q does not start its own style", row)
		}
	}
	// Plain captures end rows with \n, styled ones with \r\n.
	if plain := capture(t, term, CaptureOptions{Scope: ScopeHistory, History: 2}); plain != "still red \nplain" {
		t.Fatalf("last 2 history rows = %q", plain)
	}
	if screen := capture(t, term, CaptureOptions{Scope: ScopeScreen}); strings.Contains(all, screen) {
		t.Fatalf("history %q contains the screen %q", all, screen)
	}
}

func TestSnapshotRestore(t *testing.T) {
	term := newTerminal(t, 40, 5)
	term.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n\x1b[?1049h\x1b[2;3Halt screen"))
	snapshot, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(snapshot, Options{XTVersion: "restored"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	for _, opts := range []CaptureOptions{{Scope: ScopeAll, Trim: true}, {Scope: ScopeScreen, Styled: true}} {
		if got, want := capture(t, restored, opts), capture(t, term, opts); got != want {
			t.Fatalf("restored %+v = %q, want %q", opts, got, want)
		}
	}
	if got, want := restored.State(), term.State(); got.Alt != want.Alt || got.CursorX != want.CursorX || got.CursorY != want.CursorY || got.Cols != want.Cols {
		t.Fatalf("restored state = %+v, want %+v", got, want)
	}
	// The restored terminal has its callbacks again.
	if got := string(restored.Write([]byte("\x1b[>q")).Reply); got != "\x1bP>|restored\x1b\\" {
		t.Fatalf("restored terminal answered %q", got)
	}
	if _, err := Restore(nil, Options{}); err == nil {
		t.Fatal("Restore(nil) succeeded")
	}
	if _, err := Restore([]byte("not a snapshot"), Options{}); err == nil {
		t.Fatal("Restore(garbage) succeeded")
	}
}

func TestKittyGraphicsOffByDefault(t *testing.T) {
	query := "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	off := newTerminal(t, 80, 24)
	if reply := off.Write([]byte(query)).Reply; bytes.Contains(reply, []byte("OK")) {
		t.Fatalf("Kitty graphics query answered %q with graphics off", reply)
	}
	on := newTerminal(t, 80, 24, func(o *Options) { o.KittyGraphics = true })
	if reply := on.Write([]byte(query)).Reply; !bytes.Contains(reply, []byte("OK")) {
		t.Fatalf("Kitty graphics query answered %q with graphics on", reply)
	}
}

func TestClosedTerminal(t *testing.T) {
	term, err := New(Options{Cols: 10, Rows: 2})
	if err != nil {
		t.Fatal(err)
	}
	term.Close()
	term.Close()
	if out := term.Write([]byte("x\x1b[c")); len(out.Reply) != 0 {
		t.Fatalf("closed terminal answered %q", out.Reply)
	}
	if _, err := term.Capture(CaptureOptions{}); err == nil {
		t.Fatal("Capture on a closed terminal succeeded")
	}
	if _, err := term.Snapshot(); err == nil {
		t.Fatal("Snapshot on a closed terminal succeeded")
	}
	if _, err := term.Resize(5, 5); err == nil {
		t.Fatal("Resize on a closed terminal succeeded")
	}
}

func TestEncodeKey(t *testing.T) {
	term := newTerminal(t, 80, 24)
	encode := func(spec string) string {
		t.Helper()
		out, err := term.EncodeKey(spec)
		if err != nil {
			t.Fatalf("EncodeKey(%q): %v", spec, err)
		}
		return string(out)
	}
	for spec, want := range map[string]string{
		"Enter": "\r", "Escape": "\x1b", "Tab": "\t", "BTab": "\x1b[Z", "Space": " ",
		"BSpace": "\x7f", "C-c": "\x03", "C-u": "\x15", "Up": "\x1b[A", "Down": "\x1b[B",
		"Left": "\x1b[D", "Right": "\x1b[C", "q": "q", "Q": "Q", "1": "1", "/": "/", "日": "日",
	} {
		if got := encode(spec); got != want {
			t.Errorf("legacy %s = %q, want %q", spec, got, want)
		}
	}
	// Cursor key application mode changes the arrows.
	term.Write([]byte("\x1b[?1h"))
	if got := encode("Up"); got != "\x1bOA" {
		t.Errorf("Up in cursor key application mode = %q", got)
	}
	term.Write([]byte("\x1b[?1l"))
	// The Kitty keyboard protocol's disambiguate flag changes Escape only.
	term.Write([]byte("\x1b[>1u"))
	if got := encode("Escape"); got != "\x1b[27u" {
		t.Errorf("Escape with Kitty disambiguate = %q", got)
	}
	if got := encode("Enter"); got != "\r" {
		t.Errorf("Enter with Kitty disambiguate = %q", got)
	}
	term.Write([]byte("\x1b[<u"))
	if got := encode("Escape"); got != "\x1b" {
		t.Errorf("Escape after popping Kitty flags = %q", got)
	}
	if _, err := term.EncodeKey("Nope"); err == nil {
		t.Error("an unknown key was encoded")
	}
	term.Close()
	if _, err := term.EncodeKey("Enter"); err == nil {
		t.Error("a closed terminal encoded a key")
	}
}

func TestRowFlagsMarkWrapAndPrompts(t *testing.T) {
	term := newTerminal(t, 10, 3)
	// A prompt (OSC 133 A), a command's output that wraps, another prompt.
	term.Write([]byte("\x1b]133;A\x07$ one\r\n0123456789ABCDEF\r\n\x1b]133;A\x07$ two\r\nx\r\ny\r\nz"))
	state := term.State()
	history := state.ScrollbackRows
	if history < 3 {
		t.Fatalf("history = %d rows, want at least 3", history)
	}
	text := capture(t, term, CaptureOptions{Scope: ScopeHistory, Styled: true})
	if got := strings.Count(text, "\n") + 1; got != history {
		t.Fatalf("history capture has %d lines, state says %d rows:\n%q", got, history, text)
	}
	flags := term.RowFlags(0, history+state.Rows)
	if len(flags) != history+state.Rows {
		t.Fatalf("RowFlags returned %d rows, want %d", len(flags), history+state.Rows)
	}
	if flags[0]&RowPrompt == 0 {
		t.Fatalf("row 0 flags = %b, want a prompt", flags[0])
	}
	if flags[1]&RowWrapped == 0 || flags[2]&RowContinuation == 0 {
		t.Fatalf("flags of the wrapped line = %b %b, want wrapped then continuation", flags[1], flags[2])
	}
	if flags[3]&RowPrompt == 0 {
		t.Fatalf("row 3 flags = %b, want the second prompt", flags[3])
	}
	if term.RowFlags(0, 0) != nil || term.RowFlags(-1, 2) != nil {
		t.Fatal("RowFlags answered an empty or negative range")
	}
}

func TestStateReportsKeyModes(t *testing.T) {
	term := newTerminal(t, 10, 3)
	before := term.State()
	t.Logf("defaults: app_cursor=%v alt_scroll=%v", before.AppCursor, before.AltScroll)
	if before.AppCursor {
		t.Fatal("application cursor keys are on in a fresh terminal")
	}
	term.Write([]byte("\x1b[?1h\x1b[?1007l"))
	after := term.State()
	if !after.AppCursor || after.AltScroll {
		t.Fatalf("after ?1h ?1007l: app_cursor=%v alt_scroll=%v", after.AppCursor, after.AltScroll)
	}
	term.Write([]byte("\x1b[?1l\x1b[?1007h"))
	again := term.State()
	if again.AppCursor || !again.AltScroll {
		t.Fatalf("after ?1l ?1007h: app_cursor=%v alt_scroll=%v", again.AppCursor, again.AltScroll)
	}
}

// libghostty-vt decodes UTF-8 in bulk with simdutf, whose AVX-512 kernels
// (built since 1.0.1, chosen at run time on CPUs that have them) leave the
// output undefined when the input has an error. An invalid sequence inside a
// long valid run must still come out as one U+FFFD with every valid character
// around it intact, whatever its offset against the 16/32/64-byte blocks.
func TestInvalidUTF8InsideLongRuns(t *testing.T) {
	const bad = "\xff"
	for _, unit := range []string{"a", "é", "日", "😀"} {
		for _, n := range []int{1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 100} {
			for pad := 0; pad < 4; pad++ {
				run := strings.Repeat("x", pad) + strings.Repeat(unit, n)
				for _, c := range []struct{ name, in, want string }{
					{"one byte", run + bad + run, run + "\uFFFD" + run},
					{"stray continuation", run + "\x80" + run, run + "\uFFFD" + run},
					{"cut sequence", run + "\xe2\x94" + run, run + "\uFFFD" + run},
					{"two in a row", run + bad + bad + run, run + "\uFFFD\uFFFD" + run},
					{"three apart", run + bad + run + "\x80" + run + "\xe2\x94" + run, run + "\uFFFD" + run + "\uFFFD" + run + "\uFFFD" + run},
					{"at the start", bad + run, "\uFFFD" + run},
					{"at the end", run + bad, run + "\uFFFD"},
				} {
					term := newTerminal(t, 1000, 2)
					term.Write([]byte(c.in))
					if got := capture(t, term, CaptureOptions{Scope: ScopeScreen, Trim: true}); got != c.want {
						t.Fatalf("%s, unit %q x%d, pad %d:\n got %q\nwant %q", c.name, unit, n, pad, got, c.want)
					}
				}
			}
		}
	}
}
