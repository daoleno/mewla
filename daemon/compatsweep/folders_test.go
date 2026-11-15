//go:build linux || darwin

package compatsweep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingAgent is a stand-in agent CLI that writes where it runs and its
// arguments to out/<pid>.json and then idles: enough to see what Mewla
// launched, where, with what, without a model.
func recordingAgent(t *testing.T, path, out string) {
	t.Helper()
	// Plain sh, so it runs on a machine with nothing else installed: the
	// folder and the NUL-separated arguments, then the record appears whole.
	script := `#!/bin/sh
[ "$1" = --version ] && { echo '2.1.325 (Claude Code)'; exit 0; }
[ "$1" = auth ] && { echo '{"loggedIn":true}'; exit 0; }
f="` + out + `/$$"
pwd > "$f.cwd.tmp"
printf '%s\0' "$@" > "$f.args"
mv "$f.cwd.tmp" "$f.cwd"
exec sleep 600
`
	writeFile(t, path, script, 0o755)
}

type launch struct {
	Cwd  string
	Argv []string
}

// launches are the recording agent's launches so far, in no order.
func launches(out string) []launch {
	entries, _ := os.ReadDir(out)
	var all []launch
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".cwd") {
			continue
		}
		base := filepath.Join(out, strings.TrimSuffix(name, ".cwd"))
		cwd, err := os.ReadFile(base + ".cwd")
		if err != nil {
			continue
		}
		args, _ := os.ReadFile(base + ".args")
		got := launch{Cwd: strings.TrimRight(string(cwd), "\n")}
		for _, arg := range strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00") {
			if arg != "" {
				got.Argv = append(got.Argv, arg)
			}
		}
		all = append(all, got)
	}
	return all
}

// waitLaunch waits for a Worker's launch: Brain's host (--add-dir) and probes
// are not one.
func waitLaunch(out string, timeout time.Duration) (launch, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, got := range launches(out) {
			if strings.Contains(strings.Join(got.Argv, " "), "--mcp-config") {
				return got, true
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return launch{}, false
}

func clearLaunches(t *testing.T, out string) {
	entries, _ := os.ReadDir(out)
	for _, entry := range entries {
		_ = os.Remove(filepath.Join(out, entry.Name()))
	}
}

// TestWorkerFolders spawns a Worker in folders a stranger's computer has:
// spaces, CJK, a very long path, a symlink, no write permission, one that does
// not exist, and one named with every shell metacharacter. Each either starts
// the agent in exactly that folder or fails with a reason the caller sees.
func TestWorkerFolders(t *testing.T) {
	e := newEnv(t, envOptions{
		executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
		hostExecutor: "claude",
	})
	out := filepath.Join(e.dir, "launches")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	recordingAgent(t, filepath.Join(e.tools, "claude"), out)
	e.start()

	root := filepath.Join(e.dir, "folders")
	long := filepath.Join(root, "long")
	for len(long) < 900 {
		long = filepath.Join(long, strings.Repeat("segment-", 12))
	}
	cases := []struct {
		name, dir string
		setup     func(dir string) error
		wantErr   bool
	}{
		{name: "spaces", dir: filepath.Join(root, "my project dir")},
		{name: "cjk", dir: filepath.Join(root, "项目 中文", "日本語フォルダ")},
		{name: "very-long", dir: long},
		{name: "metachar", dir: filepath.Join(root, `a%b "q" 'x' & $HOME `+"`bt`"+` ^c ! ;(z)`)},
		{name: "symlink", dir: filepath.Join(root, "link-to-real"), setup: func(dir string) error {
			real := filepath.Join(root, "real target")
			if err := os.MkdirAll(real, 0o700); err != nil {
				return err
			}
			return os.Symlink(real, dir)
		}},
		{name: "no-write", dir: filepath.Join(root, "readonly"), setup: func(dir string) error {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			return os.Chmod(dir, 0o555)
		}},
		{name: "nonexistent", dir: filepath.Join(root, "does", "not", "exist"), setup: func(string) error { return nil }, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearLaunches(t, out)
			setup := tc.setup
			if setup == nil {
				setup = func(dir string) error { return os.MkdirAll(dir, 0o700) }
			}
			if err := setup(tc.dir); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(tc.dir, 0o700)
			response, err := e.cli(60*time.Second, "worker", "spawn", "@state", "-name", "Sweep "+tc.name, "-executor", "claude",
				"-cwd", tc.dir, "-prompt", "sweep folder check", "-profile", "quick")
			if tc.wantErr {
				ok := err != nil && strings.Contains(response, "\"ok\": false")
				outcome := map[bool]string{true: "pass", false: "fail"}[ok]
				record(t, "folder-"+tc.name, outcome, "spawn refused: "+oneLine(response))
				if !ok {
					t.Fatalf("spawn in a missing folder did not fail clearly: err=%v\n%s", err, response)
				}
				return
			}
			if err != nil {
				record(t, "folder-"+tc.name, "fail", "spawn failed: "+oneLine(response))
				t.Fatalf("spawn: %v\n%s", err, response)
			}
			got, ok := waitLaunch(out, 30*time.Second)
			want, _ := filepath.EvalSymlinks(tc.dir)
			if !ok || (got.Cwd != tc.dir && got.Cwd != want) {
				record(t, "folder-"+tc.name, "fail", fmt.Sprintf("agent ran in %q (launched=%v), want %q", got.Cwd, ok, tc.dir))
				t.Fatalf("agent cwd = %q (launched=%v), want %q\n%s", got.Cwd, ok, tc.dir, e.logTail(20))
			}
			record(t, "folder-"+tc.name, "pass", fmt.Sprintf("agent ran in the folder; argv=%q", got.Argv))
		})
	}
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 400 {
		text = text[:400] + "…"
	}
	return text
}
