package agentuse

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Chrome with the extension loaded unpacked, running an old version, is told by the daemon that
// carries a newer one to reload, and comes back as the new version without anyone touching it.
//
//	MEWLA_E2E_CHROME=<chrome for testing> MEWLA_E2E_BIN=<mewla binary> \
//	  go test ./agentuse -run ExtensionReloadsItself -v -count=1
//
// Chrome runs headless (no display needed).
func TestExtensionReloadsItselfWhenTheDaemonCarriesANewerOneE2E(t *testing.T) {
	chromeBin, mewla := os.Getenv("MEWLA_E2E_CHROME"), os.Getenv("MEWLA_E2E_BIN")
	if chromeBin == "" || mewla == "" {
		t.Skip("set MEWLA_E2E_CHROME and MEWLA_E2E_BIN")
	}
	root, err := os.MkdirTemp("", "reload-e2e")
	if err != nil {
		t.Fatal(err)
	}
	// Chrome's child processes may still be writing as it ends: removal is best effort.
	defer func() { time.Sleep(time.Second); _ = os.RemoveAll(root) }()
	state := filepath.Join(root, "state")
	profile := filepath.Join(root, "chrome")
	extDir := filepath.Join(root, "ext")

	// The folder Chrome loads, as the daemon writes it, but as the old version.
	writeExtension := func(version string) {
		err := fs.WalkDir(extensionFiles, "extension", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := extensionFiles.ReadFile(path)
			if err != nil {
				return err
			}
			rel := strings.TrimPrefix(path, "extension/")
			if rel == "manifest.json" {
				var manifest map[string]any
				if err := json.Unmarshal(raw, &manifest); err != nil {
					return err
				}
				manifest["version"] = version
				if raw, err = json.Marshal(manifest); err != nil {
					return err
				}
			}
			if err := os.MkdirAll(filepath.Join(extDir, filepath.Dir(rel)), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(extDir, rel), raw, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	const oldVersion, newVersion = "0.3.0", "0.3.1"
	writeExtension(oldVersion)

	svc, err := New(state, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.executable = func() (string, error) { return mewla, nil }
	if err := svc.InstallNativeHost(profile, mewla, []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}

	// An unpacked extension is something a person loaded with Developer mode on; without it Chrome
	// disables the extension when it reloads. A real profile with such an extension has it on.
	if err := os.MkdirAll(filepath.Join(profile, "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	prefs := `{"extensions":{"ui":{"developer_mode":true}}}`
	if err := os.WriteFile(filepath.Join(profile, "Default", "Preferences"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}

	chrome := exec.Command(chromeBin, "--user-data-dir="+profile, "--no-sandbox", "--no-first-run",
		"--no-default-browser-check", "--remote-debugging-port=9335", "--load-extension="+extDir, "about:blank")
	// The native host finds the daemon by the state directory in its environment: this test's, not the shell's.
	chrome.Env = append(os.Environ(), "HOME="+root, "MEWLA_STATE_DIR="+state, "DISPLAY="+os.Getenv("DISPLAY"))
	chrome.Stderr = os.Stderr
	if err := chrome.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chrome.Process.Kill(); _ = chrome.Wait() }()

	// Chrome has loaded the old extension; the daemon has not started yet, so it keeps retrying.
	// The daemon's start rewrites the folder to the version it carries, then it listens.
	time.Sleep(3 * time.Second)
	writeExtension(newVersion)
	bridge := svc.Bridge()
	bridge.bundled = newVersion
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = bridge.Serve(ctx) }()

	var seen []string
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		v := bridge.HostVersion()
		if v != "" && (len(seen) == 0 || seen[len(seen)-1] != v) {
			seen = append(seen, v)
			t.Logf("extension connected, running version %s", v)
		}
		if v == newVersion {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(seen) == 0 || seen[len(seen)-1] != newVersion {
		bridge.mu.Lock()
		t.Logf("reload asked for %v", bridge.reloadAsked)
		if req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9335/json/new?chrome-extension://"+MewlaExtensionID+"/status.html", nil); err == nil {
			if res, err := http.DefaultClient.Do(req); err == nil {
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
			time.Sleep(2 * time.Second)
		}
		if res, err := http.Get("http://127.0.0.1:9335/json/list"); err == nil {
			raw, _ := io.ReadAll(res.Body)
			res.Body.Close()
			t.Logf("targets: %s", raw)
		}
		bridge.mu.Unlock()
		if os.Getenv("MEWLA_E2E_HOLD") != "" {
			time.Sleep(90 * time.Second)
		}
		t.Fatalf("the extension did not come back as %s; it ran %v", newVersion, seen)
	}
	// The old one connected (the daemon asked exactly that version to reload) and the new one is what runs now.
	bridge.mu.Lock()
	askedOld := bridge.reloadAsked[oldVersion]
	bridge.mu.Unlock()
	if !askedOld {
		t.Fatalf("the daemon never asked %s to reload", oldVersion)
	}
	// It is not asked again: stay up a while and see nothing changes.
	time.Sleep(5 * time.Second)
	bridge.mu.Lock()
	asked := len(bridge.reloadAsked)
	bridge.mu.Unlock()
	if asked != 1 || bridge.HostVersion() != newVersion {
		t.Fatalf("reload asked for %d versions, running %q", asked, bridge.HostVersion())
	}
}
