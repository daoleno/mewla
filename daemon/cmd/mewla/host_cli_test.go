package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

func TestHostCommandNeedsTheTerminalEngine(t *testing.T) {
	if ghosttyvt.Available() {
		t.Skip("this build has the terminal engine")
	}
	var stderr bytes.Buffer
	err := run([]string{"host", "--socket", t.TempDir() + "/h.sock"}, &stderr)
	if !errors.Is(err, errNoTerminalEngine) {
		t.Fatalf("mewla host without the engine: %v", err)
	}
}

func TestHostStatusTable(t *testing.T) {
	code := 3
	var out bytes.Buffer
	host := sessionhost.Welcome{Protocol: 1, HostVersion: "0.2.99 (abc1234)", Engine: true, EngineVersion: "1.0.0", PID: 42, StartedAt: time.Unix(0, 0)}
	sessions := []sessionhost.Info{
		{ID: "w1", PID: 100, Alive: true, Cols: 80, Rows: 24, Cwd: "/src", Argv: []string{"claude", "--resume"}},
		{ID: "w2", PID: 101, ExitCode: &code, Cols: 120, Rows: 40, Argv: []string{"codex"}},
	}
	if err := writeHostStatus(&out, "/run/user/1000/mewla/host-1.sock", host, sessions); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"Session host 0.2.99 (abc1234), protocol 1, pid 42",
		"terminal engine libghostty-vt 1.0.0",
		"w1       100  running   80x24   /src  claude --resume",
		"w2       101  exited 3  120x40",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("status lacks %q:\n%s", want, text)
		}
	}
	out.Reset()
	if err := writeHostStatus(&out, "/s", sessionhost.Welcome{}, nil); err != nil || !strings.Contains(out.String(), "terminal engine none") || !strings.Contains(out.String(), "No Sessions.") {
		t.Fatalf("empty status: %v\n%s", err, out.String())
	}
}
