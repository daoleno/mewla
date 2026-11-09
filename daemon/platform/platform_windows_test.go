//go:build windows

package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A job ends the whole tree: a child that starts its own child is gone
// with it, which is what a session's Kill must do.
func TestJobTerminatesTheTree(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := job.AssignProcess(cmd.Process); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var pids []int
	for time.Now().Before(deadline) {
		if pids, err = job.PIDs(); err != nil {
			t.Fatal(err)
		}
		if len(pids) >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(pids) < 2 {
		t.Fatalf("job holds %v; want cmd and its ping child", pids)
	}
	if err := job.Terminate(1); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	for _, pid := range pids {
		for i := 0; i < 50 && ProcessAlive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if ProcessAlive(pid) {
			t.Fatalf("pid %d survived the job", pid)
		}
	}
}

func TestProcessStartTimeIsRecent(t *testing.T) {
	start, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if age := time.Since(start); age < 0 || age > time.Hour {
		t.Fatalf("this process started %s ago", age)
	}
}

// Windows refuses to overwrite a running executable; ReplaceFile moves it
// aside instead, and the sweep leaves the copy that is still running.
func TestReplaceFileOverARunningExecutable(t *testing.T) {
	dir := t.TempDir()
	target := dir + `\running.exe`
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(target, "-test.run", "^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "MEWLA_PLATFORM_HELPER=sleep")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(500 * time.Millisecond)
	if err := os.WriteFile(dir+`\staged`, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+`\staged`, target); err == nil {
		t.Skip("this Windows renamed over a running image; the aside path is not exercised")
	}
	if err := os.WriteFile(dir+`\staged`, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(dir+`\staged`, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("target holds %d bytes, not the new file", len(got))
	}
	SweepReplaced(target)
	matches, _ := filepathGlob(target + replacedMarker + "*")
	if len(matches) != 1 {
		t.Fatalf("aside copies = %v; want the running one kept", matches)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	time.Sleep(300 * time.Millisecond)
	SweepReplaced(target)
	if matches, _ := filepathGlob(target + replacedMarker + "*"); len(matches) != 0 {
		t.Fatalf("aside copies after exit = %v", matches)
	}
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("MEWLA_PLATFORM_HELPER") != "sleep" {
		t.Skip("helper process")
	}
	time.Sleep(60 * time.Second)
}

// Nothing serves a socket in a folder that does not exist yet (the host's
// before its first start), a missing socket file or a stale one.
func TestNoListenerOnEveryAbsentSocket(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.sock")
	l, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	for _, path := range []string{filepath.Join(dir, "no-folder", "h.sock"), filepath.Join(dir, "h.sock"), stale} {
		_, err := net.Dial("unix", path)
		if err == nil || !NoListener(err) {
			t.Errorf("dial %s = %v; NoListener false", path, err)
		}
	}
}

// Claude's process registry records this FILETIME; a finished process has none.
func TestProcessCreationFiletime(t *testing.T) {
	self, err := ProcessCreationFiletime(os.Getpid())
	if err != nil || self == 0 {
		t.Fatalf("this process = %d, %v", self, err)
	}
	started, _ := ProcessStartTime(os.Getpid())
	if got := time.Unix(0, int64(self-116444736000000000)*100); !got.Equal(started) {
		t.Fatalf("FILETIME %d is %v; start time is %v", self, got, started)
	}
	child := exec.Command("cmd.exe", "/d", "/c", "exit 0")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessCreationFiletime(child.Process.Pid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a finished process = %v", err)
	}
}

// Codex's app-server refuses a socket directory unless its ACL is exactly the
// one Codex itself would set; see MkdirPrivate.
func TestMkdirPrivateMatchesCodexSocketDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "codex-ctl")
	for round := 0; round < 2; round++ { // creating it, then repairing it
		if err := MkdirPrivate(dir); err != nil {
			t.Fatal(err)
		}
		sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		_, sid, _ := CurrentUser()
		want := fmt.Sprintf("O:%sD:P(A;OICI;FA;;;%s)", sid, sid)
		if got := sd.String(); got != want {
			t.Fatalf("round %d: descriptor %s, want %s", round, got, want)
		}
	}
}
