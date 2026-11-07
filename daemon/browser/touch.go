package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// The person may use a browser an agent holds, without taking it over: what
// they do is delivered to the page and told to the agent with its next
// result, never turned into a rule that stops it. Only Take over (the person
// holds the lease) and Stop (halt) are hard.

// person says why an agent cannot act, in words it can act on. It matches
// both ErrBusy (asking for control) and ErrLease (acting under a lease).
type person struct{ text string }

func (p person) Error() string        { return p.text }
func (p person) Is(target error) bool { return target == ErrBusy || target == ErrLease }

var (
	errPersonInControl error = person{"The person took over this browser and was told you are waiting. Do other work or wait a while, then request control again; do not poll"}
	errHalted          error = person{"The person stopped you using this browser. Do not request it again; tell them you stopped and what is left"}
)

// maxTouches bounds what is remembered between two actions of an agent.
const maxTouches = 64

type touch struct {
	kind   string // click, scroll, type, key
	at     time.Time
	during bool // while the agent's own action was running
	chars  int  // characters typed; the characters themselves are never kept
	name   string
}

type span struct{ from, to time.Time }

func workerOf(o Owner) string { w, _, _ := strings.Cut(o.ID, "/"); return w }

// touchOf is what is worth telling an agent about one input event.
func touchOf(i Input, during bool, now time.Time) (touch, bool) {
	t := touch{at: now, during: during}
	switch i.Kind {
	case "mousePressed":
		t.kind = "click"
	case "mouseWheel":
		t.kind = "scroll"
	case "text":
		t.kind, t.chars = "type", utf8.RuneCountInString(i.Text)
	case "keyDown":
		if utf8.RuneCountInString(i.Key) == 1 {
			t.kind, t.chars = "type", 1
		} else {
			t.kind, t.name = "key", i.Key
		}
	default:
		return touch{}, false
	}
	return t, true
}

func (e *entry) addTouchLocked(t touch) {
	e.touches = append(e.touches, t)
	if len(e.touches) > maxTouches {
		e.touches = e.touches[len(e.touches)-maxTouches:]
	}
}

// touch delivers the person's input to the page without a lease and without
// waiting behind the agent's action. An agent that holds the browser is told.
func (m *Manager) touch(ctx context.Context, e *entry, o Owner, q Request) (Response, error) {
	if q.Command == nil || q.Command.Input == nil {
		return Response{}, errors.New("Input is required")
	}
	input := *q.Command.Input
	e.mu.Lock()
	if e.deleted || e.uncertain || e.runtime == nil || !e.runtime.Alive() {
		e.mu.Unlock()
		return Response{}, ErrUnavailable
	}
	if e.transferring || (e.owner.Kind == "human" && e.owner != o) {
		e.mu.Unlock()
		return Response{}, ErrBusy
	}
	rt, agentHolds, during := e.runtime, e.owner.Kind == "agent", e.agentBusy
	e.mu.Unlock()
	actionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := rt.Command(actionCtx, Command{Kind: "input", Input: &input}); err != nil {
		return Response{}, err
	}
	if t, ok := touchOf(input, during, time.Now()); ok && agentHolds {
		e.mu.Lock()
		if e.owner.Kind == "agent" {
			e.addTouchLocked(t)
		}
		e.mu.Unlock()
	}
	return Response{}, nil
}

// halt ends the agent's control and refuses that Worker until the person
// hands the browser back or restarts it.
func (m *Manager) halt(ctx context.Context, e *entry) error {
	e.mu.Lock()
	if e.owner.Kind == "agent" {
		if e.halted == nil {
			e.halted = map[string]bool{}
		}
		e.halted[workerOf(e.owner)] = true
	}
	e.mu.Unlock()
	return m.releaseMatching(ctx, e, func(x Owner) bool { return x.Kind == "agent" })
}

// notesFor drains what the person did since the agent last heard, as plain
// sentences. The page address comes without its query or fragment.
func (m *Manager) notesFor(ctx context.Context, e *entry) []string {
	e.mu.Lock()
	touches, handed := e.touches, e.handedBack
	e.touches, e.handedBack = nil, nil
	rt := e.runtime
	e.mu.Unlock()
	if len(touches) == 0 && handed == nil {
		return nil
	}
	var notes []string
	now := time.Now()
	if handed != nil {
		notes = append(notes, fmt.Sprintf("The person had this browser from %s to %s and handed it back.", handed.from.Format("15:04:05"), handed.to.Format("15:04:05")))
	}
	if len(touches) > 0 {
		count := map[string]int{}
		chars, during := 0, false
		keys := []string{}
		for _, t := range touches {
			count[t.kind]++
			chars += t.chars
			during = during || t.during
			if t.kind == "key" {
				keys = append(keys, t.name)
			}
		}
		var parts []string
		if n := count["click"]; n > 0 {
			parts = append(parts, plural(n, "click"))
		}
		if chars > 0 {
			parts = append(parts, plural(chars, "typed character"))
		}
		if len(keys) > 0 {
			parts = append(parts, "keys "+strings.Join(keys, ", "))
		}
		if n := count["scroll"]; n > 0 {
			parts = append(parts, plural(n, "scroll"))
		}
		last := touches[len(touches)-1].at
		line := fmt.Sprintf("The person used the page while you held it: %s, the last %ds ago.", strings.Join(parts, ", "), int(now.Sub(last).Seconds()))
		if during {
			line += " Some of it happened while your own action was running, so that action may not have done what you meant."
		}
		notes = append(notes, line)
	}
	if page := m.pageOf(ctx, rt); page != "" {
		notes = append(notes, "The page is now "+page+".")
	}
	notes = append(notes, "Element refs from before may be stale: read the page again before acting, and judge from it whether the person's input matters to your task. They may have signed in or typed something private, which you do not see. Carry on, wait or ask them as you see fit.")
	return notes
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// pageOf is the selected tab's address without query or fragment, or "" when
// the runtime cannot say.
func (m *Manager) pageOf(ctx context.Context, rt Runtime) string {
	if rt == nil {
		return ""
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw, err := rt.Command(c, Command{Kind: "tabs"})
	if err != nil {
		return ""
	}
	var tabs []struct{ ID, URL string }
	if json.Unmarshal(raw, &tabs) != nil {
		return ""
	}
	for _, t := range tabs {
		if t.ID == rt.Target() {
			u, err := url.Parse(t.URL)
			if err != nil || u.Host == "" {
				return ""
			}
			return u.Scheme + "://" + u.Host + u.Path
		}
	}
	return ""
}
