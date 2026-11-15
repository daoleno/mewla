package main

import (
	"testing"

	"github.com/daoleno/mewla/daemon/addressbook"
)

// A second Windows user's Mewla listens on the next free port (the first
// user's holds 127.0.0.1:9876): the agent it connects is handed its own
// address, not the other person's Mewla.
func TestOutsideAgentConnectsToThisUsersMewla(t *testing.T) {
	stateDir := t.TempDir()
	if got := thisComputerMCPURL(stateDir); got != "http://127.0.0.1:9876/mcp" {
		t.Fatalf("no address found yet: %q", got)
	}
	book, err := addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.ReplaceDiscovered([]string{"http://192.168.1.5:9877", "http://127.0.0.1:9877"}); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Add("https://mewla.example.com", addressbook.SourceManual); err != nil {
		t.Fatal(err)
	}
	if got := thisComputerMCPURL(stateDir); got != "http://127.0.0.1:9877/mcp" {
		t.Fatalf("MCP address = %q, want this user's Mewla on 9877", got)
	}
}
