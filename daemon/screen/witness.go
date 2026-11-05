package screen

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A click or a key sent to an app behind other windows can be dropped without
// any error: the window server accepts the event and the app ignores it. So
// the answer to such an action says what an accessibility reading of the app
// shows after it, never just that the event was sent. The reading is the
// focused field (which one, its text, the caret in it) and the window's
// title; it is a witness to an effect, not a proof of one's absence: a click
// can land and change something the reading doesn't cover.

// Witness is what the app shows of its focus.
type Witness struct {
	// Readable is false when the app gave no reading at all.
	Readable bool
	Window   string
	Field    string // the focused element's role and title
	Text     string
	Caret    string
}

// appWitness is a backend that can read an app's Witness.
type appWitness interface {
	Witness(ctx context.Context, pid int) (Witness, error)
}

// changedFrom names what differs between two readings.
func (w Witness) changedFrom(before Witness) []string {
	var changed []string
	if w.Field != before.Field {
		changed = append(changed, "which field has the focus")
	}
	if w.Text != before.Text {
		changed = append(changed, "the focused field's text")
	}
	if w.Caret != before.Caret {
		changed = append(changed, "the caret or selection")
	}
	if w.Window != before.Window {
		changed = append(changed, "the window title")
	}
	return changed
}

// effectLine words the result for an agent. sent is what was sent ("click",
// "keys"); behind says it went to an app that isn't in front.
func effectLine(app string, sent string, before, after Witness, haveBefore bool) string {
	switch {
	case !haveBefore || !before.Readable || !after.Readable:
		return fmt.Sprintf("Effect not checked: %s gave no accessibility reading of its focus, so whether the %s landed is unknown; take a screenshot of it, or read it with app_state.", app, sent)
	}
	if changed := after.changedFrom(before); len(changed) > 0 {
		return fmt.Sprintf("Effect seen: %s changed %s.", app, strings.Join(changed, ", "))
	}
	return fmt.Sprintf("No effect seen: nothing in %s's focused field, caret or window title changed after the %s. It may have been ignored (an app behind other windows can drop input) or it changed something this reading doesn't cover. Check with app_state or a screenshot before relying on it; to be sure, use an element id, or bring_forward when nobody is using the computer.", app, sent)
}

// witnessAfter reads the app until it differs from before, for at most wait.
func witnessAfter(ctx context.Context, w appWitness, pid int, before Witness, wait time.Duration) (Witness, error) {
	deadline := time.Now().Add(wait)
	for {
		after, err := w.Witness(ctx, pid)
		if err != nil || len(after.changedFrom(before)) > 0 || time.Now().After(deadline) {
			return after, err
		}
		select {
		case <-time.After(80 * time.Millisecond):
		case <-ctx.Done():
			return after, ctx.Err()
		}
	}
}
