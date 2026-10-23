package watcher

import (
	"testing"
	"time"
)

func screensAt(now *time.Time) *paneScreens {
	p := newPaneScreens()
	p.now = func() time.Time { return *now }
	return p
}

func TestAnIdlePaneIsNotReadAgainUntilItPrints(t *testing.T) {
	now := time.Unix(1000, 0)
	p := screensAt(&now)
	pane := tmuxPane{target: "%1", activity: 990}
	p.inventory([]tmuxPane{pane})
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a pane never read was reused")
	}
	startedAt, startedUnix := p.readStarted()
	p.remember("%1", "screen", true, -1, startedAt, startedUnix)

	now = now.Add(500 * time.Millisecond)
	p.inventory([]tmuxPane{pane})
	if content, alive, _, ok := p.reuse("%1"); !ok || content != "screen" || !alive {
		t.Fatalf("an idle pane was read again: %q %v %v", content, alive, ok)
	}

	pane.activity = 1000
	p.inventory([]tmuxPane{pane})
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a pane that printed since it was read was reused")
	}
}

func TestOutputInTheSecondOfTheReadIsNotTakenAsSeen(t *testing.T) {
	now := time.Unix(1000, 0).Add(200 * time.Millisecond)
	p := screensAt(&now)
	pane := tmuxPane{target: "%1", activity: 1000}
	p.inventory([]tmuxPane{pane})
	startedAt, startedUnix := p.readStarted()
	p.remember("%1", "screen", true, -1, startedAt, startedUnix)
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a pane read in the same second it printed was trusted: more output could follow in that second")
	}
	now = now.Add(time.Second)
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("the screen read before the second ended was trusted")
	}
}

func TestAReusedScreenExpiresAndAPaneThatDiedIsReadAgain(t *testing.T) {
	now := time.Unix(1000, 0)
	p := screensAt(&now)
	pane := tmuxPane{target: "%1", activity: 900}
	p.inventory([]tmuxPane{pane})
	startedAt, startedUnix := p.readStarted()
	p.remember("%1", "screen", true, -1, startedAt, startedUnix)

	now = now.Add(paneCaptureRefresh)
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a screen older than the refresh bound was reused")
	}
	now = startedAt.Add(time.Second)
	if _, _, _, ok := p.reuse("%1"); !ok {
		t.Fatal("setup: the screen should be reusable again")
	}
	pane.dead = true
	p.inventory([]tmuxPane{pane})
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a pane that died was not read again")
	}
}

func TestAFailedReadIsNeverReused(t *testing.T) {
	now := time.Unix(1000, 0)
	p := screensAt(&now)
	p.inventory([]tmuxPane{{target: "%1", activity: 900}})
	startedAt, startedUnix := p.readStarted()
	p.remember("%1", "", false, -1, startedAt, startedUnix)
	if _, _, _, ok := p.reuse("%1"); ok {
		t.Fatal("a failed read was remembered as a screen")
	}
}

func TestOwnershipIsBelievedForOwnedPanesAndRecheckedForTheRest(t *testing.T) {
	now := time.Unix(1000, 0)
	p := screensAt(&now)
	panes := []tmuxPane{{target: "%1"}, {target: "%2"}}
	p.inventory(panes)
	owned, ask := p.ownership(panes)
	if len(owned) != 0 || len(ask) != 2 {
		t.Fatalf("first look: owned=%v ask=%v", owned, ask)
	}
	p.learnOwnership(ask, map[string]bool{"%1": true})

	now = now.Add(time.Second)
	owned, ask = p.ownership(panes)
	if !owned["%1"] || len(ask) != 0 {
		t.Fatalf("within the recheck window: owned=%v ask=%v", owned, ask)
	}
	now = now.Add(notOwnedRecheck)
	owned, ask = p.ownership(panes)
	if !owned["%1"] || len(ask) != 1 || ask[0].target != "%2" {
		t.Fatalf("after the recheck window: owned=%v ask=%v", owned, ask)
	}
	p.inventory([]tmuxPane{{target: "%2"}})
	if owned, _ := p.ownership([]tmuxPane{{target: "%1"}}); owned["%1"] {
		t.Fatal("a pane that is gone stayed owned")
	}
}

func TestProcessTableIsReadOnlyWhenTheInventoryMovesOrAges(t *testing.T) {
	p := newPaneScreens()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	reads := 0
	read := func() map[int]processInfo {
		reads++
		return map[int]processInfo{1: {pid: 1}}
	}
	panes := []tmuxPane{{target: "%1", panePID: 10, activity: 5}}

	p.inventory(panes)
	p.processes(read)
	p.inventory(panes)
	p.processes(read)
	if reads != 1 {
		t.Fatalf("a quiet inventory read the table %d times, want 1", reads)
	}
	panes[0].activity = 6
	p.inventory(panes)
	p.processes(read)
	if reads != 2 {
		t.Fatalf("pane output did not refresh the table (reads=%d)", reads)
	}
	now = now.Add(processTableRefresh)
	p.processes(read)
	if reads != 3 {
		t.Fatalf("an old table was kept (reads=%d)", reads)
	}
	now = now.Add(processTableRefresh)
	if p.processes(func() map[int]processInfo { return nil }) != nil {
		t.Fatal("a failed read must surface as unknown")
	}
	p.processes(read)
	if reads != 4 {
		t.Fatalf("a failed read was cached (reads=%d)", reads)
	}
}

func TestPollSlowsOnlyAfterTheInventoryStaysQuiet(t *testing.T) {
	p := newPaneScreens()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	p.changedAt = now
	base := 500 * time.Millisecond
	panes := []tmuxPane{{target: "%1", panePID: 10, activity: 5}}
	p.inventory(panes)
	if got := p.nextPollDelay(base); got != base {
		t.Fatalf("a fresh change polled at %v, want %v", got, base)
	}
	now = now.Add(idlePollAfter)
	p.inventory(panes)
	if got := p.nextPollDelay(base); got != idlePollInterval {
		t.Fatalf("a quiet inventory polled at %v, want %v", got, idlePollInterval)
	}
	panes[0].activity = 9
	p.inventory(panes)
	if got := p.nextPollDelay(base); got != base {
		t.Fatalf("output did not bring the fast cadence back (%v)", got)
	}
	if got := p.nextPollDelay(10 * time.Second); got != 10*time.Second {
		t.Fatalf("a slower configured interval was shortened to %v", got)
	}
}
