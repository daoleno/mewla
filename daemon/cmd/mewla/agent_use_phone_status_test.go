package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/phone"
)

func phoneLine(t *testing.T, st phone.Status) string {
	t.Helper()
	var out bytes.Buffer
	printPhone(&out, agentuse.Status{Phone: &agentuse.PhoneStatus{Enabled: true, Status: st}})
	return out.String()
}

func TestAgentUseStatusSaysWhyThePhoneIsInSetupAndSinceWhen(t *testing.T) {
	since := time.Now().Add(-4*time.Minute - 30*time.Second)
	line := phoneLine(t, phone.Status{Connected: true, State: phone.StateSetup, Model: "PYR110", SetupSince: &since,
		Problem: "Wireless debugging is off and Mewla has not been given the permission to switch it on."})
	for _, want := range []string{"PYR110", "not ready since ", "(4m)", "Wireless debugging is off and Mewla has not been given the permission to switch it on"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
	if strings.Contains(line, "..") {
		t.Fatalf("doubled full stop: %q", line)
	}
}

func TestAgentUseStatusSaysWhenThePhoneGaveNoReason(t *testing.T) {
	line := phoneLine(t, phone.Status{Connected: true, State: phone.StateSetup, Model: "PYR110"})
	if !strings.Contains(line, "has not said why") {
		t.Fatalf("%q", line)
	}
}
