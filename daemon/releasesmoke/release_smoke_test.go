//go:build linux || darwin

package releasesmoke

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

func TestReleaseBinary(t *testing.T) {
	bin := os.Getenv("MEWLA_RELEASE_SMOKE_BINARY")
	if bin == "" {
		t.Skip("set MEWLA_RELEASE_SMOKE_BINARY to a built mewla release binary")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("MEWLA_RELEASE_SMOKE_VERSION")

	// A short private directory: macOS limits socket paths to 104 bytes, and
	// the host refuses a socket directory other users can open.
	dir, err := os.MkdirTemp("/tmp", "mrs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// macOS's /tmp is a symlink, and the daemon refuses state under one.
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TERM=xterm-256color", "LANG=C.UTF-8"}

	t.Run("version", func(t *testing.T) {
		out, err := exec.Command(bin, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("mewla version: %v\n%s", err, out)
		}
		t.Logf("%s", bytes.TrimSpace(out))
		if want != "" && !strings.Contains(string(out), "mewla "+want+" ") {
			t.Fatalf("mewla version = %q, want version %s", out, want)
		}
		if platform := runtime.GOOS + "/" + runtime.GOARCH; !strings.Contains(string(out), platform) {
			t.Fatalf("mewla version = %q, want platform %s", out, platform)
		}
	})

	state := filepath.Join(dir, "state")
	sock := filepath.Join(dir, "h.sock")
	hostLog, err := os.Create(filepath.Join(dir, "host.log"))
	if err != nil {
		t.Fatal(err)
	}
	host := exec.Command(bin, "host", "--state-dir", state, "--socket", sock)
	host.Env = env
	host.Dir = dir
	host.Stdout, host.Stderr = hostLog, hostLog
	if err := host.Start(); err != nil {
		t.Fatalf("start mewla host: %v", err)
	}
	hostDone := make(chan error, 1)
	go func() { hostDone <- host.Wait() }()
	t.Cleanup(func() {
		select {
		case <-hostDone:
		default:
			_ = host.Process.Kill()
			<-hostDone
		}
		if t.Failed() {
			log, _ := os.ReadFile(hostLog.Name())
			t.Logf("host log:\n%s", log)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var client *sessionhost.Client
	for deadline := time.Now().Add(15 * time.Second); ; {
		dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
		client, err = sessionhost.Dial(dialCtx, sock, "release-smoke")
		dialCancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session host never answered on %s: %v", sock, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	defer client.Close()

	t.Run("host has the terminal engine", func(t *testing.T) {
		w := client.Welcome()
		t.Logf("host %s protocol %d engine %v %s", w.HostVersion, w.Protocol, w.Engine, w.EngineVersion)
		if !w.Engine {
			t.Fatal("the release binary's session host has no terminal engine: built without the ghosttyvt tag or cgo")
		}
		if want != "" && w.HostVersion != want {
			t.Fatalf("host version %q, want %q", w.HostVersion, want)
		}
		out, err := exec.Command(bin, "host", "status", "--state-dir", state, "--socket", sock).CombinedOutput()
		if err != nil {
			t.Fatalf("mewla host status: %v\n%s", err, out)
		}
		t.Logf("%s", bytes.TrimSpace(out))
	})

	create := func(t *testing.T, id string, argv ...string) {
		t.Helper()
		if _, err := client.Create(ctx, sessionhost.CreateParams{ID: id, Argv: argv, Env: env, Dir: dir, Cols: 100, Rows: 30}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	input := func(t *testing.T, id string, steps ...sessionhost.InputStep) {
		t.Helper()
		if _, err := client.Input(ctx, id, steps); err != nil {
			t.Fatalf("input to %s: %v", id, err)
		}
	}
	waitScreen := func(t *testing.T, id, what string, ok func(sessionhost.Capture) bool) sessionhost.Capture {
		t.Helper()
		var last sessionhost.Capture
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			c, err := client.Capture(ctx, sessionhost.CaptureParams{ID: id, Scope: "screen", Trim: true})
			if err != nil {
				t.Fatalf("capture %s: %v", id, err)
			}
			if ok(c) {
				return c
			}
			last = c
		}
		t.Fatalf("%s never showed %s; screen:\n%s", id, what, last.Text)
		return last
	}
	waitExit := func(t *testing.T, id string) sessionhost.Info {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			info, err := client.Info(ctx, id)
			if err != nil {
				t.Fatalf("info %s: %v", id, err)
			}
			if !info.Alive {
				return info
			}
		}
		c, _ := client.Capture(ctx, sessionhost.CaptureParams{ID: id, Scope: "screen", Trim: true})
		t.Fatalf("%s is still running; screen:\n%s", id, c.Text)
		return sessionhost.Info{}
	}

	t.Run("shell", func(t *testing.T) {
		create(t, "smoke-shell", "/bin/sh")
		input(t, "smoke-shell", sessionhost.InputStep{Paste: "echo smoke-$((40 + 2))"}, sessionhost.InputStep{Key: "Enter"})
		waitScreen(t, "smoke-shell", "the shell's output", func(c sessionhost.Capture) bool {
			// The command line reads smoke-$((40 + 2)); only its output reads
			// smoke-42, wherever the prompt put it.
			return strings.Contains(c.Text, "smoke-42")
		})
	})

	t.Run("full-screen program", func(t *testing.T) {
		file := filepath.Join(dir, "tui.txt")
		create(t, "smoke-tui", "vi", file)
		c := waitScreen(t, "smoke-tui", "vi's screen", func(c sessionhost.Capture) bool {
			return c.Alt || strings.Count(c.Text, "~") >= 5
		})
		t.Logf("vi: alternate screen %v, cursor %d,%d", c.Alt, c.CursorX, c.CursorY)
		input(t, "smoke-tui",
			// Typed, not pasted: vi pastes a bracketed paste as text even in
			// normal mode.
			sessionhost.InputStep{Key: "i"},
			sessionhost.InputStep{Bytes: []byte("written in the session host")},
			sessionhost.InputStep{Key: "Escape"},
			sessionhost.InputStep{SleepMS: 200},
			sessionhost.InputStep{Bytes: []byte(":wq")},
			sessionhost.InputStep{Key: "Enter"},
		)
		info := waitExit(t, "smoke-tui")
		if info.ExitCode == nil || *info.ExitCode != 0 {
			t.Fatalf("vi ended with %+v", info)
		}
		got, err := os.ReadFile(file)
		if err != nil || strings.TrimSpace(string(got)) != "written in the session host" {
			t.Fatalf("vi wrote %q (%v)", got, err)
		}
	})

	t.Run("attach", func(t *testing.T) {
		cmd := exec.Command(bin, "attach", "--state-dir", state, "--socket", sock, "smoke-shell")
		cmd.Env = env
		tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 90, Rows: 25})
		if err != nil {
			t.Fatalf("start mewla attach in a terminal: %v", err)
		}
		defer tty.Close()
		var mu sync.Mutex
		var seen bytes.Buffer
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := tty.Read(buf)
				mu.Lock()
				seen.Write(buf[:n])
				mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		waitSeen := func(what string) {
			t.Helper()
			for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				mu.Lock()
				ok := strings.Contains(seen.String(), what)
				mu.Unlock()
				if ok {
					return
				}
			}
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("mewla attach never showed %q; got %q", what, seen.String())
		}
		waitSeen("smoke-42") // the replayed screen
		if _, err := tty.Write([]byte("echo attached-$((1 + 1))\r")); err != nil {
			t.Fatal(err)
		}
		waitSeen("attached-2")
		if _, err := tty.Write([]byte{0x1c}); err != nil { // Ctrl-\ detaches
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("mewla attach after detaching: %v", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatal("mewla attach did not exit after Ctrl-\\")
		}
		info, err := client.Info(ctx, "smoke-shell")
		if err != nil || !info.Alive {
			t.Fatalf("the Session must keep running after a detach: %+v %v", info, err)
		}
		// The host saw the attached terminal's size.
		if info.Cols != 90 || info.Rows != 25 {
			t.Logf("note: Session size after attach is %dx%d", info.Cols, info.Rows)
		}
	})

	t.Run("daemon answers /health", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		daemonLog, err := os.Create(filepath.Join(dir, "daemon.log"))
		if err != nil {
			t.Fatal(err)
		}
		daemon := exec.Command(bin, "-addr", addr, "-state-dir", filepath.Join(dir, "daemon-state"))
		// An agent host executor keeps the throwaway Brain from signing in.
		daemon.Env = append(append([]string{}, env...), "MEWLA_BRAIN_HOST_EXECUTOR=agent")
		daemon.Dir = dir
		daemon.Stdout, daemon.Stderr = daemonLog, daemonLog
		if err := daemon.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- daemon.Wait() }()
		defer func() {
			_ = daemon.Process.Signal(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(15 * time.Second):
				_ = daemon.Process.Kill()
				<-exited
			}
			if t.Failed() {
				log, _ := os.ReadFile(daemonLog.Name())
				t.Logf("daemon log:\n%s", log)
			}
		}()
		var last string
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
			select {
			case err := <-exited:
				exited <- err
				t.Fatalf("the daemon exited: %v", err)
			default:
			}
			resp, err := http.Get("http://" + addr + "/health")
			if err != nil {
				last = err.Error()
				continue
			}
			var body bytes.Buffer
			_, _ = body.ReadFrom(resp.Body)
			_ = resp.Body.Close()
			last = fmt.Sprintf("%s %s", resp.Status, body.String())
			if resp.StatusCode == http.StatusOK {
				t.Logf("/health: %s", strings.TrimSpace(body.String()))
				if want != "" && !strings.Contains(body.String(), want) {
					t.Fatalf("/health does not report version %s: %s", want, body.String())
				}
				return
			}
		}
		t.Fatalf("the daemon never answered /health: %s", last)
	})

	t.Run("host shuts down", func(t *testing.T) {
		if err := client.Shutdown(ctx, true); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		select {
		case err := <-hostDone:
			hostDone <- err
		case <-time.After(15 * time.Second):
			t.Fatal("the session host did not exit after a forced shutdown")
		}
	})
}
