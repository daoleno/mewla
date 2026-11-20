package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

func TestEnvironmentUpdatesTakesTheFreshEnvironment(t *testing.T) {
	sep := string(os.PathListSeparator)
	current := []string{
		"Path=" + `/windows` + sep + `/home/me/.local/bin`,
		"ANTHROPIC_BASE_URL=https://old.example",
		"MEWLA_ONLY=kept",
		"SAME=1",
		"=C:=C:\\Windows",
	}
	fresh := []string{
		"Path=" + `/windows/` + sep + `/home/me/codex/bin`,
		"ANTHROPIC_BASE_URL=https://relay.example",
		"HTTPS_PROXY=http://127.0.0.1:7890",
		"same=1",
		"=D:=D:\\",
	}
	got := environmentUpdates(current, fresh)
	want := map[string]string{
		"Path":               `/windows/` + sep + `/home/me/codex/bin` + sep + `/home/me/.local/bin`,
		"ANTHROPIC_BASE_URL": "https://relay.example",
		"HTTPS_PROXY":        "http://127.0.0.1:7890",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %#v\nwant %#v", got, want)
	}
}

func TestEnvironmentUpdatesNothingWhenCurrent(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/me"}
	if got := environmentUpdates(env, env); len(got) != 0 {
		t.Fatalf("updates = %#v, want none", got)
	}
}

func TestRefreshEnvironmentAddsAnInstallerFolderCreatedLater(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the person's real environment would replace the test's home folder")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(hostdeps.SetSystemPathDirs())
	bin := filepath.Join(home, ".local", "bin")
	refresh := func() {
		lastRefresh = time.Time{}
		RefreshEnvironment()
	}
	refresh()
	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), bin) {
		t.Fatal("added a folder that does not exist")
	}
	// Claude Code's installer creates ~/.local/bin after the daemon started
	// and leaves PATH alone; a Session started next must still find claude.
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	refresh()
	if !slices.Contains(filepath.SplitList(os.Getenv("PATH")), bin) {
		t.Fatalf("PATH = %q, want %s", os.Getenv("PATH"), bin)
	}
}

func TestPinnedDirectoryStaysFirstThroughARefresh(t *testing.T) {
	t.Cleanup(func() { pinned = nil })
	list := func(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }
	pinned = []string{"/Applications/Mewla.app/Contents/Helpers"}
	updates := environmentUpdates(
		[]string{"PATH=" + list("/Applications/Mewla.app/Contents/Helpers", "/usr/bin")},
		[]string{"PATH=" + list("/Users/me/.local/bin", "/usr/bin")},
	)
	if got, want := updates["PATH"], list("/Applications/Mewla.app/Contents/Helpers", "/Users/me/.local/bin", "/usr/bin"); got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
}
