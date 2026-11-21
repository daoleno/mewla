package main

import (
	"strings"
	"testing"
)

// The install line must be one the person can paste into their own shell.
func TestAgyInstallCommandIsThePlatformsOwnOneLiner(t *testing.T) {
	if got := agyInstallCommand("windows"); !strings.Contains(got, "install.ps1") || strings.Contains(got, "bash") {
		t.Fatalf("windows = %q", got)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if got := agyInstallCommand(goos); !strings.Contains(got, "install.sh") {
			t.Fatalf("%s = %q", goos, got)
		}
	}
}
