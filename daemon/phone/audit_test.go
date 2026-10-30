package phone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheAuditLogHoldsWhatWasDoneNeverWhatWasTypedOrRead(t *testing.T) {
	dir := t.TempDir()
	audit := NewAudit(dir)
	p := &scriptedPhone{screen: appScreen(
		node("EditText", "to", "alice@example.com", "", field, [4]int{0, 100, 1000, 200}),
		node("Button", "send", "Send the secret plan", "", tap1, [4]int{0, 300, 1000, 400}),
	)}
	m := NewManager(Options{})
	_, conn := connect(t, m, StateReady, p.handle)
	a := NewAgent(m, audit)

	ctx := context.Background()
	const secret = "the launch code is 0451"
	for _, action := range []AgentAction{
		{Action: ActScreen},
		{Action: ActType, Element: 1, Text: secret},
		{Action: ActTap, Element: 2},
		{Action: ActSwipe, Direction: "up"},
		{Action: ActKey, Key: "enter"},
		{Action: ActLaunch, App: "Mail"},
		{Action: ActHandoff, Reason: "Enter the code 0451 sent to bob@example.com"},
	} {
		_, _ = a.Act(ctx, "%7", "Writer", action)
	}
	_, _ = a.Act(ctx, "%7", "Writer", AgentAction{Action: ActTap, Element: 99})
	_ = conn

	path := filepath.Join(dir, AuditFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, leaked := range []string{secret, "0451", "alice@example.com", "bob@example.com", "Enter the code"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("the audit log holds %q:\n%s", leaked, text)
		}
	}
	sum := sha256.Sum256([]byte(secret))
	for _, want := range []string{
		`"worker":"%7"`, `"action":"type"`, `"text_length":23`, `"text_sha256":"` + hex.EncodeToString(sum[:8]) + `"`,
		`"action":"tap"`, `"element":2`, `"label":"Send the secret plan"`, `"x":500`, `"y":350`,
		`"action":"swipe"`, `"direction":"up"`, `"x2":`, `"action":"key"`, `"key":"enter"`, `"action":"handoff"`,
		`"action":"launch"`, `"error":"there is no element [99]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the audit log lacks %s:\n%s", want, text)
		}
	}
	// The field the agent typed into is named by what it asks for, not by what it holds.
	if !strings.Contains(text, `"element":1`) || strings.Contains(text, `"element":1,"label"`) {
		t.Fatalf("a field is labeled by its content:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
}

func TestOldAuditLinesAreDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, AuditFile)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	old := `{"time":"` + now.Add(-31*24*time.Hour).Format(time.RFC3339) + `","worker":"old","action":"tap"}`
	recent := `{"time":"` + now.Add(-29*24*time.Hour).Format(time.RFC3339) + `","worker":"recent","action":"tap"}`
	if err := os.WriteFile(path, []byte(old+"\n"+recent+"\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	audit := NewAudit(dir)
	audit.now = func() time.Time { return now }
	audit.Record("new", AgentAction{Action: ActKey, Key: "back"}, trace{key: "back"}, errors.New("nope"))
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if strings.Contains(text, `"old"`) || strings.Contains(text, "not json") || !strings.Contains(text, `"recent"`) || !strings.Contains(text, `"new"`) {
		t.Fatalf("audit after pruning:\n%s", text)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	var nilAudit *Audit
	nilAudit.Record("x", AgentAction{}, trace{}, nil) // an Agent without a log still works
}
