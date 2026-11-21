package watcher

import "testing"

const agyRule = "────────────────────────────────────────────────────────────────────────────────────────────────────────────────"

const agyHeader = "      ▄▀▀▄        Antigravity CLI 1.5.14\n     ▀▀▀▀▀▀       Gemini API key\n    ▀▀▀▀▀▀▀▀      Gemini 3.1 Pro (Low)\n   ▄▀▀    ▀▀▄     /home/u/work\n  ▄▀▀      ▀▀▄\n\n"

// The screens are agy 1.5.14 in a tmux pane (captured 2026-11-22).
func TestAntigravityInputReady(t *testing.T) {
	ready := agyHeader + agyRule + "\n>\n" + agyRule + "\n? for shortcuts                                              Gemini 3.1 Pro · low\n"
	if !isWorkerInputReady("agy", ready) {
		t.Fatalf("an empty composer under the header is ready")
	}
	// Auto mode (the signed-in default) puts a hint in the empty composer.
	auto := agyHeader + agyRule + "\n> Auto mode: edits & safe commands auto-approved (shift+tab to cycle)\n" + agyRule + "\n? for shortcuts                       auto · Gemini 4 Neon · low\n"
	if !isWorkerInputReady("agy", auto) {
		t.Fatalf("the Auto-mode hint in an empty composer is not a draft")
	}
	typed := agyHeader + agyRule + "\n> say hi\n" + agyRule + "\n? for shortcuts                                              Gemini 3.1 Pro · low\n"
	if isWorkerInputReady("agy", typed) {
		t.Fatalf("a composer with a draft is not ready for a brief")
	}
	for name, screen := range map[string]string{
		"nothing yet":                 "",
		"trust":                       "Accessing workspace:\n/home/u/work\nDo you trust the contents of this project?\n> Yes, I trust this folder\n  No, exit\n",
		"sign-in":                     "Welcome to the Antigravity CLI. You are currently not signed in.\nSelect login method:\n> 1. Google OAuth\n",
		"first-run theme":             "Welcome to Antigravity CLI!\nChoose your color scheme:\n  > terminal\n    light\n",
		"first-run terms":             "Terms of Service & Data Use\n> [x] Yes, I agree\n [Previous]  Done\n",
		"after a restart in the pane": ready + "\nWelcome to the Antigravity CLI. You are currently not signed in.\nSelect login method:\n",
	} {
		if isWorkerInputReady("agy", screen) {
			t.Errorf("%s must not be ready", name)
		}
	}
	if !HoldsFirstMessage("agy --dangerously-skip-permissions") || !HoldsFirstMessage("/home/u/.local/bin/agy") {
		t.Fatalf("agy waits for its prompt before it is handed a first message")
	}
	if HoldsFirstMessage("antigravity") {
		t.Fatalf("antigravity is the desktop launcher, not an agent pane")
	}
}
