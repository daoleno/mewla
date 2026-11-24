package watcher

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A prompt's ID is drawn from what it shows, so an agent that asks the same
// question again, word for word, shows the same ID. Each showing is stamped
// with when it appeared (Prompt.AskedAt): the answer to one showing is never
// taken for the answer to the next, on the computer or on a phone.

// promptShowingBlink is how long a prompt may vanish from the screen and still
// be the same showing when it is back: a redraw or a half-read frame makes a
// dialog blink out for a poll. It matches the server's promptLeftGrace.
const promptShowingBlink = 5 * time.Second

// promptShowing is the prompt a Session's pane last showed.
type promptShowing struct {
	id     string
	callID string
	at     time.Time
	// goneAt is when the pane stopped showing it, zero while it shows.
	goneAt time.Time
	// answered: Mewla answered it. Once it has left the screen, the same
	// prompt again is a new showing however soon it comes back.
	answered bool
}

type promptShowings struct {
	mu       sync.Mutex
	sessions map[string]promptShowing
}

// stamp returns prompt with the time its showing appeared (nil for none).
// The same prompt read again keeps its stamp; so does one back within a blink,
// unless Mewla answered it or it names another tool call.
func (p *promptShowings) stamp(sessionID string, prompt *classifier.Prompt, now time.Time) *classifier.Prompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = map[string]promptShowing{}
	}
	shown, had := p.sessions[sessionID]
	if prompt == nil {
		switch {
		case !had:
		case shown.goneAt.IsZero():
			shown.goneAt = now
			p.sessions[sessionID] = shown
		case now.Sub(shown.goneAt) >= promptShowingBlink:
			delete(p.sessions, sessionID)
		}
		return nil
	}
	same := had && shown.id == prompt.ID &&
		(shown.callID == "" || prompt.CallID == "" || shown.callID == prompt.CallID) &&
		(shown.goneAt.IsZero() || (!shown.answered && now.Sub(shown.goneAt) < promptShowingBlink))
	if !same {
		shown = promptShowing{id: prompt.ID, at: now.UTC().Truncate(time.Millisecond)}
	}
	shown.goneAt = time.Time{}
	if prompt.CallID != "" {
		shown.callID = prompt.CallID
	}
	p.sessions[sessionID] = shown
	stamped := *prompt
	at := shown.at
	stamped.AskedAt = &at
	return &stamped
}

// answered notes that Mewla answered the prompt a Session showed.
func (p *promptShowings) answered(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if shown, ok := p.sessions[sessionID]; ok {
		shown.answered = true
		p.sessions[sessionID] = shown
	}
}

// promptAnswered marks the Session's prompt answered through Mewla once an
// answer's keys went in.
func (w *Watcher) promptAnswered(sessionID string, err error) error {
	if err == nil {
		w.showings.answered(sessionID)
	}
	return err
}
