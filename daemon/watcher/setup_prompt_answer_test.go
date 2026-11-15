package watcher

import (
	"os"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// setupPane shows one screen per state and moves between them on the keys
// and text the answer sends, as the real setup screens do.
type setupPane struct {
	t      *testing.T
	screen string
	next   map[string]string // "key:Enter" or "text" -> the screen after it
	keys   []string
	typed  []string
}

func readScreen(t *testing.T, file string) string {
	t.Helper()
	raw, err := os.ReadFile("../classifier/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (p *setupPane) capture() (string, error) { return p.screen, nil }
func (p *setupPane) captureDialog() (string, classifier.CaptureInfo, error) {
	return p.screen, classifier.CaptureInfo{Exact: true}, nil
}
func (p *setupPane) sendKey(key string) error {
	p.keys = append(p.keys, key)
	if screen, ok := p.next["key:"+key]; ok {
		p.screen = screen
	}
	return nil
}
func (p *setupPane) sendText(text string) error {
	p.typed = append(p.typed, text)
	if screen, ok := p.next["text"]; ok {
		p.screen = screen
	}
	return nil
}

const setupComposer = "● Ready\n\n❯ \n"

func setupWatcher(t *testing.T, command string) (*Watcher, string) {
	w, sessionID := watcherWithClaude(t)
	w.workers[sessionID].Command = command
	return w, sessionID
}

// "Press Enter to continue" is answered with Enter, not the option's digit.
func TestASetupScreenIsAnsweredWithTheKeyItNames(t *testing.T) {
	w, sessionID := setupWatcher(t, "claude")
	pane := &setupPane{t: t, screen: readScreen(t, "claude-2.1.325/security-notes-windows-43x39.txt"), next: map[string]string{"key:Enter": setupComposer}}
	prompt, err := w.currentPrompt(pane, sessionID)
	if err != nil || prompt == nil || len(prompt.Options) != 1 {
		t.Fatalf("prompt %+v (%v)", prompt, err)
	}
	if err := w.answerPrompt(pane, sessionID, prompt.ID, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pane.keys, " ") != "Enter" {
		t.Fatalf("keys %v", pane.keys)
	}
}

// Codex's sign-in menu ignores a typed digit: the answer moves its cursor.
func TestCodexSignInMenuIsAnsweredByMovingTheCursor(t *testing.T) {
	w, sessionID := setupWatcher(t, "codex")
	device := readScreen(t, "codex-0.169.0/device-code-windows-43x39.txt")
	pane := &setupPane{t: t, screen: readScreen(t, "codex-0.169.0/login-windows-200x50.txt"), next: map[string]string{
		"key:Down": readScreen(t, "codex-0.169.0/login-cursor-on-2-windows-43x39.txt"),
	}}
	prompt, err := w.currentPrompt(pane, sessionID)
	if err != nil || prompt == nil || !prompt.Unnumbered {
		t.Fatalf("prompt %+v (%v)", prompt, err)
	}
	pane.next["key:Enter"] = device
	if err := w.answerPrompt(pane, sessionID, prompt.ID, 2); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pane.keys, " ") != "Down Enter" {
		t.Fatalf("keys %v", pane.keys)
	}
	// The device page: its one option is Esc.
	signIn, err := w.currentPrompt(pane, sessionID)
	if err != nil || signIn == nil || signIn.Kind != classifier.PromptSignIn || signIn.Code != "ABCD-1234Z" {
		t.Fatalf("device prompt %+v (%v)", signIn, err)
	}
	pane.keys, pane.next = nil, map[string]string{"key:Escape": readScreen(t, "codex-0.169.0/login-windows-200x50.txt")}
	if err := w.answerPrompt(pane, sessionID, signIn.ID, 1); err != nil || strings.Join(pane.keys, " ") != "Escape" {
		t.Fatalf("cancel: %v, keys %v", err, pane.keys)
	}
}

// Claude's sign-in takes the code its page shows: typed, then Enter. One line
// only, and only into the sign-in prompt that was answered.
func TestClaudeSignInIsAnsweredWithTheCode(t *testing.T) {
	w, sessionID := setupWatcher(t, "claude")
	link := readScreen(t, "claude-2.1.325/signin-link-windows-43x39.txt")
	pane := &setupPane{t: t, screen: link, next: map[string]string{"key:Enter": setupComposer}}
	prompt, err := w.currentPrompt(pane, sessionID)
	if err != nil || prompt == nil || prompt.Input == "" {
		t.Fatalf("prompt %+v (%v)", prompt, err)
	}
	for _, bad := range []string{"", "code\nrm -rf /", "a\x1b[2J"} {
		if err := w.answerPromptText(pane, sessionID, prompt.ID, bad); err == nil {
			t.Fatalf("%q was typed", bad)
		}
	}
	if err := w.answerPromptText(pane, sessionID, "another", "abc#def"); err == nil {
		t.Fatal("typed into a prompt that is not open")
	}
	if len(pane.typed)+len(pane.keys) != 0 {
		t.Fatalf("sent %v %v", pane.typed, pane.keys)
	}
	if err := w.answerPromptText(pane, sessionID, prompt.ID, "  abc#def-123  "); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pane.typed, "|") != "abc#def-123" || strings.Join(pane.keys, " ") != "Enter" {
		t.Fatalf("typed %v keys %v", pane.typed, pane.keys)
	}
	// A menu is not answered with text.
	pane.screen = readScreen(t, "claude-2.1.325/login-method-windows-43x39.txt")
	menu, _ := w.currentPrompt(pane, sessionID)
	if err := w.answerPromptText(pane, sessionID, menu.ID, "1"); err == nil {
		t.Fatal("a menu took text")
	}
}
