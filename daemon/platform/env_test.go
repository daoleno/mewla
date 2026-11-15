package platform

import (
	"os"
	"reflect"
	"testing"
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
