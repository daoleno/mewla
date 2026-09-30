//go:build linux

package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestClaudeProcessSessionRejectsPIDReuse(t *testing.T) {
	root := t.TempDir()
	pid := os.Getpid()
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))[19]
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, start string
		wantLive    bool
	}{{"live", start, true}, {"recycled pid", start + "0", false}} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(claudeProcessSession{PID: pid, SessionID: "session", Cwd: root, ProcStart: tc.start})
			if err := os.WriteFile(filepath.Join(root, "sessions", strconv.Itoa(pid)+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			_, live, err := claudeProcessRecord(root, pid)
			if err != nil || live != tc.wantLive {
				t.Fatalf("live=%v err=%v", live, err)
			}
			owner, err := LiveClaudeSessionOwner("session", root)
			if err != nil || (owner == pid) != tc.wantLive {
				t.Fatalf("owner=%d err=%v", owner, err)
			}
		})
	}
}
