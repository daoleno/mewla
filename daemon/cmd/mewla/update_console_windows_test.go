package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/selfupdate"
	"github.com/daoleno/mewla/daemon/statedir"
	"golang.org/x/sys/windows"
)

// An old updater starts the new binary detached. Its synchronous probes need a
// windowless console immediately. After the next handover even ordinary children
// should inherit a console from the new daemon.
func TestWindowsUpdateConsole(t *testing.T) {
	const role = "MEWLA_TEST_UPDATE_CONSOLE"
	const result = "MEWLA_TEST_UPDATE_RESULT"
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(mode string) *exec.Cmd {
		cmd := exec.Command(self, "-test.run=^TestWindowsUpdateConsole$")
		cmd.Env = append(os.Environ(), role+"="+mode)
		return cmd
	}
	switch os.Getenv(role) {
	case "detached":
		if cp, _ := windows.GetConsoleCP(); cp != 0 {
			t.Fatal("legacy updater's child already has a console")
		}
		cmd := platform.WithoutConsoleWindow(child("probe"))
		out, err := cmd.Output()
		if err != nil || !bytes.Contains(out, []byte("probe completed")) {
			t.Fatalf("supervised probe: %q, %v", out, err)
		}
		dir := filepath.Dir(self)
		statedir.SetRoot(dir)
		t.Setenv(role, "successor")
		h := &updateHandover{Installed: self, Staged: self + selfupdate.StagedSuffix, StateDir: dir,
			Addr: os.Getenv("MEWLA_TEST_UPDATE_ADDR"), Version: "2.0.0", DaemonID: "lab"}
		t.Fatal(runUpdateHandover(h, os.Args[1:]))
	case "probe", "inherited":
		var pids [16]uint32
		n, _, callErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList").Call(
			uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
		if n == 0 || n > uintptr(len(pids)) {
			t.Fatalf("console processes: %d, %v", n, callErr)
		}
		if os.Getenv(role) == "inherited" && !slices.Contains(pids[:n], uint32(os.Getppid())) {
			t.Fatalf("ordinary child got a new console: parent %d, members %v", os.Getppid(), pids[:n])
		}
		// The Win32 console can have a message-only HWND on newer systems.
		hwnd, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		visible, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("IsWindowVisible").Call(hwnd)
		if visible != 0 {
			t.Fatal("background probe has a visible console window")
		}
		os.Stdout.WriteString("probe completed\n")
	case "successor":
		if os.Getenv(selfupdate.ChildEnv) != "" {
			if os.Getenv("MEWLA_TEST_UPDATE_OUTCOME") == "rollback" {
				os.Exit(17)
			}
			t.Fatal(http.ListenAndServe(os.Getenv("MEWLA_TEST_UPDATE_ADDR"), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"version":"2.0.0","daemon_id":"lab"}`)
			})))
		}
		out, err := child("inherited").CombinedOutput()
		if err != nil {
			t.Fatalf("successor's ordinary child: %s, %v", out, err)
		}
		if err := os.WriteFile(os.Getenv(result), out, 0600); err != nil {
			t.Fatal(err)
		}
	default:
		image, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		for _, outcome := range []string{"adopt", "rollback"} {
			t.Run(outcome, func(t *testing.T) {
				dir := t.TempDir()
				installed := filepath.Join(dir, "mewla.exe")
				for _, path := range []string{installed, installed + selfupdate.StagedSuffix} {
					if err := os.WriteFile(path, image, 0700); err != nil {
						t.Fatal(err)
					}
				}
				// A retained Session host can still map the previous hard-linked
				// executable. Start from a mapped image with that extra link.
				if err := os.Link(installed, installed+selfupdate.PrevSuffix); err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("MEWLA_TEST_UPDATE_ADDR", listener.Addr().String())
				listener.Close()
				t.Setenv("MEWLA_TEST_UPDATE_OUTCOME", outcome)
				path := filepath.Join(dir, "result")
				t.Setenv(result, path)
				cmd := child("detached")
				cmd.Path, cmd.Args[0] = installed, installed
				cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
				// The replacement inherits these pipes. CombinedOutput waits for EOF
				// after both processes exit; WaitDelay bounds a broken handover.
				cmd.WaitDelay = 15 * time.Second
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("handover: %s, %v", out, err)
				} else if b, err := os.ReadFile(path); err != nil || !bytes.Contains(b, []byte("probe completed")) {
					t.Fatalf("successor did not finish: %q, %v\n%s", b, err, out)
				}
				if outcome == "adopt" {
					if _, err := os.Stat(installed + selfupdate.PrevSuffix); err != nil {
						t.Fatal(err)
					}
				} else if failure, ok := selfupdate.LastFailure(dir); !ok || !failure.Permanent() || failure.Version != "2.0.0" {
					t.Fatalf("missing failed trial: %+v", failure)
				}
			})
		}
	}
}
