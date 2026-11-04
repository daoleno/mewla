package watcher

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

type historyPane struct {
	visible string
	history string
}

func (p *historyPane) capture() (string, error)        { return p.visible, nil }
func (p *historyPane) captureHistory() (string, error) { return p.history, nil }
func (p *historyPane) sendKey(string) error            { return nil }
func (p *historyPane) sendText(string) error           { return nil }

// A dialog taller than what is visible is identified from the capture the poll
// made, so the id Brain was told answers the same dialog later.
func TestCurrentPromptIdentifiesADialogFromTheHistoryCapture(t *testing.T) {
	var body []string
	for index := 0; index < 30; index++ {
		body = append(body, "   # part "+strings.Repeat("x", index%4))
	}
	whole := "Claude Code v2.1.0\n────────────────────────────────────────\n Bash command\n\n   echo HEAD\n" + strings.Join(body, "\n") +
		"\n   Write it\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	lines := strings.Split(whole, "\n")
	visible := strings.Join(lines[len(lines)-12:], "\n")

	w := New(0)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	sessionID := "mewla-worker-history:@1"
	worker := classifier.Worker{ID: sessionID, Delegated: true, State: classifier.StateBlocked, Command: "claude"}
	w.workers[sessionID] = &worker
	w.workerOrder = append(w.workerOrder, sessionID)

	polled := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: worker, PaneContent: whole})
	if polled.Prompt == nil || !strings.Contains(polled.Prompt.Request, "echo HEAD") {
		t.Fatalf("the poll's prompt: %+v", polled.Prompt)
	}
	got, err := w.currentPrompt(&historyPane{visible: visible, history: whole}, sessionID)
	if err != nil || got == nil || got.ID != polled.Prompt.ID {
		t.Fatalf("answer-time prompt %+v (err %v), want id %s", got, err, polled.Prompt.ID)
	}
	// With only the visible screen the dialog's first line is not there: no id.
	only := &struct{ claudeChoicePane }{&historyPane{visible: visible, history: whole}}
	if got, err := w.currentPrompt(only, sessionID); err != nil || (got != nil && got.ID == polled.Prompt.ID) {
		t.Fatalf("a visible-only read produced the poll's id: %+v (err %v)", got, err)
	}
}
