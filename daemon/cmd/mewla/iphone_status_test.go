package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentuse"
)

// The status names the one fix when iPhone use is on but something is missing.
func TestAgentUseStatusHasTheIPhoneLine(t *testing.T) {
	var out bytes.Buffer
	printAgentUse(&out, agentuse.Status{Enabled: true, IPhone: &agentuse.IPhoneStatus{Enabled: false, State: agentuse.IPhoneOff}})
	if strings.Contains(out.String(), "iPhone") {
		t.Fatalf("an iPhone line while it is off:\n%s", out.String())
	}
	out.Reset()
	printAgentUse(&out, agentuse.Status{Enabled: true, IPhone: &agentuse.IPhoneStatus{Enabled: true, State: agentuse.IPhoneReady, Summary: "ready: keep your iPhone locked.",
		Active: &agentuse.ActiveAgent{Worker: "%7", Name: "Shopper"}}})
	if got := out.String(); !strings.Contains(got, "Your iPhone: iPhone use is on; ready") || !strings.Contains(got, "mewla_iphone") || !strings.Contains(got, "Shopper is using it now") {
		t.Fatalf("ready:\n%s", got)
	}
	out.Reset()
	printAgentUse(&out, agentuse.Status{Enabled: true, IPhone: &agentuse.IPhoneStatus{Enabled: true, State: agentuse.IPhoneBlocked, Summary: "it needs Screen Recording.", Fix: agentuse.FixOpenSettings}})
	if got := out.String(); strings.Contains(got, "mewla_iphone") || !strings.Contains(got, "Privacy & Security") {
		t.Fatalf("blocked:\n%s", got)
	}
}
