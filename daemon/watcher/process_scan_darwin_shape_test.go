package watcher

import (
	"strings"
	"testing"
	"time"
)

// macOS ps prints argv[0] cut to 16 columns for comm= when it is not the last
// column; ucomm= is the executable name. Lines are shaped like a real Mac
// snapshot (paths sanitized).
func TestDarwinProcessSnapshotProvesProviderFromUcomm(t *testing.T) {
	const tmuxServer = "100 1 100 0 Thu Oct 15 14:07:42 2026 tmux tmux new-session -d -s mewla-worker-brain\n"
	for _, tc := range []struct {
		name, lines, want string
	}{
		{
			name: "native codex",
			lines: tmuxServer +
				"200 100 200 200 Thu Oct 15 14:07:42 2026 codex /Users/u/.codex/packages/standalone/bin/codex --remote ws://127.0.0.1:1\n",
			want: "codex",
		},
		{
			name: "claude native installer symlink",
			lines: tmuxServer +
				"200 100 200 200 Thu Oct 15 14:07:42 2026 2.1.296 /Users/u/.local/bin/claude --session-id 7d9bb1ec-38cf-4d56-8862-b8fafcaceffc\n" +
				"300 200 200 200 Thu Oct 15 14:10:39 2026 npm npm exec chrome-devtools-mcp@latest\n",
			want: "claude --session-id 7d9bb1ec-38cf-4d56-8862-b8fafcaceffc",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processes := parseProcessSnapshot([]byte(tc.lines), time.UTC)
			authority, ok := resolveForegroundTargetProcess(200, processes)
			if !ok || authority.command != tc.want || authority.provider.pid != 200 {
				t.Fatalf("authority=%#v ok=%v, want %q from pid 200", authority, ok, tc.want)
			}
		})
	}

	// The old comm= column made codex indeterminate on every Mac.
	truncated := strings.Replace(tmuxServer+
		"200 100 200 200 Thu Oct 15 14:07:42 2026 codex /Users/u/.codex/packages/standalone/bin/codex --remote ws://127.0.0.1:1\n",
		" codex /Users", " /Users/u/.codex/ /Users", 1)
	if _, ok := resolveForegroundTargetProcess(200, parseProcessSnapshot([]byte(truncated), time.UTC)); ok {
		t.Fatal("truncated comm unexpectedly proved codex; the regression guard no longer holds")
	}
}

func TestOwnershipProbeDiagnosticsOmitProcessArguments(t *testing.T) {
	processes := parseProcessSnapshot([]byte(
		"200 100 200 200 Thu Oct 15 14:07:42 2026 node node /x/codex --api-key=sk-secret\n"+
			"201 200 200 200 Thu Oct 15 14:07:42 2026 claude claude --token=other-secret\n",
	), time.UTC)
	got := describeForegroundGroup(200, processes)
	if strings.Contains(got, "secret") || !strings.Contains(got, "200(") || !strings.Contains(got, "provider=claude") {
		t.Fatalf("diagnostic = %q", got)
	}
}
