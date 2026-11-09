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

func TestReconstructRebuildsTheTerminal(t *testing.T) {
	term := newTerminal(t, 30, 6)
	term.Write([]byte("history 1\r\nhistory 2\r\n\x1b[1;32mgreen\x1b[0m\r\n\x1b[?2004h\x1b[3;5Hmoved"))
	rebuilt := capture(t, term, CaptureOptions{Scope: ScopeAll, Reconstruct: true})

	fresh := newTerminal(t, 30, 6)
	fresh.Write([]byte(rebuilt))
	for _, opts := range []CaptureOptions{{Scope: ScopeAll, Trim: true}, {Scope: ScopeScreen, Styled: true}} {
		if got, want := capture(t, fresh, opts), capture(t, term, opts); got != want {
			t.Fatalf("rebuilt %+v = %q, want %q", opts, got, want)
		}
	}
	got, want := fresh.State(), term.State()
	if got.CursorX != want.CursorX || got.CursorY != want.CursorY || got.BracketedPaste != want.BracketedPaste {
		t.Fatalf("rebuilt state = %+v, want %+v", got, want)
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
