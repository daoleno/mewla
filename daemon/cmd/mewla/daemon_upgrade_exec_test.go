package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The re-exec for real: a helper process changes its environment and working
// directory the way a running daemon does, then runs execInPlace. What starts
// must see the arguments, environment and directory the helper was started
// with; on Linux and macOS it is the same PID, on Windows a child of the old
// process, whose exit code the old process passes on.

const reexecTestDirEnv = "MEWLA_REEXEC_TEST_DIR"

type reexecRecord struct {
	PID  int      `json:"pid"`
	PPID int      `json:"ppid"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
	Dir  string   `json:"dir"`
}

func TestReexecHelper(t *testing.T) {
	dir := os.Getenv(reexecTestDirEnv)
	if dir == "" {
		t.Skip("helper process for TestInPlaceRestartKeepsArgsEnvAndDir")
	}
	wd, _ := os.Getwd()
	record := reexecRecord{PID: os.Getpid(), PPID: os.Getppid(), Args: os.Args, Dir: wd}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "MEWLA_REEXEC_") {
			record.Env = append(record.Env, entry)
		}
	}
	data, _ := json.Marshal(record)
	stage1 := filepath.Join(dir, "stage1.json")
	if _, err := os.Stat(stage1); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "stage2.json"), data, 0o600)
		os.Exit(7)
	}
	_ = os.WriteFile(stage1, data, 0o600)
	// What a daemon does while it runs.
	_ = os.Setenv("MEWLA_REEXEC_MUTATED", "1")
	_ = os.Setenv("MEWLA_REEXEC_ORIGINAL", "changed")
	_ = os.Chdir(dir)
	err := execInPlace(daemonStartup, make(chan os.Signal, 1))
	_ = os.WriteFile(filepath.Join(dir, "exec-error.txt"), []byte(err.Error()), 0o600)
	os.Exit(3)
}

func TestInPlaceRestartKeepsArgsEnvAndDir(t *testing.T) {
	if os.Getenv(reexecTestDirEnv) != "" {
		t.Skip("inside the helper")
	}
	dir, startDir := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestReexecHelper$", "-test.count=1", "--", "keep", "two words")
	cmd.Env = append(os.Environ(), reexecTestDirEnv+"="+dir, "MEWLA_REEXEC_ORIGINAL=yes")
	cmd.Dir = startDir
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		failure, _ := os.ReadFile(filepath.Join(dir, "exec-error.txt"))
		t.Fatalf("helper ended with %v, want exit 7 from the new process; exec error %q\n%s", err, failure, output)
	}
	read := func(name string) reexecRecord {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, output)
		}
		var record reexecRecord
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	before, after := read("stage1.json"), read("stage2.json")
	if runtime.GOOS == "windows" {
		if after.PID == before.PID || after.PPID != before.PID {
			t.Fatalf("new process %d (parent %d), old %d: want a child of the old process", after.PID, after.PPID, before.PID)
		}
	} else if after.PID != before.PID {
		t.Fatalf("PID %d became %d: want the same process", before.PID, after.PID)
	}
	if !slices.Equal(after.Args, cmd.Args) {
		t.Fatalf("args %q, want %q", after.Args, cmd.Args)
	}
	wantEnv := []string{reexecTestDirEnv + "=" + dir, "MEWLA_REEXEC_ORIGINAL=yes"}
	slices.Sort(wantEnv)
	slices.Sort(after.Env)
	if !slices.Equal(after.Env, wantEnv) {
		t.Fatalf("environment %q, want %q", after.Env, wantEnv)
	}
	resolve := func(path string) string {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return path
		}
		return resolved
	}
	if resolve(after.Dir) != resolve(startDir) {
		t.Fatalf("working directory %q, want %q", after.Dir, startDir)
	}
}
