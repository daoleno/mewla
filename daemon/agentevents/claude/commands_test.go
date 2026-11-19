package claude

import "testing"

// The argv joined as the process table shows it, captured from Claude Code
// 2.1.328 on Linux (zsh) for `sleep 61; echo "it's done"`.
const capturedBashShell = `/bin/zsh -c source /home/me/.claude/shell-snapshots/snapshot-zsh-1795061956762-8g8rl2.sh 2>/dev/null || true && \builtin setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_BRACE_CCL NO_MAGIC_EQUAL_SUBST NO_EQUALS 2>/dev/null || true && { \builtin unalias -- 'which'; \builtin unset -f -- 'which'; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval 'sleep 61; echo "it'"'"'s done"' < /dev/null && pwd -P >| /tmp/claude-96be-cwd`

func TestTurnCommandReadsClaudesBashShell(t *testing.T) {
	for _, tc := range []struct {
		args, want string
	}{
		{capturedBashShell, `sleep 61; echo "it's done"`},
		{`/bin/bash -c source /h/.claude/shell-snapshots/snapshot-bash-1-x.sh 2>/dev/null || true && shopt -u extglob 2>/dev/null || true && eval 'for i in $(seq 1 30); do echo $i; sleep 1; done' < /dev/null && pwd -P >| /tmp/c-cwd`, `for i in $(seq 1 30); do echo $i; sleep 1; done`},
		// A command that itself says "&& eval" stays whole.
		{`/bin/zsh -c source /h/.claude/shell-snapshots/snapshot-zsh-1-x.sh || true && eval 'a && eval b' < /dev/null && pwd -P >| /tmp/c-cwd`, `a && eval b`},
	} {
		got, ok := Adapter{}.TurnCommand(tc.args)
		if !ok || got != tc.want {
			t.Fatalf("TurnCommand(%q) = %q, %v; want %q", tc.args, got, ok, tc.want)
		}
	}
}

func TestTurnCommandRefusesClaudesOtherChildren(t *testing.T) {
	for _, args := range []string{
		"sleep 7.5", // a hook command
		"/usr/bin/mewla agent-event -state-dir /h/.mewla claude",
		"/usr/bin/mewla agent-use chrome-mcp --state-dir /h/.mewla",
		"/bin/sh -c eval 'x'",
		"/bin/zsh -c source /h/.claude/shell-snapshots/snapshot-zsh-1-x.sh || true && eval 'unterminated",
	} {
		if got, ok := (Adapter{}).TurnCommand(args); ok {
			t.Fatalf("TurnCommand(%q) = %q, want refused", args, got)
		}
	}
}
