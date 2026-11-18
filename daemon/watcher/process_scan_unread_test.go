package watcher

import (
	"errors"
	"testing"
	"time"
)

// What each platform's process table gives for a command line it could not
// read stands for "unknown", never for a command.
func TestCommandLineUnread(t *testing.T) {
	denied := errors.New("STATUS_PARTIAL_COPY")
	for _, tc := range []struct {
		comm, args string
		err        error
		want       bool
	}{
		{"codex", "(codex)", nil, true},   // macOS ps
		{"claude", "[claude]", nil, true}, // Linux, an empty /proc cmdline
		{"claude", "", nil, true},         // nothing read
		{"claude", "  ", nil, true},       // nothing read
		{"claude", "", denied, true},      // Windows read failed
		{"claude", "claude.exe", denied, true},
		{"codex", "/opt/homebrew/bin/codex --remote ws://127.0.0.1:1", nil, false},
		{"claude", "claude --session-id 7d9b", nil, false},
		{"node", "(codex)", nil, false}, // another program's parenthesized name is an argument
	} {
		if got := commandLineUnread(tc.comm, tc.args, tc.err); got != tc.want {
			t.Errorf("commandLineUnread(%q, %q, %v) = %v, want %v", tc.comm, tc.args, tc.err, got, tc.want)
		}
	}
}

// A Mac snapshot taken while ps could not read a provider's arguments (it
// prints "(codex)") leaves the Session's provider undecided; the next
// snapshot, with the arguments, proves it. Recorded as a bare "codex", that
// read would have been taken for a different process ever after.
func TestDarwinSnapshotWithUnreadArgsIsUndecided(t *testing.T) {
	const host = "100 1 100 0 Thu Oct 15 14:07:42 2026 mewla /Users/u/.local/bin/mewla host serve\n"
	unread := parseProcessSnapshot([]byte(host+"200 100 200 200 Thu Oct 15 14:07:42 2026 codex (codex)\n"), time.UTC)
	if !unread[200].argsUnread {
		t.Fatalf("(codex) read as arguments: %+v", unread[200])
	}
	if authority, ok := resolveForegroundTargetProcess(200, unread); ok {
		t.Fatalf("an unread command line proved a provider: %+v", authority)
	}
	read := parseProcessSnapshot([]byte(host+"200 100 200 200 Thu Oct 15 14:07:42 2026 codex /Users/u/.codex/packages/standalone/bin/codex --remote ws://127.0.0.1:1\n"), time.UTC)
	if authority, ok := resolveForegroundTargetProcess(200, read); !ok || authority.command != "codex" {
		t.Fatalf("the full read did not prove codex: %+v ok=%v", authority, ok)
	}
}
