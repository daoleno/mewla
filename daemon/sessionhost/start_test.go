//go:build linux || darwin

package sessionhost

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testStartOptions(t *testing.T, hostMode string) StartOptions {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := shortTempDir(t)
	return StartOptions{
		SocketPath:    filepath.Join(dir, "h.sock"),
		Executable:    exe,
		Env:           append(os.Environ(), testHostEnv+"="+hostMode),
		LogPath:       filepath.Join(dir, "logs", "host.log"),
		ClientVersion: "test-daemon",
		Wait:          10 * time.Second,
	}
}

func TestConnectStartsADetachedHostOnce(t *testing.T) {
	opts := testStartOptions(t, "serve")
	ctx := ctxT(t)
	client, started, err := Connect(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	host := client.Welcome().PID
	t.Cleanup(func() {
		if c, err := Dial(context.Background(), opts.SocketPath, "cleanup"); err == nil {
			_ = c.Shutdown(context.Background(), true)
			c.Close()
		}
		waitFor(t, 10*time.Second, "test host gone", func() bool { return !processAlive(host) })
	})
	if !started || host == os.Getpid() || client.Welcome().HostVersion != "test-host" {
		t.Fatalf("started=%v welcome=%+v", started, client.Welcome())
	}
	if runtime.GOOS == "linux" {
		// It leads its own session: no signal to the caller's group reaches it.
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(host), "stat"))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) < 4 || fields[3] != strconv.Itoa(host) {
			t.Fatalf("host %d is not a session leader: %s", host, stat)
		}
	}
	if _, err := client.Create(ctx, shellSession("kept", shortTempDir(t), "sleep 30")); err != nil {
		t.Fatal(err)
	}
	client.Close()

	again, started, err := Connect(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if started || again.Welcome().PID != host {
		t.Fatalf("second Connect started=%v pid=%d, want the running host %d", started, again.Welcome().PID, host)
	}
	if list, _ := again.List(ctx); len(list) != 1 || list[0].ID != "kept" {
		t.Fatalf("sessions on the running host: %+v", list)
	}
	if _, err := os.Stat(opts.LogPath); err != nil {
		t.Fatalf("host log: %v", err)
	}
}

func TestConnectReportsAHostThatExitsAtStart(t *testing.T) {
	opts := testStartOptions(t, "fail")
	_, _, err := Connect(ctxT(t), opts)
	if err == nil || !strings.Contains(err.Error(), "exited at start") || !strings.Contains(err.Error(), opts.LogPath) {
		t.Fatalf("Connect to a host that fails: %v", err)
	}
	log, _ := os.ReadFile(opts.LogPath)
	if !strings.Contains(string(log), "failing at start as asked") {
		t.Fatalf("host log = %q", log)
	}
}

func TestConnectDoesNotStartAHostOverAnotherUsersSocket(t *testing.T) {
	h := startHost(t, ServerOptions{})
	saved := serverUIDAllowed
	serverUIDAllowed = func(uint32) bool { return false }
	defer func() { serverUIDAllowed = saved }()
	opts := testStartOptions(t, "serve")
	opts.SocketPath = h.path
	if _, started, err := Connect(ctxT(t), opts); err == nil || started {
		t.Fatalf("Connect over a refused socket: started=%v err=%v", started, err)
	}
	if _, err := os.Stat(opts.LogPath); err == nil {
		t.Fatal("a host was started")
	}
}
