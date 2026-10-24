package tmuxguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv builds an Env over an in-memory set of existing directories, so the
// decision table needs no tmux and no filesystem.
func fakeEnv(dirs ...string) Env {
	exists := map[string]bool{}
	for _, d := range dirs {
		exists[d] = true
	}
	return Env{
		TmuxTmpdir: "/scratch/w1",
		Scratch:    "/scratch/w1",
		Cwd:        "/scratch/w1/work",
		UID:        1000,
		IsDir:      func(p string) bool { return exists[filepath.Clean(p)] },
		Resolve:    filepath.Clean,
	}
}

func TestDecide(t *testing.T) {
	base := fakeEnv("/scratch/w1", "/scratch/w1/work", "/tmp")
	with := func(mod func(*Env)) Env {
		e := base
		mod(&e)
		return e
	}
	cases := []struct {
		name    string
		args    []string
		env     Env
		allow   bool
		reasonw string
	}{
		{"version never touches a server", []string{"-V"}, with(func(e *Env) { e.TmuxTmpdir = "" }), true, ""},
		{"missing TMUX_TMPDIR falls back to live: ls", []string{"ls"}, with(func(e *Env) { e.TmuxTmpdir = "/scratch/w1/gone/tmux" }), false, "does not exist"},
		{"missing TMUX_TMPDIR: kill-server", []string{"kill-server"}, with(func(e *Env) { e.TmuxTmpdir = "/scratch/w1/gone/tmux" }), false, "explicit -S"},
		{"unset TMUX_TMPDIR falls back to live (env -i)", []string{"ls"}, with(func(e *Env) { e.TmuxTmpdir = "" }), false, "unset"},
		{"existing scratch TMUX_TMPDIR is fine for ls", []string{"ls"}, base, true, ""},
		{"existing TMUX_TMPDIR outside scratch is refused", []string{"ls"}, with(func(e *Env) { e.TmuxTmpdir = "/tmp" }), false, "outside your own scratch"},
		{"-L label under scratch dir", []string{"-L", "side", "ls"}, base, true, ""},
		{"-L label with escaping name", []string{"-L", "../../../tmp/tmux-1000/default", "ls"}, base, false, "outside your own scratch"},
		{"no scratch known: nothing implicit is trusted", []string{"ls"}, with(func(e *Env) { e.Scratch = "" }), false, "outside your own scratch"},
		{"bare kill-server even with a good TMUX_TMPDIR", []string{"kill-server"}, base, false, "explicit -S"},
		{"kill-server with -S under scratch", []string{"-S", "/scratch/w1/proof/s.sock", "kill-server"}, base, true, ""},
		{"kill-server with clustered -S", []string{"-uS/scratch/w1/proof/s.sock", "kill-server"}, base, true, ""},
		{"kill-server with relative -S under scratch", []string{"-S", "../s.sock", "kill-server"}, base, true, ""},
		{"kill-server on the live socket", []string{"-S", "/tmp/tmux-1000/default", "kill-server"}, base, false, "not"},
		{"kill-server with dot-dot escape", []string{"-S", "/scratch/w1/../../tmp/tmux-1000/default", "kill-server"}, base, false, "not"},
		{"kill-server on the scratch root itself", []string{"-S", "/scratch/w1", "kill-server"}, base, false, "not"},
		{"kill-server with empty -S", []string{"-S", "", "kill-server"}, base, false, "empty"},
		{"unique prefix kill-ser", []string{"-S", "/tmp/tmux-1000/default", "kill-ser"}, base, false, "kill-server"},
		{"kill-sess is kill-session, not kill-server", []string{"-S", "/tmp/x.sock", "kill-session", "-t", "a"}, base, true, ""},
		{"second command of a ; sequence", []string{"-S", "/tmp/tmux-1000/default", "ls", ";", "kill-server"}, base, false, "kill-server"},
		{"trailing ; separator", []string{"-S", "/tmp/tmux-1000/default", "ls;", "kill-server"}, base, false, "kill-server"},
		{"escaped \\; stays one command", []string{"-S", "/tmp/x.sock", "send-keys", `kill-server\;`, "Enter"}, base, true, ""},
		{"kill-server as send-keys text is not a command", []string{"-S", "/tmp/x.sock", "send-keys", "-t", "a", "kill-server", "Enter"}, base, true, ""},
		{"explicit -S elsewhere passes other commands", []string{"-S", "/tmp/tmux-1000/default", "list-panes", "-a"}, base, true, ""},
		{"-S wins over a missing TMUX_TMPDIR", []string{"-S", "/scratch/w1/p.sock", "new-session", "-d"}, with(func(e *Env) { e.TmuxTmpdir = "/gone" }), true, ""},
		{"no command = new-session, still needs a safe socket", nil, with(func(e *Env) { e.TmuxTmpdir = "/gone" }), false, "does not exist"},
		{"inside a scratch tmux client, $TMUX names the server", []string{"ls"}, with(func(e *Env) { e.Tmux = "/scratch/w1/p.sock,123,0"; e.TmuxTmpdir = "/gone" }), true, ""},
		{"inside the live tmux client, $TMUX is the live server", []string{"ls"}, with(func(e *Env) { e.Tmux = "/tmp/tmux-1000/default,123,0" }), false, "outside your own scratch"},
		{"unknown global option", []string{"-Z", "ls"}, base, false, "unknown tmux option"},
		{"option missing its value", []string{"-S"}, base, false, "needs a value"},
		{"-f takes a value and is skipped", []string{"-f", "/dev/null", "-S", "/scratch/w1/p.sock", "kill-server"}, base, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.args, tc.env)
			if got.Allow != tc.allow {
				t.Fatalf("Decide(%q) = %+v, want allow=%v", tc.args, got, tc.allow)
			}
			if !tc.allow && !strings.Contains(got.Reason, tc.reasonw) {
				t.Fatalf("reason = %q, want it to mention %q", got.Reason, tc.reasonw)
			}
		})
	}
}

func TestSocketSymlinkToLiveIsNotScratch(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "scratch")
	live := filepath.Join(root, "live", "default")
	for _, d := range []string{scratch, filepath.Dir(live)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(live, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scratch, "innocent.sock")
	if err := os.Symlink(live, link); err != nil {
		t.Fatal(err)
	}
	env := SystemEnv()
	env.Scratch, env.TmuxTmpdir, env.Tmux, env.Cwd = scratch, scratch, "", scratch

	if got := Decide([]string{"-S", link, "kill-server"}, env); got.Allow {
		t.Fatalf("a scratch symlink to the live socket must not pass: %+v", got)
	}
	if got := Decide([]string{"-S", filepath.Join(scratch, "new", "s.sock"), "kill-server"}, env); !got.Allow {
		t.Fatalf("a not-yet-existing socket below scratch must pass: %+v", got)
	}
	// A scratch that is itself reached through a symlink still matches.
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(scratch, alias); err != nil {
		t.Fatal(err)
	}
	if got := Decide([]string{"-S", filepath.Join(alias, "s.sock"), "kill-server"}, env); !got.Allow {
		t.Fatalf("socket through an alias of scratch must pass: %+v", got)
	}
}

func TestInstallWritesGuardedShimAndRealTmuxSkipsIt(t *testing.T) {
	state := t.TempDir()
	exe := filepath.Join(state, "mewla")
	dir, err := Install(state, exe)
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "tmux")
	info, err := os.Stat(shim)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("shim not executable: %v %v", info, err)
	}
	if !isGuard(shim) {
		t.Fatal("installed shim lacks the guard marker")
	}
	if again, err := Install(state, exe); err != nil || again != dir {
		t.Fatalf("reinstall = %q, %v", again, err)
	}

	// A different tmux later on PATH wins over the shim for the daemon.
	other := t.TempDir()
	fake := filepath.Join(other, "tmux")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := lookRealTmux(dir + string(os.PathListSeparator) + other); got != fake {
		t.Fatalf("lookRealTmux = %q, want %q (must skip the shim)", got, fake)
	}
	// A look-alike without the marker (a test firewall) is not skipped.
	if got := lookRealTmux(other + string(os.PathListSeparator) + dir); got != fake {
		t.Fatalf("lookRealTmux = %q, want the non-guard tmux", got)
	}
	if got := lookRealTmux(dir); got != "" {
		t.Fatalf("a PATH with only the shim has no real tmux, got %q", got)
	}
}

func TestWithoutGuardDropsOnlyGuardDirs(t *testing.T) {
	state := t.TempDir()
	dir, err := Install(state, filepath.Join(state, "mewla"))
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	sep := string(os.PathListSeparator)
	if got, want := WithoutGuard(dir+sep+other+sep+"/usr/bin"), other+sep+"/usr/bin"; got != want {
		t.Fatalf("WithoutGuard = %q, want %q", got, want)
	}
}
