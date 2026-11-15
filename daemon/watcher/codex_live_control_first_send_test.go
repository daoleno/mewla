package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// codexNativeFixtureSource is the fake installed-Codex native binary. It
// mirrors the real @openai/codex 0.147 split: the TUI client half reads the
// pane's stdin and records every received line; the headless app-server half
// never reads stdin (its output is redirected to a log by the launch).
const codexNativeFixtureSource = `package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		time.Sleep(24 * time.Hour)
		return
	}
	// Render a codex-ready composer + model footer so the readiness gate
	// converges before the first send.
	go func() {
		for {
			fmt.Print("\x1b[2J\x1b[H› \nmodel: gpt-5.6-sol · 100% context left\n")
			time.Sleep(100 * time.Millisecond)
		}
	}()
	record := os.Getenv("MEWLA_TEST_CODEX_RECORD")
	f, err := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		fmt.Fprintln(f, line)
	}
}
`

// codexWrapperFixtureSource is the fake installed-Codex node wrapper
// (bin/codex.js in the real npm package): comm=node, argv carries the wrapper
// path, and it spawns the native binary descendant with argv passed through.
const codexWrapperFixtureSource = `#!/usr/bin/env node
const { spawn } = require("node:child_process");
const child = spawn(process.env.MEWLA_TEST_CODEX_NATIVE, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});
child.on("exit", (code, signal) => {
  if (signal) {
    try { process.kill(process.pid, signal); } catch { process.exit(1); }
  } else {
    process.exit(code ?? 1);
  }
});
["SIGINT", "SIGTERM", "SIGHUP"].forEach((sig) => {
  process.on(sig, () => { try { child.kill(sig); } catch {} });
});
`

// buildFakeCodexInstall materializes the real installed-Codex process shape:
// a node wrapper at <root>/bin/codex (comm=node, argv contains "/bin/codex")
// that spawns a compiled native binary at <root>/native/codex (comm=codex).
// It returns the wrapper path, the native path, and the stdin record path.
func buildFakeCodexInstall(t *testing.T, root string) (wrapper, native, record string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; cannot build the installed-Codex wrapper shape")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available; cannot build the native codex fixture")
	}
	_ = node
	binDir := filepath.Join(root, "bin")
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper = filepath.Join(binDir, "codex")
	if err := os.WriteFile(wrapper, []byte(codexWrapperFixtureSource), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(nativeDir, "main.go")
	if err := os.WriteFile(src, []byte(codexNativeFixtureSource), 0o600); err != nil {
		t.Fatal(err)
	}
	native = filepath.Join(nativeDir, "codex")
	build := exec.Command("go", "build", "-o", native, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native codex fixture: %v: %s", err, out)
	}
	record = filepath.Join(root, "codex-stdin.rec")
	return wrapper, native, record
}

// liveControlProcessTreeComplete reports whether the pane's foreground group
// holds the full real live-control tree: the node-wrapper TUI (pane process),
// its native --remote descendant, the node-wrapper app-server sibling, and its
// native app-server descendant. Node 24 reports the wrapper comm as
// "MainThread", so the wrapper halves are recognized by argv role only.
func liveControlProcessTreeComplete(panePID int, processes map[int]processInfo) bool {
	nativeTUI := false
	nativeServer := false
	siblingCount := 0
	for _, proc := range processes {
		if proc.pgid != panePID || proc.tpgid != panePID || proc.pid == panePID || proc.ppid != panePID {
			continue
		}
		if workerCommandFromProcess(proc) == "" {
			return false
		}
		siblingCount++
		if isCodexAppServerProcess(proc.args) {
			nativeServer = true
		}
		if proc.comm == "codex" && isCodexTUIClientProcess(proc.args) {
			nativeTUI = true
		}
	}
	// The two sibling subtrees: native TUI client and app-server wrapper
	// (whose own native descendant is a grandchild, not a pane child).
	return siblingCount >= 2 && nativeTUI && nativeServer
}
