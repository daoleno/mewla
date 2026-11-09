//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

// These tests run the watcher against a real session host (libghostty-vt
// linked) served in this process.

type inProcessHost struct {
	socket string
	stop   context.CancelFunc
	done   chan struct{}
}

func startInProcessHost(t *testing.T) *inProcessHost {
	t.Helper()
	dir, err := os.MkdirTemp("", "wh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h := &inProcessHost{socket: filepath.Join(dir, "h.sock"), done: make(chan struct{})}
	server := sessionhost.NewServer(sessionhost.ServerOptions{
		SocketPath:  h.socket,
		HostVersion: "watcher-test",
		Logf:        func(format string, args ...any) { t.Logf("host: "+format, args...) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	h.stop = cancel
	go func() {
		defer close(h.done)
		if err := server.Run(ctx); err != nil {
			t.Errorf("host: %v", err)
		}
	}()
	waitUntil(t, 5*time.Second, "host socket", func() bool {
		_, err := os.Stat(h.socket)
		return err == nil
	})
	t.Cleanup(h.shutdown)
	return h
}

func (h *inProcessHost) shutdown() {
	h.stop()
	select {
	case <-h.done:
	case <-time.After(15 * time.Second):
	}
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hostWatcher(t *testing.T, h *inProcessHost) *Watcher {
	t.Helper()
	w := New(50 * time.Millisecond)
	// tmux on a socket nothing listens on: only the host runs Sessions here.
	w.SetTmuxServer(filepath.Join(t.TempDir(), "no-tmux"), t.TempDir())
	w.SetSessionHost(SessionHostConfig{
		SocketPath: h.socket,
		Executable: "/nonexistent/mewla",
		StateDir:   t.TempDir(),
		Version:    "watcher-test",
	})
	return w
}

// createHostSession starts a Session the way CreateSession does, through the
// backend's place and create, but runs command under /bin/sh with a minimal
// environment: the developer's login shell and its rc files (which may start
// a multiplexer of their own) stay out of the test.
func createHostSession(t *testing.T, w *Watcher, spec backendCreateSpec) string {
	t.Helper()
	if spec.cwd == "" {
		spec.cwd = t.TempDir()
	}
	if spec.env == nil {
		spec.env = []string{"PATH=/usr/bin:/bin", "HOME=" + spec.cwd}
	}
	at, err := w.sessions().place("", false, spec.name)
	if err != nil {
		t.Fatal(err)
	}
	if at.backend != backendHost {
		t.Fatalf("placed on %q, want the host", at.backend)
	}
	id, err := w.sessions().create(at, spec)
	if err != nil {
		t.Fatal(err)
	}
	w.registerCreatedSession(id, spec.cwd, CreateSessionOptions{Name: spec.name}, time.Now())
	return id
}

func hostPaneOf(t *testing.T, w *Watcher, id string) (sessionPane, bool) {
	t.Helper()
	panes, err := w.sessions().listOwned()
	if err != nil {
		t.Fatalf("listOwned: %v", err)
	}
	for _, pane := range panes {
		if pane.target == id {
			return pane, true
		}
	}
	return sessionPane{}, false
}

func TestHostBackendRunsASession(t *testing.T) {
	h := startInProcessHost(t)
	w := hostWatcher(t, h)
	dir := t.TempDir()
	id := createHostSession(t, w, backendCreateSpec{
		name:    "probe",
		cwd:     dir,
		command: "printf 'HELLO\\n'; cat",
		labels:  sessionLabels{executorID: "claude-main", agentControl: "/tmp/agent.sock"},
	})
	if !hostSessionIDPattern.MatchString(id) || !w.sessions().validID(id) {
		t.Fatalf("host Session id %q", id)
	}
	if target, ok := w.TerminalTarget(id); !ok || target != (TerminalTarget{Backend: "host", Socket: h.socket}) {
		t.Fatalf("terminal of a host Session = %+v, %v", target, ok)
	}
	pane, listed := hostPaneOf(t, w, id)
	if !listed || pane.name != "probe" || pane.sessionName != hostSessionGroup || pane.executorID != "claude-main" ||
		pane.agentControl != "/tmp/agent.sock" || pane.panePID <= 0 || pane.dead {
		t.Fatalf("listed = %v %+v", listed, pane)
	}
	waitUntil(t, 20*time.Second, "HELLO", func() bool {
		screen, alive, _ := w.sessions().captureState(id)
		return alive && strings.Contains(screen, "HELLO")
	})
	if path, err := w.sessions().currentPath(id); err != nil || path != dir {
		t.Fatalf("currentPath = %q, %v", path, err)
	}

	// The origin marker is the Session's first line and proves the dialog
	// capture holds everything after it.
	text, info, err := w.sessions().captureDialog(id)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exact || !strings.HasPrefix(info.Origin, "mewla-origin ") || !strings.Contains(text, info.Origin) {
		t.Fatalf("dialog info = %+v\n%s", info, text)
	}
	if strings.Index(text, info.Origin) > strings.Index(text, "HELLO") {
		t.Fatalf("origin marker after the output:\n%s", text)
	}

	if err := w.sessions().sendLiteral(id, "typed"); err != nil {
		t.Fatal(err)
	}
	if err := w.sessions().sendKeys(id, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 10*time.Second, "echo of typed input", func() bool {
		screen, _, _ := w.sessions().captureState(id)
		return strings.Count(screen, "typed") >= 2 // the terminal's echo and cat's
	})

	present, owned, err := w.sessions().probeOwnership(id)
	if err != nil || !present || !owned {
		t.Fatalf("probeOwnership = %v %v %v", present, owned, err)
	}
	if err := w.sessions().kill(id); err != nil {
		t.Fatal(err)
	}
	if _, listed := hostPaneOf(t, w, id); listed {
		t.Fatal("killed Session still listed")
	}
	if gone, err := w.sessions().resolveAbsence(id, true); err != nil || !gone {
		t.Fatalf("resolveAbsence after kill = %v, %v", gone, err)
	}
	if err := w.sessions().kill(id); err != nil {
		t.Fatalf("killing a gone Session: %v", err)
	}
}

func TestHostSessionKnowsItsWorkerID(t *testing.T) {
	h := startInProcessHost(t)
	w := hostWatcher(t, h)
	// The prefix a progress-reporting Worker's command starts with.
	id := createHostSession(t, w, backendCreateSpec{
		name:    "worker",
		command: workerProgressEnvScript() + `; printf 'WORKER=%s TMUX=%s\n' "$MEWLA_WORKER_ID" "${TMUX:-none}"; cat`,
	})
	waitUntil(t, 20*time.Second, "worker id", func() bool {
		screen, _, _ := w.sessions().captureState(id)
		return strings.Contains(screen, "WORKER="+id+" TMUX=none")
	})
}

func TestHostInputSubmitsAndKeepsReceipts(t *testing.T) {
	h := startInProcessHost(t)
	w := hostWatcher(t, h)
	dir := t.TempDir()
	out := filepath.Join(dir, "in.bin")
	payload := "first line\nsecond line"
	want := "\x15" + payload + "\r"
	id := createHostSession(t, w, backendCreateSpec{
		name:    "raw",
		cwd:     dir,
		command: fmt.Sprintf(`stty raw -echo; printf 'RAW-READY\n'; dd bs=1 count=%d of=%s 2>/dev/null; printf 'RAW-DONE\n'; sleep 60`, len(want), out),
	})
	waitUntil(t, 20*time.Second, "raw reader", func() bool {
		screen, _, _ := w.sessions().captureState(id)
		return strings.Contains(screen, "RAW-READY")
	})
	io := w.sessions().inputIO()
	socket := io.socket(id)
	pane := io.pane(socket, id)
	if !pane.alive || pane.paneID != id || pane.generation != w.sessions().observedGeneration(id) || pane.generation == "" {
		t.Fatalf("input pane = %+v", pane)
	}
	checked := false
	started, err := io.submitPaste(socket, id, payload, sessionInputProvider{submitKey: "Enter"}, func() error {
		checked = true
		return nil
	})
	if err != nil || !started || !checked {
		t.Fatalf("submitPaste = %v, %v (checked %v)", started, err, checked)
	}
	waitUntil(t, 10*time.Second, "submission read", func() bool {
		screen, _, _ := w.sessions().captureState(id)
		return strings.Contains(screen, "RAW-DONE")
	})
	if got, _ := os.ReadFile(out); string(got) != want {
		t.Fatalf("program read %q, want %q", got, want)
	}

	// A refused guard sends nothing.
	if started, err := io.submitPaste(socket, id, "x", sessionInputProvider{submitKey: "Enter"}, func() error {
		return errors.New("identity moved")
	}); started || err == nil {
		t.Fatalf("submitPaste with a failing guard = %v, %v", started, err)
	}

	empty, err := io.receiptLedger(socket, id)
	if err != nil || len(empty.Entries) != 0 {
		t.Fatalf("fresh ledger = %+v, %v", empty, err)
	}
	digest := sha256.Sum256([]byte(payload))
	ledger := sessionInputReceiptLedger{SchemaVersion: sessionInputReceiptLedgerSchema, Entries: []sessionInputReceiptEntry{{
		Receipt: "turn:1", PayloadSHA256: hex.EncodeToString(digest[:]), Outcome: InputAccepted,
	}}}
	if err := io.writeReceiptLedger(socket, id, ledger); err != nil {
		t.Fatal(err)
	}
	read, err := io.receiptLedger(socket, id)
	if err != nil || len(read.Entries) != 1 || read.Entries[0].Receipt != "turn:1" {
		t.Fatalf("ledger read back = %+v, %v", read, err)
	}
}

func TestHostInventoryNeverReadsACrashAsTheEnd(t *testing.T) {
	h := startInProcessHost(t)
	w := hostWatcher(t, h)
	id := createHostSession(t, w, backendCreateSpec{name: "kept", command: "sleep 60"})
	if _, listed := hostPaneOf(t, w, id); !listed {
		t.Fatal("Session not listed")
	}
	h.shutdown()
	// The watcher holds a host Session: a vanished host is unknown, not an
	// empty inventory.
	if panes, err := w.sessions().listOwned(); err == nil {
		t.Fatalf("listOwned with the host gone = %+v, nil error", panes)
	}
	if _, err := w.sessions().resolveAbsence(id, true); !errors.Is(err, ErrOwnershipProbeUnavailable) {
		t.Fatalf("resolveAbsence after the host vanished = %v", err)
	}
	// A fresh process that never saw the host: the Session is gone with it.
	if gone, err := w.sessions().resolveAbsence(id, false); err != nil || !gone {
		t.Fatalf("resolveAbsence in a process that never saw a host = %v, %v", gone, err)
	}
}
