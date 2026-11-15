package classifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bottom of real Claude Code 2.1.325 screens (testdata, captured from
// Mewla Sessions on 2026-11-16; the conversation lines above are replaced): a
// turn that runs, however long and whatever the footer also counts, is
// working; a finished turn is not, even with a shell left running.
func TestAgentWorkingOnScreenFromRealCaptures(t *testing.T) {
	for _, tc := range []struct {
		file    string
		working bool
	}{
		{"screen-working-quantumizing-2h-shells.txt", true},
		{"screen-working-fermenting.txt", true},
		{"screen-done-background-shell.txt", false},
		{"screen-done-idle.txt", false},
		{"screen-done-idle-narrow-54.txt", false},
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.325", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := AgentWorkingOnScreen(string(raw)); got != tc.working {
			t.Errorf("%s: working = %v, want %v", tc.file, got, tc.working)
		}
	}
}

// The lines the incident reports quote (ZeroGPrdUI at 23:08 on 2026-11-15,
// ZeroGHarnessPhase1), under a footer that counts background work and has no
// interrupt hint: the spinner alone proves the turn runs. A narrow screen that
// wraps the footer keeps the spinner, and the same words in the conversation
// above the bottom do not count.
func TestAgentWorkingOnScreenFromTheReportedLines(t *testing.T) {
	const prompt = "────────────────────────────────────────\n❯ \n────────────────────────────────────────\n"
	calculating := "● Reading the router.\n\n✻ Calculating… (1h 34m 36s · thinking with high effort)\n\n" + prompt +
		"  ⏵⏵ bypass permissions on · 1 shell, 1 monitor · ↓ to manage\n"
	quantumizing := "● Read 3 files\n\n· Quantumizing… (59m 12s · ↓ 40.1k tokens)\n  ⎿  Tip: Use /clear to start fresh when switching topics\n\n" + prompt +
		"  ⏵⏵ bypass permissions on · 1 shell, 1 monitor · ↓ to manage\n"
	narrow := "· Quantumizing… (59m 12s · ↓ 40.1k tokens)\n" +
		"──────────────────────\n❯ \n──────────────────────\n  ⏵⏵ bypass permissions on\n  · 2 shells · esc to\n  interrupt · ↓ to manage\n"
	above := "✻ Calculating… (1h 34m 36s · thinking with high effort)\n" + strings.Repeat("● an older line\n", 12) +
		"✻ Worked for 1h 44m 54s · done 12:23\n" + prompt + "  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	for name, tc := range map[string]struct {
		pane    string
		working bool
	}{
		"calculating":  {calculating, true},
		"quantumizing": {quantumizing, true},
		"narrow":       {narrow, true},
		"only above":   {above, false},
	} {
		if got := AgentWorkingOnScreen(tc.pane); got != tc.working {
			t.Errorf("%s: working = %v, want %v", name, got, tc.working)
		}
	}
}

// A report that ends the turn is taken whatever phase word comes with it
// ("--status done --phase done" was refused, and the turn read as lost); a
// running report still has to name a real phase.
func TestATurnEndingReportIsTakenWithAnUnknownPhase(t *testing.T) {
	for _, status := range []string{"done", "failed", "blocked"} {
		got, err := ValidateProgress(WorkerProgress{Status: status, Phase: "done", Attention: "done"})
		if err != nil || got.Phase != "reporting" {
			t.Fatalf("%s with phase done: %+v, %v", status, got, err)
		}
	}
	if _, err := ValidateProgress(WorkerProgress{Status: "running", Phase: "done", Attention: "none"}); err == nil {
		t.Fatal("a running report with an unknown phase was taken")
	}
}
