package platform

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

// ExecInPlace for real: a helper process notes how it was started, changes
// its environment and working directory, and runs itself again with what it
// noted. The second run must see exactly that; on Unix it is the same PID, on
// Windows a child of the first process, which exits with the child's code.

const execInPlaceDirEnv = "MEWLA_EXEC_IN_PLACE_DIR"

type execInPlaceRecord struct {
	PID  int      `json:"pid"`
	PPID int      `json:"ppid"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
	Dir  string   `json:"dir"`
}

func TestExecInPlaceHelper(t *testing.T) {
	dir := os.Getenv(execInPlaceDirEnv)
	if dir == "" {
		t.Skip("helper process for TestExecInPlaceKeepsArgsEnvAndDir")
	}
	args, env := append([]string(nil), os.Args...), os.Environ()
	wd, _ := os.Getwd()
	record := execInPlaceRecord{PID: os.Getpid(), PPID: os.Getppid(), Args: args, Dir: wd}
	for _, entry := range env {
		if strings.HasPrefix(entry, "MEWLA_EXEC_IN_PLACE_") {
			record.Env = append(record.Env, entry)
		}
	}
	data, _ := json.Marshal(record)
	first := filepath.Join(dir, "first.json")
	if _, err := os.Stat(first); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "second.json"), data, 0o600)
		os.Exit(7)
	}
	_ = os.WriteFile(first, data, 0o600)
	executable, _ := os.Executable()
	_ = os.Setenv("MEWLA_EXEC_IN_PLACE_MUTATED", "1")
	_ = os.Setenv("MEWLA_EXEC_IN_PLACE_ORIGINAL", "changed")
	_ = os.Chdir(dir)
	err := ExecInPlace(executable, args, env, wd, make(chan os.Signal, 1))
	_ = os.WriteFile(filepath.Join(dir, "exec-error.txt"), []byte(err.Error()), 0o600)
	os.Exit(3)
}

func TestExecInPlaceKeepsArgsEnvAndDir(t *testing.T) {
	if os.Getenv(execInPlaceDirEnv) != "" {
		t.Skip("inside the helper")
	}
	dir, startDir := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecInPlaceHelper$", "-test.count=1", "--", "keep", "two words")
	cmd.Env = append(os.Environ(), execInPlaceDirEnv+"="+dir, "MEWLA_EXEC_IN_PLACE_ORIGINAL=yes")
	cmd.Dir = startDir
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		failure, _ := os.ReadFile(filepath.Join(dir, "exec-error.txt"))
		t.Fatalf("helper ended with %v, want exit 7 from the second run; exec error %q\n%s", err, failure, output)
	}
	read := func(name string) execInPlaceRecord {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, output)
		}
		var record execInPlaceRecord
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	before, after := read("first.json"), read("second.json")
	if runtime.GOOS == "windows" {
		if after.PID == before.PID || after.PPID != before.PID {
			t.Fatalf("second run %d (parent %d), first %d: want a child of the first", after.PID, after.PPID, before.PID)
		}
	} else if after.PID != before.PID {
		t.Fatalf("PID %d became %d: want the same process", before.PID, after.PID)
	}
	if !slices.Equal(after.Args, cmd.Args) {
		t.Fatalf("args %q, want %q", after.Args, cmd.Args)
	}
	wantEnv := []string{execInPlaceDirEnv + "=" + dir, "MEWLA_EXEC_IN_PLACE_ORIGINAL=yes"}
	slices.Sort(wantEnv)
	slices.Sort(after.Env)
	if !slices.Equal(after.Env, wantEnv) {
		t.Fatalf("environment %q, want %q", after.Env, wantEnv)
	}
	resolve := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	if !strings.EqualFold(resolve(after.Dir), resolve(startDir)) {
		t.Fatalf("working directory %q, want %q", after.Dir, startDir)
	}
}
