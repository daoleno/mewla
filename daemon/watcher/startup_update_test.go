package watcher

import "testing"

func TestCodexUpdatePickerIsNotComposer(t *testing.T) {
	// Real 0.159.2 startup picker. The selected menu item uses the same glyph
	// as the composer, and stale TUI chrome can still be visible above it.
	picker := "OpenAI Codex\nUpdate available · 0.158.0 → 0.159.2\nRelease notes: https://github.com/openai/codex/releases/latest\n› 1. Update now (runs `npm install -g @openai/codex`)\n  2. Skip\n  3. Skip until next version\n  enter continue · esc skip\n"
	if isCodexStartupReady(picker) {
		t.Fatal("update picker accepts the brief's Enter as Update now")
	}
}
