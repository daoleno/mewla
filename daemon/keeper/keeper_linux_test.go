//go:build linux

package keeper

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary is the keeper's own program in these tests, as mewla is in
// the daemon: a keeper process it starts runs Main.
func TestMain(m *testing.M) {
	if os.Getenv(SpecEnv) != "" {
		if err := Main(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	inProcess = false
	os.Exit(m.Run())
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "kp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// A keeper runs its program in a process of its own; a client may come and
// go; with nobody attached for its linger time it stops the program and ends.
func TestKeeperOutlivesItsClientThenLingersOut(t *testing.T) {
	dir := shortDir(t)
	spec := Spec{Socket: filepath.Join(dir, "k.sock"), Linger: 2 * time.Second, Argv: []string{"sleep", "300"}, Log: filepath.Join(dir, "k.log"), Cleanup: []string{filepath.Join(dir, "gone")}}
	if err := os.WriteFile(filepath.Join(dir, "gone"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Spawn(spec); err != nil {
		t.Fatal(err)
	}
	c, err := Attach(spec.Socket)
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Pid
	if !alive(pid) {
		t.Fatal("the program is not running")
	}
	c.Close()
	// A new client (a restarted daemon) finds the same program.
	time.Sleep(500 * time.Millisecond)
	again, err := Attach(spec.Socket)
	if err != nil || again.Pid != pid {
		t.Fatalf("reattach = %v, pid %d want %d", err, again.Pid, pid)
	}
	time.Sleep(3 * time.Second)
	if !alive(pid) {
		t.Fatal("the program stopped while a client was attached")
	}
	again.Close()
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("the program outlived the linger time with nobody attached")
	}
	time.Sleep(300 * time.Millisecond)
	for _, p := range []string{spec.Socket, filepath.Join(dir, "gone")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s was left behind", p)
		}
	}
	if !strings.Contains(Tail(spec.Log), "nobody attached") {
		t.Fatalf("log = %q", Tail(spec.Log))
	}
}

// A CDP keeper relays the program's pipe (here one that echoes what it is
// sent) to whoever is attached, and keeps it across a client that leaves.
// Stop sends Browser.close first, then ends the program.
func TestKeeperRelaysThePipeAcrossClients(t *testing.T) {
	dir := shortDir(t)
	spec := Spec{Socket: filepath.Join(dir, "k.sock"), Linger: time.Minute, CDP: true, Argv: []string{"sh", "-c", "cat <&3 >&4"}, Log: filepath.Join(dir, "k.log"), Lock: filepath.Join(dir, "lock")}
	if err := Spawn(spec); err != nil {
		t.Fatal(err)
	}
	echo := func(msg string) {
		t.Helper()
		c, err := Attach(spec.Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.Write([]byte(msg + "\x00")); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		got, err := bufio.NewReader(c).ReadString(0)
		if err != nil || got != msg+"\x00" {
			t.Fatalf("relay = %q, %v", got, err)
		}
	}
	echo(`{"id":1,"method":"Browser.getVersion"}`)
	echo(`{"id":2,"method":"Browser.getVersion"}`)
	// A second runtime on the same lock is refused.
	other := spec
	other.Socket = filepath.Join(dir, "o.sock")
	other.Log = filepath.Join(dir, "o.log")
	if err := Spawn(other); err == nil || !strings.Contains(err.Error(), "held by another runtime") {
		t.Fatalf("a second keeper on the same lock = %v", err)
	}
	c, err := Attach(spec.Socket)
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Pid
	c.Close()
	if err := Stop(spec.Socket); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if alive(pid) {
		t.Fatal("Stop left the program running")
	}
	if _, err := Attach(spec.Socket); err == nil {
		t.Fatal("the keeper is still there after Stop")
	}
}

// A keeper that is sent SIGTERM stops its program first, so killing the keeper
// never leaves the program running without one.
func TestATerminatedKeeperStopsItsProgram(t *testing.T) {
	dir := shortDir(t)
	spec := Spec{Socket: filepath.Join(dir, "k.sock"), Linger: time.Minute, Argv: []string{"sleep", "300"}, Log: filepath.Join(dir, "k.log")}
	if err := Spawn(spec); err != nil {
		t.Fatal(err)
	}
	c, err := Attach(spec.Socket)
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Pid
	c.Close()
	keeperPid := 0
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		fields := strings.Fields(string(raw))
		keeperPid, _ = strconv.Atoi(fields[3])
	}
	if keeperPid <= 1 {
		t.Fatalf("no keeper process above the program %d", pid)
	}
	if err := syscall.Kill(keeperPid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for (alive(pid) || alive(keeperPid)) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("the program outlived its terminated keeper")
	}
}
