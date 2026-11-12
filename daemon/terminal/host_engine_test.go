//go:build ghosttyvt && cgo && (linux || darwin)

package terminal

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

// startTestHost runs a session host on a private socket.
func startTestHost(t *testing.T) (string, *sessionhost.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("", "th") // socket paths are short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "h.sock")
	server := sessionhost.NewServer(sessionhost.ServerOptions{SocketPath: socket, HostVersion: "test-host", Logf: t.Logf})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = server.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session host did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client, err := sessionhost.Dial(ctx, socket, "test-daemon")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return socket, client
}

func TestHostTerminalView(t *testing.T) {
	socket, client := startTestHost(t)
	dir := t.TempDir()
	const id = "mh-0123456789abcdef01234567"
	script := `i=0; while [ $i -lt 40 ]; do printf 'line %d\r\n' $i; i=$((i+1)); done; printf '\033[32mready\033[0m '; read x; printf 'got %s\r\n' "$x"; read y; exit 4`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready")

	sent := make(chan map[string]any, 1024)
	manager := NewManager(&HostBackend{Version: "test-daemon"})
	view, err := manager.Open("owner", HostBackendName, id, OpenOptions{Cols: 70, Rows: 20, Socket: socket}, func(v any) { sent <- v.(map[string]any) })
	if err != nil {
		t.Fatal(err)
	}
	opened := <-sent
	if opened["type"] != "terminal_opened" || opened["backend"] != HostBackendName || opened["cols"] != 70 || opened["rows"] != 20 {
		t.Fatalf("opened = %v", opened)
	}

	viewer, err := ghosttyvt.New(ghosttyvt.Options{Cols: 70, Rows: 20})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	var exit map[string]any
	settle := func() {
		for {
			select {
			case msg := <-sent:
				switch msg["type"] {
				case "terminal_output":
					viewer.Write([]byte(msg["data"].(string)))
				case "terminal_exit":
					exit = msg
					return
				default:
					t.Fatalf("unexpected %v", msg)
				}
			case <-time.After(300 * time.Millisecond):
				return
			}
		}
	}
	settle()
	matchHost(t, viewer, client, id)

	if err := manager.Input("owner", view.ID(), "hello\r"); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "got hello")
	settle()
	matchHost(t, viewer, client, id)

	// The App scrolls History; scrolling and pane focus are no-ops.
	history, err := manager.History("owner", view.ID())
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(history.ANSI, "\n"), "\n")
	if history.PaneID != id || history.Cols != 70 || history.Alternate || history.Total < 20 ||
		len(rows) != history.Total || !strings.Contains(rows[0], "line 0") ||
		len(history.Panes) != 1 || history.Panes[0] != (PaneScrollMode{ID: id, Cols: 70, Rows: 20}) {
		t.Fatalf("history = %+v (%d rows)", history, len(rows))
	}
	for _, call := range []func() error{
		func() error { return manager.Scroll("owner", view.ID(), -5) },
		func() error { return manager.ScrollCancel("owner", view.ID()) },
		func() error { return manager.FocusPane("owner", view.ID(), 3, 3) },
	} {
		if err := call(); err != nil {
			t.Fatalf("a no-op failed: %v", err)
		}
	}

	if err := manager.Input("owner", view.ID(), "\r"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for exit == nil && time.Now().Before(deadline) {
		settle()
	}
	if exit == nil || exit["exit_code"] != 4 {
		t.Fatalf("exit = %v, want terminal_exit with 4", exit)
	}
}

// The App opens a view at the grid it measured, and its grid can settle a
// row smaller before the replay lands: the replay scrolls the viewer by a row
// and the cursor lands a row below the text. The resize that follows draws
// the view again at the settled size, as tmux redraws a client.
func TestHostTerminalViewRedrawsOnResize(t *testing.T) {
	socket, client := startTestHost(t)
	dir := t.TempDir()
	const id = "mh-0123456789abcdef76543210"
	script := `i=0; while [ $i -lt 6 ]; do printf 'line %d\r\n' $i; i=$((i+1)); done; printf 'ready '; read x; exit 0`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready")

	sent := make(chan map[string]any, 1024)
	manager := NewManager(&HostBackend{Version: "test-daemon"})
	view, err := manager.Open("owner", HostBackendName, id, OpenOptions{Cols: 70, Rows: 20, Socket: socket}, func(v any) { sent <- v.(map[string]any) })
	if err != nil {
		t.Fatal(err)
	}
	if opened := <-sent; opened["type"] != "terminal_opened" {
		t.Fatalf("opened = %v", opened)
	}
	viewer, err := ghosttyvt.New(ghosttyvt.Options{Cols: 70, Rows: 19})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	settle := func() {
		for {
			select {
			case msg := <-sent:
				if msg["type"] != "terminal_output" {
					t.Fatalf("unexpected %v", msg)
				}
				viewer.Write([]byte(msg["data"].(string)))
			case <-time.After(300 * time.Millisecond):
				return
			}
		}
	}
	settle()
	if screen, _ := viewer.Capture(ghosttyvt.CaptureOptions{Scope: ghosttyvt.ScopeScreen}); strings.HasPrefix(screen, "line 0") {
		t.Fatalf("a 20-row replay did not scroll a 19-row viewer: %q", screen)
	}
	if err := manager.Resize("owner", view.ID(), 70, 19); err != nil {
		t.Fatal(err)
	}
	settle()
	matchHost(t, viewer, client, id)

	if _, err := viewer.Resize(50, 12); err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize("owner", view.ID(), 50, 12); err != nil {
		t.Fatal(err)
	}
	settle()
	matchHost(t, viewer, client, id)

	// The view still carries the program's output and input.
	if err := manager.Input("owner", view.ID(), "x\r"); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case msg := <-sent:
			if msg["type"] == "terminal_exit" {
				if msg["exit_code"] != 0 {
					t.Fatalf("exit = %v", msg)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("the program did not exit after its input")
		}
	}
}

// Another viewer (mewla attach on a desk) resizes the Session while the App's
// view is open: the program redraws for the desk's width, and the App's grid,
// another width, would draw rows over rows. The view starts over at once, and
// the person typing on the App takes the size back, after which the App's
// screen is the host's.
func TestHostTerminalViewRedrawsWhenAnotherViewerResizes(t *testing.T) {
	socket, client := startTestHost(t)
	dir := t.TempDir()
	const id = "mh-0123456789abcdef76543211"
	// Like a full-screen agent: on every resize it draws rows as wide as the terminal.
	script := `last=""; while :; do s=$(stty size); if [ "$s" != "$last" ]; then last=$s; c=${s#* }
printf '\033[H\033[2J'; for r in 1 2 3; do head -c "$c" /dev/zero | tr '\0' "$r"; printf '\r\n'; done; printf 'ready-%s ' "$c"; fi; sleep 0.05; done`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready-80")

	sent := make(chan map[string]any, 1024)
	manager := NewManager(&HostBackend{Version: "test-daemon"})
	view, err := manager.Open("owner", HostBackendName, id, OpenOptions{Cols: 70, Rows: 19, Socket: socket}, func(v any) { sent <- v.(map[string]any) })
	if err != nil {
		t.Fatal(err)
	}
	if opened := <-sent; opened["type"] != "terminal_opened" {
		t.Fatalf("opened = %v", opened)
	}
	viewer, err := ghosttyvt.New(ghosttyvt.Options{Cols: 70, Rows: 19})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	settle := func() (resets int) {
		for {
			select {
			case msg := <-sent:
				if msg["type"] != "terminal_output" {
					t.Fatalf("unexpected %v", msg)
				}
				data := msg["data"].(string)
				resets += strings.Count(data, fullReset)
				viewer.Write([]byte(data))
			case <-time.After(400 * time.Millisecond):
				return resets
			}
		}
	}
	waitHostScreen(t, client, id, "ready-70")
	settle()
	matchHost(t, viewer, client, id)

	desk, err := client.Attach(ctx, sessionhost.AttachParams{ID: id, Cols: 160, Rows: 45, Redraw: true})
	if err != nil {
		t.Fatal(err)
	}
	defer desk.Close()
	waitHostScreen(t, client, id, "ready-160")
	if resets := settle(); resets == 0 {
		t.Fatal("the App's view was not drawn again when another viewer resized the Session")
	}

	// The App's view losing the keyboard (a focus report) is no reason to take the size.
	if err := manager.Input("owner", view.ID(), "\x1b[O"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if info, err := client.Info(ctx, id); err != nil || info.Cols != 160 {
		t.Fatalf("info = %+v, %v; a focus-out took the size", info, err)
	}

	// The person types on the App: the Session is the App's size again, and its screen the host's.
	if err := manager.Input("owner", view.ID(), "x\r"); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready-70")
	settle()
	matchHost(t, viewer, client, id)
	for range desk.Output() {
	}
	if end, err := desk.End(); err != nil || end.Reason != sessionhost.AttachResized {
		t.Fatalf("the desk's view ended with %+v, %v; want %q", end, err, sessionhost.AttachResized)
	}

	// The App's grid sent again at the size it has (the terminal shown again) changes nothing.
	if err := manager.Resize("owner", view.ID(), 70, 19); err != nil {
		t.Fatal(err)
	}
	if resets := settle(); resets != 0 {
		t.Fatalf("showing the terminal again at its own size drew it again %d times", resets)
	}
}

// olderHost serves a socket in front of the host at socket that acts as a host
// from before AttachResized: an attach request loses its "redraw", so the host
// neither ends the view on another viewer's resize nor says it would.
func olderHost(t *testing.T, socket string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "o.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			down, err := listener.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("unix", socket)
			if err != nil {
				down.Close()
				continue
			}
			go func() { io.Copy(down, up); down.Close() }()
			go func() {
				defer up.Close()
				for {
					var header [4]byte
					if _, err := io.ReadFull(down, header[:]); err != nil {
						return
					}
					body := make([]byte, binary.BigEndian.Uint32(header[:]))
					if _, err := io.ReadFull(down, body); err != nil {
						return
					}
					body = bytes.Replace(body, []byte(`,"redraw":true`), nil, 1)
					binary.BigEndian.PutUint32(header[:], uint32(len(body)))
					if _, err := up.Write(append(header[:], body...)); err != nil {
						return
					}
				}
			}()
		}
	}()
	return path
}

// A daemon newer than its host is the usual state after an upgrade. A host
// from before AttachResized never tells the view of another viewer's resize,
// so the App's grid sent again (the terminal shown) still resizes the Session
// and draws the view again, as it did before.
func TestHostTerminalViewOnAnOlderHostTakesItsSizeBack(t *testing.T) {
	socket, client := startTestHost(t)
	dir := t.TempDir()
	const id = "mh-0123456789abcdef76543212"
	script := `last=""; while :; do s=$(stty size); if [ "$s" != "$last" ]; then last=$s; c=${s#* }
printf '\033[H\033[2J'; for r in 1 2 3; do head -c "$c" /dev/zero | tr '\0' "$r"; printf '\r\n'; done; printf 'ready-%s ' "$c"; fi; sleep 0.05; done`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready-80")

	sent := make(chan map[string]any, 1024)
	manager := NewManager(&HostBackend{Version: "test-daemon"})
	view, err := manager.Open("owner", HostBackendName, id, OpenOptions{Cols: 70, Rows: 19, Socket: olderHost(t, socket)}, func(v any) { sent <- v.(map[string]any) })
	if err != nil {
		t.Fatal(err)
	}
	if opened := <-sent; opened["type"] != "terminal_opened" {
		t.Fatalf("opened = %v", opened)
	}
	viewer, err := ghosttyvt.New(ghosttyvt.Options{Cols: 70, Rows: 19})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	settle := func() (resets int) {
		for {
			select {
			case msg := <-sent:
				if msg["type"] != "terminal_output" {
					t.Fatalf("unexpected %v", msg)
				}
				data := msg["data"].(string)
				resets += strings.Count(data, fullReset)
				viewer.Write([]byte(data))
			case <-time.After(400 * time.Millisecond):
				return resets
			}
		}
	}
	waitHostScreen(t, client, id, "ready-70")
	settle()

	desk, err := client.Attach(ctx, sessionhost.AttachParams{ID: id, Cols: 160, Rows: 45, Redraw: true})
	if err != nil {
		t.Fatal(err)
	}
	defer desk.Close()
	waitHostScreen(t, client, id, "ready-160")
	settle()

	if err := manager.Resize("owner", view.ID(), 70, 19); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready-70")
	if resets := settle(); resets == 0 {
		t.Fatal("the App's grid sent again on an older host did not draw the view again")
	}
	matchHost(t, viewer, client, id)
}

func TestHostTerminalRefusesWithoutAHost(t *testing.T) {
	manager := NewManager(&HostBackend{})
	if _, err := manager.Open("owner", HostBackendName, "mh-x", OpenOptions{}, func(any) {}); err == nil {
		t.Fatal("opened a host Session with no host socket")
	}
	dir, err := os.MkdirTemp("", "th")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "none.sock")
	if _, err := manager.Open("owner", HostBackendName, "mh-x", OpenOptions{Socket: socket}, func(any) {}); err == nil {
		t.Fatal("opened a host Session with no host running")
	}
	// Opening never starts a host.
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("a host socket appeared: %v", err)
	}
}

func waitHostScreen(t *testing.T, client *sessionhost.Client, id, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		capture, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeScreen})
		if err == nil && strings.Contains(capture.Text, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q on screen", want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func matchHost(t *testing.T, viewer *ghosttyvt.Terminal, client *sessionhost.Client, id string) {
	t.Helper()
	host, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeScreen, Styled: true})
	if err != nil {
		t.Fatal(err)
	}
	view, err := viewer.Capture(ghosttyvt.CaptureOptions{Scope: ghosttyvt.ScopeScreen, Styled: true})
	if err != nil {
		t.Fatal(err)
	}
	state := viewer.State()
	if view != host.Text || state.CursorX != host.CursorX || state.CursorY != host.CursorY {
		t.Fatalf("viewer %q at %d,%d; host %q at %d,%d", view, state.CursorX, state.CursorY, host.Text, host.CursorX, host.CursorY)
	}
}
