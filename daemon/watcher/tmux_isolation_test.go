package watcher

import (
	"strings"
	"testing"
	"time"
)

func TestWatcherUsesOneSelectedHostServerForKnownTargets(t *testing.T) {
	w := New(time.Second)
	socket := "/run/user/1000/custom tmux.sock "
	w.SetTmuxServer(socket, "/home/user/.mewla/run/tmux-scratch/daemon-1")
	w.registerCreatedSession("mewla-worker-worker:@3", "/repo", CreateSessionOptions{
		Name: "worker", Delegated: true, ProgressEnv: true,
	}, time.Now().UTC())

	if got := w.socketPathFor("mewla-worker-worker:@3"); got != socket {
		t.Fatalf("internal selected socket = %q, want %q", got, socket)
	}
	if got, ok := w.TerminalTarget("mewla-worker-worker:@3"); !ok || got != (TerminalTarget{Backend: "tmux", Socket: socket}) {
		t.Fatalf("known target terminal = %+v, %v; want tmux on %q", got, ok, socket)
	}
	if got, ok := w.TerminalTarget("ambient:@9"); ok || got.Socket != "" {
		t.Fatalf("unknown target exposed %+v", got)
	}
}

func TestWorkerProgressEnvScriptDropsHostTmuxCapabilityAfterIdentity(t *testing.T) {
	script := workerProgressEnvScript()
	unset := strings.Index(script, "unset TMUX")
	derive := strings.Index(script, "MEWLA_WORKER_ID=")
	if unset < 0 {
		t.Fatalf("script does not unset TMUX: %s", script)
	}
	if derive < 0 || derive > unset {
		t.Fatalf("script must derive MEWLA_WORKER_ID before unsetting TMUX: %s", script)
	}
}

func TestProviderWindowEnvironmentGetsPrivateTmuxScratch(t *testing.T) {
	w := New(time.Second)
	scratch := "/home/user/.mewla/run/tmux-scratch/daemon-1"
	w.SetTmuxServer("/run/user/1000/custom-tmux.sock", scratch)

	host := CreateSessionOptions{Name: "Brain", Hidden: true, ProgressEnv: true, Env: map[string]string{}}
	applyProviderTmuxIsolation(&host, w)
	if host.Env["TMUX_TMPDIR"] != scratch {
		t.Fatalf("hidden host TMUX_TMPDIR = %q, want %q", host.Env["TMUX_TMPDIR"], scratch)
	}

	delegated := CreateSessionOptions{Name: "worker", Delegated: true, ProgressEnv: true, Env: map[string]string{}}
	delegated.resource = &delegatedResourceSpec{TempDir: "/home/user/.mewla/t/abc123"}
	applyProviderTmuxIsolation(&delegated, w)
	// Never the Worker's own scratch: it goes when the Worker ends, and tmux
	// whose TMUX_TMPDIR is gone falls back to the user's default server.
	if delegated.Env["TMUX_TMPDIR"] != scratch {
		t.Fatalf("delegated TMUX_TMPDIR = %q, want the daemon's %q", delegated.Env["TMUX_TMPDIR"], scratch)
	}
	if _, present := host.Env["TMUX"]; present {
		t.Fatal("host environment injected TMUX")
	}
}

// Brain, a Worker and an agent a person launched keep their plain tmux in
// the daemon's private TMUX_TMPDIR; a person's own shell keeps their own.
func TestOnlyAgentSessionsGetThePrivateTmux(t *testing.T) {
	for _, opts := range []CreateSessionOptions{
		{ProgressEnv: true, Command: "bash"},
		{Hidden: true},
		{Delegated: true, Command: "bash --norc -i"},
		{ExecutorID: "my-agent", Command: "/opt/agent/run"},
		{Command: "claude --permission-mode bypassPermissions"},
		{Command: "codex resume 019a1b2c-3d4e-7f00-8a9b-0c1d2e3f4a5b"},
		{Command: "grok"},
	} {
		if !sessionRunsAnAgent(opts) {
			t.Errorf("%+v is not an agent", opts)
		}
	}
	for _, opts := range []CreateSessionOptions{
		{},
		{Command: "/bin/zsh"},
		{Command: "bash -l"},
		{Command: "htop"},
	} {
		if sessionRunsAnAgent(opts) {
			t.Errorf("%+v, a person's own shell, is an agent", opts)
		}
	}
}
