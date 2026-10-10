package workerproc

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestEnvironDescendantsAndOpenFilesOfRealProcesses(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.Env = append(os.Environ(), "MEWLA_INSPECT_MARKER=present")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	defer func() {
		for _, pid := range Descendants(cmd.Process.Pid) {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
		}
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	// Until the child execs, /proc shows the forking parent's environment.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !slices.Contains(Environ(cmd.Process.Pid), "MEWLA_INSPECT_MARKER=present") {
		time.Sleep(10 * time.Millisecond)
	}
	if !slices.Contains(Environ(cmd.Process.Pid), "MEWLA_INSPECT_MARKER=present") {
		t.Fatal("Environ did not return the child's environment")
	}
	var children []int
	for time.Now().Before(deadline) && len(children) == 0 {
		children = Descendants(cmd.Process.Pid)
		time.Sleep(10 * time.Millisecond)
	}
	if len(children) != 1 {
		t.Fatalf("Descendants = %v, want the one sleep child", children)
	}

	path := filepath.Join(t.TempDir(), "held-open")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !slices.Contains(OpenFiles(os.Getpid()), path) {
		t.Fatalf("OpenFiles(self) does not include %s", path)
	}
	if Environ(0) != nil || Descendants(0) != nil || OpenFiles(0) != nil {
		t.Fatal("pid 0 must yield nothing")
	}
}

func TestReadProcStatReportsZombiesAndMissingProcesses(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	// Until Wait reaps it, the exited child is a zombie.
	deadline := time.Now().Add(2 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		if _, err = ReadProcStat(cmd.Process.Pid); errors.Is(err, ErrExited) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !errors.Is(err, ErrExited) {
		t.Fatalf("zombie ReadProcStat err = %v, want ErrExited", err)
	}
	if _, ok := StartTime(cmd.Process.Pid); !ok {
		t.Fatal("a zombie keeps its start time")
	}
	_, _ = cmd.Process.Wait()
	if _, err := ReadProcStat(cmd.Process.Pid); !os.IsNotExist(err) {
		t.Fatalf("reaped ReadProcStat err = %v, want not-exist", err)
	}

	self, err := ReadProcStat(os.Getpid())
	if err != nil || self.PID != os.Getpid() || self.Start == "" {
		t.Fatalf("ReadProcStat(self) = %+v, %v", self, err)
	}
}
