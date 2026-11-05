package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCalendarCreateHelpAndValidationRequireSourceThread(t *testing.T) {
	var help bytes.Buffer
	err := runCalendarWrite(false, []string{"-help"}, &help)
	if err == nil {
		t.Fatal("expected help sentinel error")
	}
	for _, want := range []string{
		"scheduled_action (requires -source-thread)",
		"required for scheduled_action: source_thread_id",
		"canonical Calendar result projection",
	} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, help.String())
		}
	}

	err = runCalendarWrite(false, []string{
		"-title", "Report",
		"-kind", "scheduled_action",
		"-date", "2026-07-15",
		"-time", "09:30",
		"-timezone", "UTC",
		"-instruction", "Write the report",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "scheduled_action requires -source-thread (source_thread_id)") {
		t.Fatalf("missing source-thread error = %v", err)
	}
}

func TestCalendarCreateScheduledActionRequiresExecutor(t *testing.T) {
	args := []string{
		"-title", "Report", "-kind", "scheduled_action", "-date", "2026-07-15", "-time", "09:30",
		"-timezone", "UTC", "-instruction", "Write the report", "-source-thread", "thread-1",
		// A state dir that holds no daemon: the check must fail before any call.
		"-state-dir", t.TempDir(),
	}
	err := runCalendarWrite(false, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "scheduled_action requires -executor") {
		t.Fatalf("missing executor error = %v", err)
	}
	var help bytes.Buffer
	_ = runCalendarWrite(false, []string{"-help"}, &help)
	for _, want := range []string{"-executor", "-model", "-reasoning"} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, help.String())
		}
	}
}
