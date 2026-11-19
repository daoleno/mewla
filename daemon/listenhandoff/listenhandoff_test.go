//go:build unix

package listenhandoff

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// A connection made after the old listener closed, while nothing serves the
// address, waits in the held socket's queue and is answered by the listener
// the next process takes back (exec simulated in-process: the fds survive).
func TestHeldSocketQueuesConnectionsForTheNextProcess(t *testing.T) {
	old := newRegistry("")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if !old.hold(ln) {
		t.Fatal("hold refused a TCP listener")
	}
	_ = ln.Close()

	// The gap: nothing accepts. The connection must not be refused.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("connection in the restart gap was refused: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}

	env := old.forExec([]string{"HOME=/x", EnvVar + "=stale"})
	spec := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, EnvVar+"=") {
			if spec != "" {
				t.Fatalf("EnvVar set twice: %v", env)
			}
			spec = strings.TrimPrefix(entry, EnvVar+"=")
		}
	}
	if !strings.HasPrefix(spec, addr+"=") {
		t.Fatalf("exec env names %q, want %s=<fd>", spec, addr)
	}

	next := newRegistry(spec)
	adopted, err := next.listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer adopted.Close()
	if got := adopted.Addr().String(); got != addr {
		t.Fatalf("adopted %s, want %s", got, addr)
	}
	accepted, err := adopted.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	_ = accepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(accepted).ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("queued connection read %q, %v", line, err)
	}
	if len(next.inherited) != 0 {
		t.Fatalf("taken socket still listed: %v", next.inherited)
	}
}

// An inherited socket nobody takes back is closed by Release, so connections
// to it are refused again instead of waiting forever.
func TestReleaseClosesUnclaimedSockets(t *testing.T) {
	old := newRegistry("")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	old.hold(ln)
	_ = ln.Close()
	env := old.forExec(nil)
	next := newRegistry(strings.TrimPrefix(env[0], EnvVar+"="))
	// The old registry's copy is the same fd (in-process exec stand-in).
	old.held = nil
	next.release()
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Fatal("released socket still accepts connections")
	}
}

// A child receives the held sockets as fd 3 onward.
func TestForChildNumbersExtraFiles(t *testing.T) {
	r := newRegistry("")
	for i := 0; i < 2; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		r.hold(ln)
		_ = ln.Close()
	}
	env, files := r.forChild([]string{"A=1"})
	if len(files) != 2 {
		t.Fatalf("files=%d", len(files))
	}
	spec := strings.TrimPrefix(env[len(env)-1], EnvVar+"=")
	pairs := strings.Split(spec, ",")
	if len(pairs) != 2 || !strings.HasSuffix(pairs[0], "=3") || !strings.HasSuffix(pairs[1], "=4") {
		t.Fatalf("child spec %q", spec)
	}
	for _, f := range files {
		f.Close()
	}
}
