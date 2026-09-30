package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestHistoryBoundsAndOwnership(t *testing.T) {
	m := managerWithScrollFixture(newSerializedScrollFixture())
	if _, err := m.History("other-owner", "session-a"); err == nil {
		t.Fatal("accepted another owner")
	}
	if _, err := m.History("owner-a", "stale"); err == nil {
		t.Fatal("accepted stale session")
	}
	b := &boundedHistoryBuffer{}
	if _, err := b.Write(make([]byte, historyByteLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("unbounded capture")
	}
	if _, err := parseHistory("%1\t80\t10\t0\n" + strings.Repeat("line\n", 6001)); err == nil {
		t.Fatal("unbounded rows")
	}
}

func TestTmuxHistoryPreservesStylesWithoutEnteringCopyMode(t *testing.T) {
	requireTmux(t)
	isolateTmuxServer(t)
	runTmuxTestCommand(t, "-f", "/dev/null", "new-session", "-d", "-s", "bootstrap", "-x", "80", "-y", "24", "sh")
	runTmuxTestCommand(t, "set-option", "-g", "history-limit", "10000")
	runTmuxTestCommand(t, "new-session", "-d", "-s", "history", "-x", "80", "-y", "24", "sh")
	runTmuxTestCommand(t, "send-keys", "-t", "history", "i=0; while [ $i -lt 7000 ]; do printf '\\033[1;3;4;38;2;10;20;30m%05d 中文 ┌─┐\\033[0m\\n' $i; i=$((i+1)); done", "Enter")
	session := &tmuxSession{targetID: "history"}
	var h History
	var err error
	for i := 0; i < 100; i++ {
		h, err = session.History()
		if err == nil && h.Total >= 6900 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if h.Total < 6900 {
		t.Fatalf("history did not populate: %d", h.Total)
	}
	if strings.Count(h.ANSI, "\n") != 6000 {
		t.Fatalf("rows=%d", strings.Count(h.ANSI, "\n"))
	}
	for _, part := range []string{"\x1b[", "38;2;10;20;30", "中文", "┌─┐"} {
		if !strings.Contains(h.ANSI, part) {
			t.Fatalf("lost %q", part)
		}
	}
	if mode := tmuxTestOutput(t, "display-message", "-p", "-t", "history", "#{pane_in_mode}"); mode != "0" {
		t.Fatalf("entered copy mode: %s", mode)
	}
}
