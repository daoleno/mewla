package watcher

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// trustMenuPane is Claude 2.1.322's folder trust menu: no numbers, the cursor
// on "No, exit". Up and Down move the cursor, Enter picks its row, and a typed
// number does nothing (as on the real menu).
type trustMenuPane struct {
	t      *testing.T
	screen string
	cursor int // 0: No, exit; 1: Yes, I trust this folder
	picked string
	keys   []string
}

func newTrustMenuPane(t *testing.T) *trustMenuPane {
	raw, err := os.ReadFile("../classifier/testdata/claude-2.1.322/folder-trust-50x40.txt")
	if err != nil {
		t.Fatal(err)
	}
	return &trustMenuPane{t: t, screen: string(raw)}
}

func (p *trustMenuPane) capture() (string, error) {
	if p.picked != "" {
		return "● Ready\n\n❯ \n", nil
	}
	rows := [2]string{"   No, exit", "   Yes, I trust this folder"}
	rows[p.cursor] = " ❯ " + strings.TrimSpace(rows[p.cursor])
	screen := strings.Replace(p.screen, " ❯ No, exit\n   Yes, I trust this folder", rows[0]+"\n"+rows[1], 1)
	if screen == p.screen && p.cursor != 0 {
		p.t.Fatal("fixture did not change")
	}
	return screen, nil
}
func (p *trustMenuPane) captureDialog() (string, classifier.CaptureInfo, error) {
	screen, err := p.capture()
	return screen, classifier.CaptureInfo{Exact: true, Alternate: true}, err
}
func (p *trustMenuPane) sendKey(key string) error {
	p.keys = append(p.keys, key)
	switch key {
	case "Down":
		p.cursor = min(p.cursor+1, 1)
	case "Up":
		p.cursor = max(p.cursor-1, 0)
	case "Enter":
		p.picked = [2]string{"No, exit", "Yes, I trust this folder"}[p.cursor]
	}
	return nil
}
func (p *trustMenuPane) sendText(string) error { return nil }

// FAILURES Round 3 #2: the trust menu is a prompt the person answers, and an
// answer moves the cursor to its row before Enter instead of typing a number.
func TestUnnumberedTrustMenuIsAnsweredByMovingTheCursor(t *testing.T) {
	for _, tc := range []struct {
		number int
		keys   string
		picked string
	}{
		{2, "Down Enter", "Yes, I trust this folder"},
		{1, "Enter", "No, exit"},
	} {
		w, sessionID := watcherWithClaude(t)
		pane := newTrustMenuPane(t)
		prompt, err := w.currentPrompt(pane, sessionID)
		if err != nil || prompt == nil || !prompt.Unnumbered || prompt.Kind != classifier.PromptStartup {
			t.Fatalf("trust menu prompt %+v (%v)", prompt, err)
		}
		if err := w.answerPrompt(pane, sessionID, prompt.ID, tc.number); err != nil {
			t.Fatalf("answer %d: %v", tc.number, err)
		}
		if got := strings.Join(pane.keys, " "); got != tc.keys || pane.picked != tc.picked {
			t.Fatalf("answer %d: keys %q picked %q, want %q %q", tc.number, got, pane.picked, tc.keys, tc.picked)
		}
	}
}

// A chat send pastes its text and presses Enter: with a menu open that Enter
// picks whatever the cursor is on ("No, exit" on the trust menu). The send is
// refused while a prompt is open, and goes through once it is answered.
func TestChatInputIsRefusedWhileAPromptIsOpen(t *testing.T) {
	w, sessionID := watcherWithClaude(t)
	pane := newTrustMenuPane(t)
	err := w.refuseInputUnderPrompt(pane, sessionID, "receipt-1")
	if !errors.Is(err, ErrPromptOpen) || InputOutcomeFromError(err) != InputNotSubmitted {
		t.Fatalf("send under the trust menu: %v", err)
	}
	if len(pane.keys) != 0 {
		t.Fatalf("keys sent: %v", pane.keys)
	}
	pane.picked = "Yes, I trust this folder"
	if err := w.refuseInputUnderPrompt(pane, sessionID, "receipt-1"); err != nil {
		t.Fatalf("send at the composer: %v", err)
	}
}
