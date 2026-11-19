//go:build linux

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Chrome keeps the agents' session sign-ins across the browser's restarts: the
// profile continues where it left off, and its other preferences are kept as
// Chrome wrote them.
func TestKeepSessionCookies(t *testing.T) {
	profile := t.TempDir()
	if err := keepSessionCookies(profile); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profile, "Default", "Preferences")
	if got := restoreOnStartup(t, path); got != "1" {
		t.Fatalf("new profile restore_on_startup = %q", got)
	}

	existing := `{"session":{"restore_on_startup":5,"startup_urls":["https://a.example"]},"big":13385612345678901234,"profile":{"name":"Agents"}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keepSessionCookies(profile); err != nil {
		t.Fatal(err)
	}
	if got := restoreOnStartup(t, path); got != "1" {
		t.Fatalf("existing profile restore_on_startup = %q", got)
	}
	raw, _ := os.ReadFile(path)
	for _, kept := range []string{`"big":13385612345678901234`, `"startup_urls":["https://a.example"]`, `"name":"Agents"`} {
		if !strings.Contains(string(raw), kept) {
			t.Fatalf("lost %s: %s", kept, raw)
		}
	}
	sessions := filepath.Join(profile, "Default", "Sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "Tabs_1"), []byte("old tab"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	before := info.ModTime()
	if err := keepSessionCookies(profile); err != nil {
		t.Fatal(err)
	}
	if info, _ = os.Stat(path); !info.ModTime().Equal(before) {
		t.Fatal("an already kept profile was rewritten")
	}
	if _, err := os.Stat(sessions); !os.IsNotExist(err) {
		t.Fatalf("the tabs Chrome would restore are still there: %v", err)
	}

	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keepSessionCookies(profile); err == nil {
		t.Fatal("unreadable preferences were overwritten")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{broken" {
		t.Fatalf("unreadable preferences changed: %s", raw)
	}
}

func restoreOnStartup(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var prefs struct {
		Session struct {
			Restore json.Number `json:"restore_on_startup"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &prefs); err != nil {
		t.Fatal(err)
	}
	return prefs.Session.Restore.String()
}
