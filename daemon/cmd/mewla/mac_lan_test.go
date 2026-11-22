package main

import "testing"

func TestMacLANChoiceBelongsToRunningApp(t *testing.T) {
	dir := t.TempDir()
	var unmanaged *macLANControl
	if unmanaged.set("on").OK {
		t.Fatal("externally managed daemon accepted LAN change")
	}
	restarts := 0
	control := &macLANControl{stateDir: dir, restart: func() { restarts++ }}
	if response := control.set("off"); !response.OK || response.After != nil {
		t.Fatal("default off must be idempotent")
	}
	if _, saved := loadStartupChoice(dir); saved {
		t.Fatal("unchanged default wrote startup choice")
	}
	response := control.set("on")
	if !response.OK || response.After == nil || restarts != 0 {
		t.Fatal("expected to restart after replying")
	}
	response.After()
	if choice, _ := loadStartupChoice(dir); choice != choiceLAN || restarts != 1 || control.set("off").OK {
		t.Fatal("lost LAN choice or accepted a concurrent change during restart")
	}
	next := &macLANControl{stateDir: dir, enabled: true, restart: control.restart}
	if response = next.set("off"); !response.OK || response.After == nil {
		t.Fatal("could not disable LAN")
	}
	if choice, _ := loadStartupChoice(dir); choice == choiceLAN {
		t.Fatal("LAN would reopen on restart")
	}
}
