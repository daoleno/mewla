//go:build ghosttyvt && cgo && (linux || darwin)

package sessionhost

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// engineViewer feeds an attach stream into a terminal of its own, as the App
// and mewla attach do.
type engineViewer struct {
	t    *testing.T
	a    *Attachment
	term *ghosttyvt.Terminal
}

func attachEngineViewer(t *testing.T, c *Client, params AttachParams) *engineViewer {
	t.Helper()
	a, err := c.Attach(ctxT(t), params)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	term, err := ghosttyvt.New(ghosttyvt.Options{Cols: a.Cols, Rows: a.Rows})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(term.Close)
	term.Write(a.Replay)
	return &engineViewer{t: t, a: a, term: term}
}

// settle feeds output until none has come for a while.
func (v *engineViewer) settle() {
	v.t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case chunk, ok := <-v.a.Output():
			if !ok {
				return
			}
			v.term.Write(chunk)
		case <-time.After(300 * time.Millisecond):
			return
		case <-deadline:
			v.t.Fatal("output never went quiet")
		}
	}
}

// matches fails unless the viewer shows what the host's terminal shows.
func (v *engineViewer) matches(c *Client, id string) {
	v.t.Helper()
	host, err := c.Capture(ctxT(v.t), CaptureParams{ID: id, Scope: ScopeScreen, Styled: true})
	if err != nil {
		v.t.Fatal(err)
	}
	view, err := v.term.Capture(ghosttyvt.CaptureOptions{Scope: ghosttyvt.ScopeScreen, Styled: true})
	if err != nil {
		v.t.Fatal(err)
	}
	if view != host.Text {
		v.t.Fatalf("viewer shows\n%q\nhost shows\n%q", view, host.Text)
	}
	state := v.term.State()
	if state.CursorX != host.CursorX || state.CursorY != host.CursorY || state.Alt != host.Alt ||
		state.BracketedPaste != host.BracketedPaste || state.MouseTracking != host.MouseTracking ||
		state.Cols != host.Cols || state.Rows != host.Rows {
		v.t.Fatalf("viewer state %+v, host %+v", state, host)
	}
}

func TestEngineViewersMatchTheHostWhileOutputFlows(t *testing.T) {
	c := startEngineHost(t)
	dir := shortTempDir(t)
	script := `read go; i=0; while [ $i -lt 600 ]; do printf '\033[3%dmline %d \033[1mbold\033[0m tail\r\n' $((i % 8)) $i; i=$((i+1)); [ $((i % 40)) -eq 0 ] && sleep 0.02; done; printf '\033[?2004h\033[?1000h\033[5;7Hmid'; read x`
	if _, err := c.Create(ctxT(t), shellSession("flow", dir, script)); err != nil {
		t.Fatal(err)
	}
	// One viewer from the start, more while the output flows.
	viewers := []*engineViewer{attachEngineViewer(t, c, AttachParams{ID: "flow"})}
	if _, err := c.Input(ctxT(t), "flow", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		time.Sleep(40 * time.Millisecond)
		viewers = append(viewers, attachEngineViewer(t, c, AttachParams{ID: "flow"}))
	}
	waitScreen(t, c, "flow", "mid")
	for _, v := range viewers {
		v.settle()
		v.matches(c, "flow")
	}
	// A viewer of another size takes the Session to its size.
	resized := attachEngineViewer(t, c, AttachParams{ID: "flow", Cols: 60, Rows: 20})
	resized.settle()
	resized.matches(c, "flow")
}

func TestEngineViewerKeepsThePrimaryScreenUnderTheAlternate(t *testing.T) {
	c := startEngineHost(t)
	dir := shortTempDir(t)
	script := `printf 'shell one\r\nshell two\r\n$ '; printf '\033[?1049h\033[H\033[2J\033[7mfull screen\033[0m\033[10;3Happ'; read x; printf '\033[?1049lback'; read y`
	if _, err := c.Create(ctxT(t), shellSession("alt", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "alt", "app")
	v := attachEngineViewer(t, c, AttachParams{ID: "alt"})
	v.settle()
	v.matches(c, "alt")
	if _, err := c.Input(ctxT(t), "alt", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "alt", "back")
	v.settle()
	v.matches(c, "alt")
}

// A viewer that falls behind starts over on the terminal it already has: a
// full reset and a fresh replay must leave nothing of what it showed before.
func TestEngineResetAndReplayOverAStaleViewer(t *testing.T) {
	c := startEngineHost(t)
	dir := shortTempDir(t)
	if _, err := c.Create(ctxT(t), shellSession("stale", dir, `printf 'now \033[32mgreen\033[0m\r\nsecond'; read x`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "stale", "second")
	v := attachEngineViewer(t, c, AttachParams{ID: "stale"})
	// What a viewer may hold from before: other text, the alternate screen,
	// mouse tracking, bracketed paste, a scroll region, odd colors.
	stale, err := ghosttyvt.New(ghosttyvt.Options{Cols: v.a.Cols, Rows: v.a.Rows})
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	stale.Write([]byte(strings.Repeat("old old old\r\n", 30) + "\x1b[?1049h\x1b[?1000h\x1b[?2004h\x1b[3;9r\x1b[41mstale\x1b]4;1;rgb:00/ff/00\x07"))
	stale.Write(append([]byte("\x1bc"), v.a.Replay...))
	v.term = stale
	v.settle()
	v.matches(c, "stale")
}

// A read can end inside a multi-byte character. A viewer that attaches then
// must not start its stream with the character's last bytes: attach waits
// for the parser to be between characters (the second half comes within
// attachSettle here).
func TestEngineViewerAttachingInsideACharacter(t *testing.T) {
	c := startEngineHost(t)
	dir := shortTempDir(t)
	script := `printf '\342\224'; sleep 0.4; printf '\200\342\234\273 after'; read x`
	if _, err := c.Create(ctxT(t), shellSession("utf8", dir, script)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // inside the sleep, between the halves
	v := attachEngineViewer(t, c, AttachParams{ID: "utf8"})
	waitScreen(t, c, "utf8", "after")
	v.settle()
	v.matches(c, "utf8")
}
