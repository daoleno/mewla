package calendar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduledActionRouteReadsTheRowTheWayTheJudgeRowsAreRead(t *testing.T) {
	for name, test := range map[string]struct {
		text string
		want ActionRoute
		err  string
	}{
		"plain":           {"| Scheduled action | claude | haiku | low |", ActionRoute{"claude", "haiku", "low"}, ""},
		"case and prefix": {"| scheduled ACTION (recurring) | `codex` | `gpt-x` | `high` |", ActionRoute{"codex", "gpt-x", "high"}, ""},
		"defaults":        {"| Scheduled action | claude | client default | - |", ActionRoute{Executor: "claude"}, ""},
		"first row wins":  {"| Scheduled action | claude | a | low |\n| Scheduled action | codex | b | high |", ActionRoute{"claude", "a", "low"}, ""},
		"no row":          {"| Work progress | claude | sonnet | low |\nScheduled action: claude", ActionRoute{}, ""},
		"three cells":     {"| Scheduled action | claude | haiku |", ActionRoute{}, "needs four cells"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseScheduledActionRoute(test.text)
			if (err == nil) != (test.err == "") || err != nil && !strings.Contains(err.Error(), test.err) {
				t.Fatalf("error = %v, want %q", err, test.err)
			}
			if got != test.want {
				t.Fatalf("route = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReadScheduledActionRouteTreatsAMissingFileAsNoRow(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"", filepath.Join(dir, "routing.md")} {
		if got, err := ReadScheduledActionRoute(path); err != nil || got != (ActionRoute{}) {
			t.Fatalf("%q: %+v %v", path, got, err)
		}
	}
	if _, err := ReadScheduledActionRoute(dir); err == nil {
		t.Fatal("a directory read as routing.md should be an error")
	}
	path := filepath.Join(dir, "routing.md")
	if err := os.WriteFile(path, []byte("| Scheduled action | claude | m | low |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadScheduledActionRoute(path); err != nil || got.Model != "m" {
		t.Fatalf("%+v %v", got, err)
	}
}
