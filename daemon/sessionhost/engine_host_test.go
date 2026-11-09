//go:build ghosttyvt && cgo && (linux || darwin)

package sessionhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// These tests run the host with the real libghostty-vt terminal.

func startEngineHost(t *testing.T) *Client {
	t.Helper()
	c := dialHost(t, startHost(t, ServerOptions{newScreen: newEngineScreen}))
	if w := c.Welcome(); !w.Engine || w.EngineVersion != ghosttyvt.Version() || w.EngineVersion == "" {
		t.Fatalf("welcome = %+v", w)
	}
	return c
}

func TestEngineAnswersTheProgramsQueries(t *testing.T) {
	c := startEngineHost(t)
	for _, query := range []struct{ name, ask, want string }{
		{"device attributes", `\033[c`, "\x1b[?62;22c"},
		{"version", `\033[>q`, "\x1bP>|" + ghosttyvt.DefaultXTVersion + "\x1b\\"},
		{"cursor position", `\033[3;7H\033[6n`, "\x1b[3;7R"},
	} {
		dir := shortTempDir(t)
		out := filepath.Join(dir, "reply.bin")
		id := "q-" + strings.ReplaceAll(query.name, " ", "-")
		script := fmt.Sprintf(`stty raw -echo; printf '%s'; dd bs=1 count=%d of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, query.ask, len(query.want), out)
		if _, err := c.Create(ctxT(t), shellSession(id, dir, script)); err != nil {
			t.Fatal(err)
		}
		if got := string(readWhenDone(t, c, id, out)); got != query.want {
			t.Errorf("%s: program read %q, want %q", query.name, got, query.want)
		}
	}
}

func TestEngineCaptureResizeAndModes(t *testing.T) {
	c := startEngineHost(t)
	ctx := ctxT(t)
	dir := shortTempDir(t)
	script := `printf '\033]2;build log\007\033[?2004h'; i=1; while [ $i -le 40 ]; do echo "row $i"; i=$((i+1)); done; echo READY; read x; stty size; read y`
	if _, err := c.Create(ctx, shellSession("cap", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "cap", "READY")
	screen, err := c.Capture(ctx, CaptureParams{ID: "cap", Scope: ScopeScreen, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen.Text, "row 1\n") || !strings.Contains(screen.Text, "row 40") {
		t.Fatalf("screen capture holds the wrong rows:\n%s", screen.Text)
	}
	if !screen.BracketedPaste || screen.Cols != 80 || screen.Rows != 24 || screen.ScrollbackRows == 0 {
		t.Fatalf("capture state = %+v", screen)
	}
	all, err := c.Capture(ctx, CaptureParams{ID: "cap", Scope: ScopeAll, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all.Text, "row 1\n") {
		t.Fatalf("full capture lacks the first row:\n%s", all.Text)
	}
	recent, err := c.Capture(ctx, CaptureParams{ID: "cap", Scope: ScopeRecent, History: 5, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(recent.Text, "\n"); strings.Contains(recent.Text, "row 1\n") || lines < strings.Count(screen.Text, "\n")+4 {
		t.Fatalf("recent capture with 5 rows of history:\n%s", recent.Text)
	}
	if info, _ := c.Info(ctx, "cap"); info.Title != "build log" {
		t.Fatalf("title = %q", info.Title)
	}

	if err := c.Resize(ctx, "cap", 50, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Input(ctx, "cap", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "cap", "12 50")
	if info, _ := c.Info(ctx, "cap"); info.Cols != 50 || info.Rows != 12 {
		t.Fatalf("size after resize = %dx%d", info.Cols, info.Rows)
	}
}

func TestEngineProgramStatusRecords(t *testing.T) {
	c := startEngineHost(t)
	ctx := ctxT(t)
	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// "approve Write" in base64.
	script := `printf '\033]7501;state=working:app=claude-code\007'; read a; ` +
		`printf '\033]7501;id=tool:state=blocked:kind=permission:msg=YXBwcm92ZSBXcml0ZQ==\007'; read b; ` +
		`printf '\033]7501;id=tool:state=clear\007'; read c`
	if _, err := c.Create(ctx, shellSession("agent", shortTempDir(t), script)); err != nil {
		t.Fatal(err)
	}
	waitStatus := func(what string, ok func([]ProgramStatus) bool) {
		t.Helper()
		waitFor(t, 10*time.Second, what, func() bool {
			info, err := c.Info(ctx, "agent")
			return err == nil && ok(info.Status)
		})
	}
	waitStatus("working", func(list []ProgramStatus) bool {
		return len(list) == 1 && list[0].State == "working" && list[0].App == "claude-code"
	})
	if _, err := c.Input(ctx, "agent", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitStatus("blocked under the working record", func(list []ProgramStatus) bool {
		return len(list) == 2 && list[1].ID == "tool" && list[1].State == "blocked" &&
			list[1].Kind == "permission" && list[1].Message == "approve Write" && list[1].App == "claude-code"
	})
	if _, err := c.Input(ctx, "agent", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	waitStatus("tool cleared", func(list []ProgramStatus) bool {
		return len(list) == 1 && list[0].ID == ""
	})

	statusEvents := 0
	for drained := false; !drained; {
		select {
		case event := <-events:
			if event.Type == EventStatus && event.Session == "agent" {
				statusEvents++
			}
		default:
			drained = true
		}
	}
	if statusEvents != 3 {
		t.Fatalf("%d status events, want 3", statusEvents)
	}
}

func TestEngineBracketsPastesForTheProgram(t *testing.T) {
	c := startEngineHost(t)
	text := "first\nsecond"
	want := "\x1b[200~" + text + "\x1b[201~"
	dir := shortTempDir(t)
	out := filepath.Join(dir, "in.bin")
	script := fmt.Sprintf(`stty raw -echo; printf '\033[?2004hRAW-READY\n'; dd bs=1 count=%d of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, len(want), out)
	if _, err := c.Create(ctxT(t), shellSession("paste", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "paste", "RAW-READY")
	if _, err := c.Input(ctxT(t), "paste", []InputStep{{Paste: text}}); err != nil {
		t.Fatal(err)
	}
	if got := string(readWhenDone(t, c, "paste", out)); got != want {
		t.Fatalf("program read %q, want %q", got, want)
	}
}

func TestEngineEncodesKeysForTheProgramsModes(t *testing.T) {
	c := startEngineHost(t)
	// Cursor key application mode first, then the Kitty keyboard protocol,
	// whose CSI form replaces it.
	dir := shortTempDir(t)
	first, second := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	script := fmt.Sprintf(`stty raw -echo; printf '\033[?1hA-READY\n'; dd bs=1 count=3 of=%s 2>/dev/null; `+
		`printf '\033[>1uB-READY\n'; dd bs=1 count=8 of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 30`, first, second)
	if _, err := c.Create(ctxT(t), shellSession("modes", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "modes", "A-READY")
	if _, err := c.Input(ctxT(t), "modes", []InputStep{{Key: "Up"}}); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "modes", "B-READY")
	if _, err := c.Input(ctxT(t), "modes", []InputStep{{Key: "Up"}, {Key: "Escape"}}); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "modes", "RAW-DONE")
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) != "\x1bOA" || string(b) != "\x1b[A\x1b[27u" {
		t.Fatalf("program read %q then %q", a, b)
	}
}
