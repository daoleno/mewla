package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAHostFromAReplacedAppIsFoundAndNothingElseIs(t *testing.T) {
	const helper = "/Applications/Mewla.app/Contents/Helpers/mewla"
	const installed = "/Applications/Mewla.app/Contents/MacOS/Mewla"
	there := func(path string) bool { return path == installed }
	cases := []struct {
		name, host, helper string
		exists             func(string) bool
		replaced           bool
	}{
		// The owner's Mac, 11-26: the update moved the old app aside and deleted it; the host ran on from there.
		{"moved aside by the updater", "/Applications/.nsh-updater-old-89387-Mewla.app/Contents/MacOS/Mewla", helper, there, true},
		{"put in the Trash by a reinstall", "/Users/someone/.Trash/Mewla.app/Contents/MacOS/Mewla", helper, there, true},
		{"the installed app's host", installed, helper, there, false},
		// APFS is case-insensitive: Contents/MacOS/mewla is the same file.
		{"the same path in another case", "/Applications/Mewla.app/Contents/MacOS/mewla", helper, there, false},
		// Mid-swap the installed app is not there yet: launchd could not start it, so nothing is done.
		{"the installed app missing", "/Applications/.nsh-updater-old-1-Mewla.app/Contents/MacOS/Mewla", helper, func(string) bool { return false }, false},
		{"a daemon outside an app", "/Applications/.old.app/Contents/MacOS/Mewla", "/usr/local/bin/mewla", there, false},
		{"a host that is no app", "/bin/zsh", helper, there, false},
		{"an unknown host", "", helper, there, false},
	}
	for _, c := range cases {
		replaced, _ := replacedAppHost(c.host, c.helper, c.exists)
		if replaced != c.replaced {
			t.Errorf("%s: replaced = %v, want %v", c.name, replaced, c.replaced)
		}
	}
	if _, want := replacedAppHost("/Applications/.x-Mewla.app/Contents/MacOS/Mewla", helper, there); want != installed {
		t.Fatalf("want = %q", want)
	}
}

func TestTheRestartOntoTheInstalledAppIsAskedOnceAndNeverInALoop(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "app-host-restart")
	now := time.Now()
	if !mayRestartAppHost(marker, 37515, now) {
		t.Fatal("no restart was asked yet")
	}
	if err := os.WriteFile(marker, []byte("37515\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if mayRestartAppHost(marker, 37515, now) || mayRestartAppHost(marker, 77229, now) {
		t.Fatal("a second restart within the gap")
	}
	later := now.Add(appHostRestartGap + time.Second)
	if mayRestartAppHost(marker, 37515, later) {
		t.Fatal("the same host twice")
	}
	if !mayRestartAppHost(marker, 77229, later) {
		t.Fatal("a later update's host is restarted again")
	}
}
