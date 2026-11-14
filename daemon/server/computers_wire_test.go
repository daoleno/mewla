package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A Work counts as an open request while it asks a question and is neither
// closed nor snoozed.
func TestOpenWorkQuestionsSkipSnoozedAndClosed(t *testing.T) {
	now := time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)
	later, earlier := now.Add(time.Hour), now.Add(-time.Minute)
	current := []brain.CurrentWork{
		{ID: "asks", Status: brain.WorkWaiting, Question: "Keep both?"},
		{ID: "snoozed", Status: brain.WorkWaiting, Question: "Later?", SnoozedUntil: &later},
		{ID: "snooze-over", Status: brain.WorkWaiting, Question: "Now?", SnoozedUntil: &earlier},
		{ID: "closed", Status: brain.WorkDone, Question: "Stale?"},
		{ID: "running", Status: brain.WorkRunning},
	}
	if got := openWorkQuestions(current, now); got != 2 {
		t.Fatalf("open Work questions = %d, want 2", got)
	}
}

// The computers message carries this computer's connection from
// remote-status.json: its phase and the newest failure with when it began.
func TestComputersMessageCarriesTheConnection(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	if _, has := srv.computersMessage("r")["connection"]; has {
		t.Fatal("a connection without remote access")
	}
	status := `{"phase":"offline","url":"https://x.example","last_error":"relay unreachable","updated_at":"2026-11-12T13:44:00Z","last_failure":"relay unreachable","last_failure_at":"2026-11-12T13:40:00Z","connected_at":"2026-11-12T09:00:00Z"}`
	if err := os.WriteFile(filepath.Join(authManager.StorageDir(), "remote-status.json"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(srv.computersMessage("r"))
	var message struct {
		Type         string `json:"type"`
		RequestID    string `json:"request_id"`
		Computers    []any  `json:"computers"`
		OpenRequests *int   `json:"open_requests"`
		Connection   struct {
			Phase         string     `json:"phase"`
			Cause         string     `json:"cause"`
			LastFailure   string     `json:"last_failure"`
			LastFailureAt *time.Time `json:"last_failure_at"`
			ConnectedAt   *time.Time `json:"connected_at"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	c := message.Connection
	if message.Type != "computers" || message.RequestID != "r" || message.Computers == nil || message.OpenRequests == nil ||
		c.Phase != "offline" || c.Cause != "relay unreachable" || c.LastFailure != "relay unreachable" ||
		c.LastFailureAt == nil || c.LastFailureAt.Minute() != 40 || c.ConnectedAt == nil {
		t.Fatalf("message = %s", raw)
	}
	t.Logf("computers = %s", raw)
}
