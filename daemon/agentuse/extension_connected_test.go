package agentuse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The owner's Mac, 11-26: macOS keeps Mewla out of Chrome's folder, so the
// extension, installed and connected for weeks, read as missing and Add
// extension had nothing to do.
func TestAnExtensionThatConnectedCountsWhereChromesFolderCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any folder")
	}
	c := newFakeComputer(t)
	c.withNode()
	s := c.service(false)
	if err := os.Chmod(c.chrome.UserDataDir, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(c.chrome.UserDataDir, 0o700)

	if status := s.Refresh(); status.Browser.State != BrowserNoExtension {
		t.Fatalf("never connected: %+v", status.Browser)
	}
	if err := os.MkdirAll(filepath.Join(c.root, "state", "agent-use"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.root, "state", "agent-use", connectedMarker), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := s.Refresh()
	if status.Browser.State == BrowserNoExtension || status.Browser.Fix == FixInstallExtension {
		t.Fatalf("connected before, folder unreadable: %+v", status.Browser)
	}
	if status.Browser.Profile != "" {
		t.Fatalf("no profile can be named: %q", status.Browser.Profile)
	}

	// A folder that can be read says it is not there: the old connection
	// does not stand for it.
	if err := os.Chmod(c.chrome.UserDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if status := s.Refresh(); status.Browser.State != BrowserNoExtension {
		t.Fatalf("readable folder without it: %+v", status.Browser)
	}
}

func TestAddExtensionAddsItOnTheScreenWhereMewlaMay(t *testing.T) {
	c := newFakeComputer(t)
	c.withNode()
	s := c.service(false)
	started := make(chan string, 1)
	release := make(chan struct{})
	ready := false
	s.SetExtensionAdder(func(_ context.Context, dir string, connected func() bool) error {
		started <- dir
		<-release
		return nil
	}, func() bool { return ready })

	// Without Accessibility: Chrome's page and the words, as before.
	if _, err := s.Fix(FixInstallExtension); err == nil || !strings.Contains(err.Error(), "Load unpacked") {
		t.Fatalf("not ready: %v", err)
	}

	waitForFile(t, filepath.Join(c.root, "chrome-opened"), "chrome://extensions")
	ready = true
	_ = os.Remove(filepath.Join(c.root, "chrome-opened"))
	status, err := s.Fix(FixInstallExtension)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case dir := <-started:
		if dir != filepath.Join(c.root, "state", "extension", "mewla") {
			t.Fatalf("dir %s", dir)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the adder did not start")
	}
	if status.Browser.State != BrowserPreparing || !strings.Contains(status.Browser.Summary, "adding") {
		t.Fatalf("while adding: %+v", status.Browser)
	}
	// A second tap while it adds starts nothing more.
	if _, err := s.Fix(FixInstallExtension); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("a second adder started")
	case <-time.After(100 * time.Millisecond):
	}
	// Chrome is driven on the screen, not handed an address it ignores.
	if _, err := os.Stat(filepath.Join(c.root, "chrome-opened")); err == nil {
		t.Fatal("Chrome was handed chrome://extensions")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for s.Refresh().Browser.State == BrowserPreparing {
		if time.Now().After(deadline) {
			t.Fatal("still adding")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Removed from Chrome after it connected: the next agent's Chrome finds it
// missing, and the old connection stops standing for it.
func TestAnExtensionThatNoLongerConnectsIsNoLongerCounted(t *testing.T) {
	state, err := os.MkdirTemp("", "ext")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	b := NewBridge(filepath.Join(state, "agent-use"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveBridge(t, ctx, b)
	marker := filepath.Join(state, "agent-use", connectedMarker)
	if err := os.WriteFile(marker, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(wait time.Duration) { openForToolsWait = wait }(openForToolsWait)
	openForToolsWait = 300 * time.Millisecond
	started := false
	err = OpenForTools(state, []string{"chrome-extension://x/connect.html?mcpRelayUrl=ws%3A%2F%2F127.0.0.1%3A1%2Fextension%2Fx"}, func(string) error { started = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "did not connect") || !started {
		t.Fatalf("err %v started %v", err, started)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the marker stayed: %v", err)
	}
}
