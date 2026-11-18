//go:build linux

package watcher

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unsafe"
)

const hiddenArgsHelperEnv = "MEWLA_TEST_HIDDEN_ARGS_HELPER"

// TestHiddenArgsHelper is not a test: run as a child, it blanks its own
// command line (what /proc shows of a program that exec is still setting up),
// says "hidden", and on any input line restores it and says "shown".
func TestHiddenArgsHelper(t *testing.T) {
	if os.Getenv(hiddenArgsHelperEnv) != "1" {
		t.Skip("helper process")
	}
	var saved [][]byte
	for _, arg := range os.Args {
		memory := unsafe.Slice(unsafe.StringData(arg), len(arg))
		saved = append(saved, append([]byte(nil), memory...))
		clear(memory)
	}
	os.Stdout.WriteString("hidden\n")
	bufio.NewReader(os.Stdin).ReadString('\n')
	for i, arg := range os.Args {
		copy(unsafe.Slice(unsafe.StringData(arg), len(arg)), saved[i])
	}
	os.Stdout.WriteString("shown\n")
	time.Sleep(time.Minute)
}

func TestScanRereadsACommandLineThatWasUnreadable(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHiddenArgsHelper$", "--", "--session-id", "launch-marker")
	cmd.Env = append(os.Environ(), hiddenArgsHelperEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	lines := bufio.NewReader(stdout)
	expect := func(want string) {
		t.Helper()
		line, err := lines.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != want {
			t.Fatalf("helper said %q (%v), want %q", line, err, want)
		}
	}
	pid := cmd.Process.Pid
	expect("hidden")

	processes, err := snapshotProcessTableHot([]int{pid}, processColdAge)
	if err != nil {
		t.Fatal(err)
	}
	if args := processes[pid].args; strings.Contains(args, "launch-marker") {
		t.Fatalf("helper's command line was readable while hidden: %q", args)
	}

	if _, err := stdin.Write([]byte("show\n")); err != nil {
		t.Fatal(err)
	}
	expect("shown")
	// Well inside cmdlineRefresh: the unreadable reading must not be kept.
	processes, err = snapshotProcessTableHot([]int{pid}, processColdAge)
	if err != nil {
		t.Fatal(err)
	}
	if args := processes[pid].args; !strings.Contains(args, "--session-id launch-marker") {
		t.Fatalf("command line after it became readable = %q, want the real one", args)
	}
	if processes[pid].argsUnread {
		t.Fatal("a readable command line is still marked unread")
	}
}
