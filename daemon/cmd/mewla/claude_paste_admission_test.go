package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/daoleno/mewla/daemon/classifier"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudePasteActivityProbeCarriesBothExactDigests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	payload := "<mewla_work_event>\n{\"summary\":\"你好\"}\n</mewla_work_event>"
	raw := "\n\n<pasted_content id=\"4f28\">\n" + payload + "\n</pasted_content id=\"4f28\">\n"
	row, _ := json.Marshal(map[string]any{"type": "user", "sessionId": "session", "uuid": "input", "cwd": "/fixture", "timestamp": at.Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": raw}})
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	probe := newWorkProviderActivityProbe()
	obs := probe.ObserveProviderActivity(classifier.Worker{ID: "fixture", Cwd: "/fixture", Command: "claude --resume session", StartedAt: at.Add(-time.Second), PaneAlive: true}, at)
	digest := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	if obs.InputSHA256 != digest(raw) || obs.InputUnwrappedSHA256 != digest(payload) || obs.AdmissionID == "" || obs.AdmissionCursor == 0 {
		t.Fatalf("probe lost exact wrapper evidence: %+v", obs)
	}
}
