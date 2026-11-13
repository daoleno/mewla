package watcher

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

type dialogPane struct {
	visible string
	dialog  string
	info    classifier.CaptureInfo
}

func (p *dialogPane) capture() (string, error) { return p.visible, nil }
func (p *dialogPane) captureDialog() (string, classifier.CaptureInfo, error) {
	return p.dialog, p.info, nil
}
func (p *dialogPane) sendKey(string) error  { return nil }
func (p *dialogPane) sendText(string) error { return nil }

func claudeDialogFixture() (whole, visible string) {
	var body []string
	for index := 0; index < 30; index++ {
		body = append(body, "   # part"+strings.Repeat("x", index%4))
	}
	whole = "Claude Code v2.1.0\n────────────────────────────────────────\n Bash command\n\n   echo HEAD\n" + strings.Join(body, "\n") +
		"\n   Write it\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	lines := strings.Split(whole, "\n")
	return whole, strings.Join(lines[len(lines)-12:], "\n")
}

func watcherWithClaude(t *testing.T) (*Watcher, string) {
	t.Helper()
	w := New(0)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	sessionID := "mewla-worker-history:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateBlocked, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)
	return w, sessionID
}

// A dialog taller than what is visible is identified from the exact capture of
// the whole scrollback, so the id Brain was told answers the same dialog later.
func TestCurrentPromptIdentifiesADialogFromTheDialogCapture(t *testing.T) {
	whole, visible := claudeDialogFixture()
	w, sessionID := watcherWithClaude(t)
	info := classifier.CaptureInfo{Exact: true}
	polled := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: classifier.Worker{Command: "claude"}, Capture: &info, PaneContent: whole})
	if polled.Prompt == nil || !strings.Contains(polled.Prompt.Request, "echo HEAD") {
		t.Fatalf("the poll's prompt: %+v", polled.Prompt)
	}
	got, err := w.currentPrompt(&dialogPane{visible: visible, dialog: whole, info: info}, sessionID)
	if err != nil || got == nil || got.ID != polled.Prompt.ID {
		t.Fatalf("answer-time prompt %+v (err %v), want id %s", got, err, polled.Prompt.ID)
	}
}

// A pane that cannot hand over an exact capture, or an inexact one, gives no id:
// the dialog is blocked and unanswerable, and the refusal says why.
func TestCurrentPromptRefusesADialogItCannotCaptureExactly(t *testing.T) {
	whole, visible := claudeDialogFixture()
	w, sessionID := watcherWithClaude(t)
	for name, pane := range map[string]claudeChoicePane{
		"visible screen only": &struct{ claudeChoicePane }{&dialogPane{visible: visible, dialog: whole}},
		"capture not exact":   &dialogPane{visible: visible, dialog: whole, info: classifier.CaptureInfo{}},
	} {
		got, err := w.currentPrompt(pane, sessionID)
		if got != nil {
			t.Fatalf("%s: an id was issued: %+v", name, got)
		}
		_ = err // visible-only panes report no dialog capture; an inexact one is refused with a reason
	}
	if _, err := w.currentPrompt(&dialogPane{visible: visible, dialog: whole, info: classifier.CaptureInfo{}}, sessionID); err == nil || !strings.Contains(err.Error(), "cannot be identified exactly") {
		t.Fatalf("an inexact capture was not refused with its reason: %v", err)
	}
}

// The prompt an answer reads at answer time carries the tool call the poll
// already named, so the record of the answer says what was approved.
func TestCurrentPromptCarriesTheToolCallItAsksAbout(t *testing.T) {
	whole, visible := claudeDialogFixture()
	w, sessionID := watcherWithClaude(t)
	w.SetPromptEnricher(func(_ string, prompt classifier.Prompt) classifier.Prompt {
		prompt.CallID, prompt.Tool, prompt.Summary = "toolu_1", "Bash", "echo HEAD"
		return prompt
	})
	got, err := w.currentPrompt(&dialogPane{visible: visible, dialog: whole, info: classifier.CaptureInfo{Exact: true}}, sessionID)
	if err != nil || got == nil || got.Tool != "Bash" || got.Summary != "echo HEAD" || got.CallID != "toolu_1" {
		t.Fatalf("prompt = %+v (err %v)", got, err)
	}
}
