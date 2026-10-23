package watcher

import (
	"strings"
	"testing"
)

func TestRealTmuxBatchOwnershipIgnoresInheritedMarker(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	owned := createHarnessPane(t, h.selected, "owned", "exec /bin/sh")
	ambient := createHarnessPane(t, h.selected, "ambient", "exec /bin/sh")
	// A global option of the same name must not make an ambient pane owned.
	for _, args := range [][]string{
		{"set-option", "-g", ownershipOption, "1"},
		{"set-option", "-p", "-t", owned, ownershipOption, "1"},
	} {
		if out, err := tmuxHarnessCommand(h.selected, args...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	got, err := ownedPaneSet(h.selected, []string{owned, ambient})
	if err != nil {
		t.Fatal(err)
	}
	if !got[owned] || got[ambient] {
		t.Fatalf("owned set = %v, want only %s", got, owned)
	}
	if _, err := ownedPaneSet(h.selected, []string{owned, "%9999"}); err == nil {
		t.Fatal("a vanished pane must fail the batch so the caller probes one by one")
	}
}

func TestRealTmuxCaptureCarriesLiveness(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	pane := createHarnessPane(t, h.selected, "cap", "printf 'HELLO-SCREEN\\n'; exec sleep 60")
	waitForHarness(t, "screen text", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, pane), "HELLO-SCREEN")
	})
	screen, alive, status, err := capturePaneWithState(h.selected, pane)
	if err != nil || !alive || status != -1 || !strings.Contains(screen, "HELLO-SCREEN") || strings.Contains(screen, paneStateMarker) {
		t.Fatalf("screen=%q alive=%v status=%d err=%v", screen, alive, status, err)
	}
	if _, _, _, err := capturePaneWithState(h.selected, "%9999"); err == nil {
		t.Fatal("a missing pane must fail")
	}
}

func TestRealTmuxBatchOwnershipAnswersEveryPane(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	var panes []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		pane := createHarnessPane(t, h.selected, name, "exec /bin/sh")
		if out, err := tmuxHarnessCommand(h.selected, "set-option", "-p", "-t", pane, ownershipOption, "1").CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		panes = append(panes, pane)
	}
	got, err := ownedPaneSet(h.selected, panes)
	if err != nil || len(got) != len(panes) {
		t.Fatalf("owned = %v err=%v, want all %d panes (pane ids with digits tmux's strftime would rewrite included)", got, err, len(panes))
	}
}
