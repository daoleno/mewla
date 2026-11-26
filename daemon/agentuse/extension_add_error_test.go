package agentuse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The owner's Mac, 11-26: Add extension failed and the app went on saying
// "Getting ready…" for a minute, the reason only in the daemon's log. The
// status says why at once, until the next try or until it is there.
func TestAFailedAddSaysWhyUntilTheNextTry(t *testing.T) {
	c := newFakeComputer(t)
	c.withNode()
	s := c.service(false)
	results := make(chan error, 2)
	s.SetExtensionAdder(func(context.Context, string, func() bool) error { return <-results }, func() bool { return true })

	settle := func() BrowserStatus {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			status := s.Refresh().Browser
			if status.State != BrowserPreparing {
				return status
			}
			if time.Now().After(deadline) {
				t.Fatal("still adding")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if _, err := s.Fix(FixInstallExtension); err != nil {
		t.Fatal(err)
	}
	reason := "step 4 (a new window): Another agent is working in that app"
	results <- errors.New(reason)
	failed := settle()
	if failed.State != BrowserNoExtension || failed.Fix != FixInstallExtension || failed.Error != reason || !strings.Contains(failed.Summary, reason) {
		t.Fatalf("after the failure: %+v", failed)
	}

	// Trying again clears it while it adds, and a success leaves no reason.
	status, err := s.Fix(FixInstallExtension)
	if err != nil {
		t.Fatal(err)
	}
	if status.Browser.State != BrowserPreparing || status.Browser.Error != "" {
		t.Fatalf("trying again: %+v", status.Browser)
	}
	results <- nil
	if after := settle(); after.Error != "" || strings.Contains(after.Summary, reason) {
		t.Fatalf("after a success: %+v", after)
	}
}
