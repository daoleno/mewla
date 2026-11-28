package watcher

import (
	"strings"
	"testing"
)

// codexScrolledOutput is Brain's own output that pushed Codex's banner out of
// the 80 lines readiness looks at.
var codexScrolledOutput = strings.Repeat("  Brain reply line that pushed the banner away.\n", 90)

// codexOwnerPhotoTail is the screen the owner's photo quoted (Mewla for Mac,
// first install, Codex 0.172 as Brain), row for row: Brain working on its
// activation, the composer, the status row and the hint row. Codex 0.172 no
// longer prints "% context left" in its footer.
const codexOwnerPhotoTail = "• Working (9s • esc to interrupt)\n" +
	"\n" +
	"› Ask Codex to do anything\n" +
	"\n" +
	"  GPT-6.2 Astra · GPT default · ~/.mewla/brain/workspace · Activate Mewla Brain Host\n" +
	"  ← for agents · ? for shortcuts                       ⚠ 1 warning · f2 to view\n"

func TestCodexReadyWhenBannerScrolledAwayOn0172Footer(t *testing.T) {
	for _, test := range []struct {
		name   string
		screen string
	}{
		{name: "owner photo, Brain working", screen: codexOwnerPhotoTail},
		// Captured from codex-cli 0.172.0 at 100x30, banner scrolled away.
		{name: "idle", screen: "  Worked for 40s • 9:32 PM\n\n" +
			"› Ask Codex to do anything\n\n" +
			"  mock-model default · ~/.mewla/brain/workspace · Brain Host activation contract: Read AGENTS.md before conti…\n" +
			"  ← for agents · ? for shortcuts                                          ⚠ 2 warnings · f2 to view\n"},
		{name: "draft typed", screen: "› hello there\n\n" +
			"  mock-model default · ~/work/plain\n" +
			"                                                                           ⚠ 1 warning · f2 to view\n"},
		{name: "working on a prompt", screen: "› hello there\n\n" +
			"• Working (3s • esc to interrupt)\n\n" +
			"› Ask Codex to do anything\n\n" +
			"  mock-model default · ~/work/plain · hello there\n" +
			"  ← for agents · ? for shortcuts                                          ⚠ 2 warnings · f2 to view\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			screen := codexScrolledOutput + test.screen
			if !isCodexStartupReady(screen) {
				t.Fatal("Codex 0.172 at its composer, banner scrolled away, was not ready")
			}
			if !isWorkerInputReady("codex --no-alt-screen", screen) {
				t.Fatal("isWorkerInputReady disagrees with isCodexStartupReady")
			}
		})
	}
}

func TestCodexHeaderlessPickersStayNotReady(t *testing.T) {
	for _, test := range []struct {
		name   string
		screen string
	}{
		// codex-cli 0.172.0, untrusted git folder.
		{name: "folder trust", screen: "  Folder access\n  /work/ws\n\n" +
			"  Trust this folder? Codex can read, edit, and run files here, subject to your permission\n" +
			"  settings. Folder settings can run code automatically, even without a model request. Continue\n" +
			"  only if you trust these files. Your trust decision will be saved.\n\n" +
			"› 1. Trust and continue\n" +
			"  2. Back to Agent Command Center\n\n" +
			"  enter continue · esc back\n"},
		// codex-cli 0.172.0, /model.
		{name: "model picker", screen: "  Select Model and Effort\n\n" +
			"› 1. GPT-6.2-Sol    Fast, go-to model for coding and everyday work.\n" +
			"  2. GPT-6.2-Astra  Our most capable model for complex coding and professional work.\n" +
			"  3. GPT-6.1 Terra  Efficient model that excels at visual understanding and computer use.\n" +
			"↓\n" +
			"  enter select · esc back\n"},
		{name: "one-option picker with a dotted hint", screen: "  Choose\n\n" +
			"› 1. Continue\n\n" +
			"  enter continue · esc back\n"},
		{name: "multi-line draft", screen: "› first line of a draft\n" +
			"  second line of the draft\n" +
			"  mock-model default · ~/work/plain\n"},
		{name: "composer without footer", screen: "› Ask Codex to do anything\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if isCodexStartupReady(codexScrolledOutput + test.screen) {
				t.Fatal("header-less Codex screen that is not its composer was treated as ready")
			}
		})
	}
}
