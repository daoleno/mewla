package screen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A batch runs several actions in one call. It is a list, not a program:
// each step passes the same gates and leaves its own audit line, and the
// whole batch has a bound.
const (
	maxSteps     = 20
	stepsBudget  = 40 * time.Second // inside the relay's 60 seconds
	waitForMax   = 10 * time.Second
	waitForEvery = 500 * time.Millisecond
)

// ElementMatch names an element by what it is: its role, and its name or
// value, matched without case, whole first and then as part.
type ElementMatch struct {
	Role  string `json:"role,omitempty"`
	Title string `json:"title,omitempty"`
	Value string `json:"value,omitempty"`
}

func (m ElementMatch) String() string {
	parts := []string{}
	if m.Role != "" {
		parts = append(parts, strings.TrimPrefix(m.Role, "AX"))
	}
	if m.Title != "" {
		parts = append(parts, `"`+m.Title+`"`)
	}
	if m.Value != "" {
		parts = append(parts, `= "`+m.Value+`"`)
	}
	return strings.Join(parts, " ")
}

// match finds the element m names in the tree and returns its id. With
// one, several equally good matches are an error rather than a guess.
func (t *ElementTree) match(m ElementMatch, one bool) (int, error) {
	if m.Role == "" && m.Title == "" && m.Value == "" {
		return 0, errors.New("a match needs a role, title or value")
	}
	role := strings.TrimPrefix(strings.ToLower(m.Role), "ax")
	var whole, part []int
	for i, e := range t.Elements {
		if role != "" && strings.TrimPrefix(strings.ToLower(e.Role), "ax") != role {
			continue
		}
		value := e.Value
		if e.Role == "AXSecureTextField" {
			value = ""
		}
		if m.Value != "" && !strings.Contains(strings.ToLower(value), strings.ToLower(m.Value)) {
			continue
		}
		switch {
		case m.Title == "" || strings.EqualFold(e.Title, m.Title):
			whole = append(whole, t.id(i))
		case strings.Contains(strings.ToLower(e.Title), strings.ToLower(m.Title)):
			part = append(part, t.id(i))
		}
	}
	found := whole
	if len(found) == 0 {
		found = part
	}
	switch {
	case len(found) == 0:
		return 0, fmt.Errorf("no element matches %s", m)
	case len(found) > 1 && one:
		ids := make([]string, len(found))
		for i, id := range found {
			ids[i] = fmt.Sprintf("[%d]", id)
		}
		return 0, fmt.Errorf("%d elements match %s: %s; name one by id", len(found), m, strings.Join(ids, ", "))
	}
	return found[0], nil
}

// waitFor reads an app until an element matching appears, for up to
// Duration seconds (at most waitForMax), and leaves that read's ids.
func (a *agentSession) waitFor(ctx context.Context, action AgentAction) (AgentResult, error) {
	if action.Match == nil {
		return AgentResult{}, errors.New("wait_for needs match")
	}
	limit := time.Duration(action.Duration * float64(time.Second))
	if limit <= 0 || limit > waitForMax {
		limit = waitForMax
	}
	deadline := time.Now().Add(limit)
	for {
		result, err := a.appState(ctx, action.App)
		if err != nil {
			return AgentResult{}, err
		}
		a.appMu.Lock()
		id, matchErr := a.tree.match(*action.Match, false)
		a.appMu.Unlock()
		if matchErr == nil {
			result.Text = fmt.Sprintf("Found [%d].\n", id) + result.Text
			return result, nil
		}
		if time.Now().After(deadline) {
			return AgentResult{}, fmt.Errorf("%s did not appear within %s", action.Match, limit.Round(time.Second))
		}
		select {
		case <-time.After(waitForEvery):
		case <-ctx.Done():
			return AgentResult{}, ctx.Err()
		case <-a.ctx.Done():
			return AgentResult{}, context.Cause(a.ctx)
		}
	}
}

// expect checks that an element matching is in the latest read, reading the
// app first when one is named.
func (a *agentSession) expect(ctx context.Context, action AgentAction) (AgentResult, error) {
	if action.Match == nil {
		return AgentResult{}, errors.New("expect needs match")
	}
	if action.App != "" {
		if _, err := a.appState(ctx, action.App); err != nil {
			return AgentResult{}, err
		}
	}
	a.appMu.Lock()
	defer a.appMu.Unlock()
	if a.tree == nil {
		return AgentResult{}, errors.New("read an app with app_state first")
	}
	id, err := a.tree.match(*action.Match, false)
	if err != nil {
		return AgentResult{}, fmt.Errorf("expected %s: %w", action.Match, err)
	}
	return AgentResult{Text: fmt.Sprintf("As expected: [%d] matches %s", id, action.Match)}, nil
}

// runSteps runs a batch in order and stops at the first step that fails,
// which fails the batch. The answer lists each step's outcome, then the last
// step's own result.
func (s *Service) runSteps(ctx context.Context, worker string, steps []AgentAction) (AgentResult, error) {
	if len(steps) == 0 || len(steps) > maxSteps {
		return AgentResult{}, fmt.Errorf("steps takes 1 to %d actions", maxSteps)
	}
	for i, step := range steps {
		if step.Action == "steps" || len(step.Steps) > 0 {
			return AgentResult{}, fmt.Errorf("step %d: steps can't hold steps", i+1)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, stepsBudget)
	defer cancel()
	var b strings.Builder
	var last AgentResult
	for i, step := range steps {
		result, err := s.Act(ctx, worker, step)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("the steps ran out of their %s", stepsBudget)
			}
			fmt.Fprintf(&b, "%d. %s: failed: %v\nStopped there; the steps after it did not run.", i+1, step.Action, err)
			return AgentResult{}, errors.New(b.String())
		}
		fmt.Fprintf(&b, "%d. %s: done\n", i+1, step.Action)
		last = result
	}
	if last.Text != "" {
		b.WriteString("\n" + last.Text)
	}
	return AgentResult{Text: b.String(), Image: last.Image, Width: last.Width, Height: last.Height}, nil
}
