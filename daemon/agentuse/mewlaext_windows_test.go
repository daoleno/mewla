//go:build windows

package agentuse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Chrome on Windows finds the host through HKEY_CURRENT_USER and starts the
// launcher through cmd.exe; the browser tools start the shim with no shell.
func TestWindowsNativeHostAndShim(t *testing.T) {
	parent := `Software\MewlaAgentUseTest`
	nativeHostsKey = parent + `\` + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, nativeHostsKey)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent)
	})
	state := filepath.Join(t.TempDir(), "100% state")
	s := &Service{dir: filepath.Join(state, "agent-use")}
	mewla := `C:\Program Files\Mewla\mewla.exe`
	if err := s.InstallNativeHost(filepath.Join(state, "User Data"), mewla, []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, nativeHostsKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath, _, err := k.GetStringValue("")
	k.Close()
	if err != nil || manifestPath != filepath.Join(s.dir, NativeHostName+".json") {
		t.Fatalf("registry points at %q (%v)", manifestPath, err)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest hostManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Path != filepath.Join(s.dir, "native-host.bat") {
		t.Fatalf("manifest %s (%v)", raw, err)
	}
	launcher, err := os.ReadFile(manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := "@echo off\r\n\"" + mewla + "\" agent-use native-host --state-dir \"" + strings.ReplaceAll(state, "%", "%%") + "\"\r\n"
	if string(launcher) != want {
		t.Fatalf("launcher %q, want %q", launcher, want)
	}
	if _, err := os.Stat(filepath.Join(state, "User Data", "NativeMessagingHosts")); !os.IsNotExist(err) {
		t.Fatalf("a profile folder Chrome on Windows never reads was written: %v", err)
	}

	exe, env, err := s.openShim(mewla)
	if err != nil || exe != mewla || len(env) != 1 || env[0] != OpenShimEnv+"="+state {
		t.Fatalf("shim %q %v %v", exe, env, err)
	}
}

func TestWindowsFindChrome(t *testing.T) {
	root := t.TempDir()
	programFiles, local := filepath.Join(root, "Program Files"), filepath.Join(root, "Local")
	t.Setenv("ProgramFiles", programFiles)
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("LOCALAPPDATA", local)
	if _, ok := findChrome(root); ok {
		t.Fatal("found a Chrome that isn't there")
	}
	chrome := filepath.Join(local, "Google", "Chrome", "Application", "chrome.exe")
	if err := os.MkdirAll(filepath.Dir(chrome), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := findChrome(root)
	if !ok || got.Executable != chrome || got.UserDataDir != filepath.Join(local, "Google", "Chrome", "User Data") {
		t.Fatalf("findChrome = %+v, %v", got, ok)
	}
}
