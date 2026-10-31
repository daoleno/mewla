package phone

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// target is the element an action names: by id from the latest screen, else by the text it shows,
// which is waited for a few seconds when the screen does not show it yet.
func (a *Agent) target(ctx context.Context, w worker, act AgentAction, t *trace) (Element, error) {
	if act.Element > 0 {
		return a.resolve(ctx, act.Element, t)
	}
	if err := a.mgr.CheckWait(ctx); err != nil {
		return Element{}, err
	}
	deadline := a.now().Add(matchWait)
	for {
		el, found, err := a.session(a.mgr.Generation()).Match(act.Match)
		if err != nil {
			return Element{}, err
		}
		if found {
			t.element = &el
			return el, nil
		}
		if !a.now().Before(deadline) {
			return Element{}, fmt.Errorf("nothing on the screen matches %q; read the screen and use an id from it", act.Match)
		}
		// Not there yet, or the reading is from before the screen changed: read again.
		reply, err := a.do(ctx, w, OpScreen, ScreenArgs{}, false)
		if err != nil {
			return Element{}, err
		}
		if reply.Screen != nil {
			a.session(reply.Gen).Observe(reply.Screen)
		}
		if _, found, _ := a.session(reply.Gen).Match(act.Match); found {
			continue
		}
		if err := a.sleep(ctx, pollEvery); err != nil {
			return Element{}, err
		}
	}
}

// stepActions are what a batch may hold: the actions that go on the screen the previous one left.
var stepActions = map[string]bool{ActLaunch: true, ActTap: true, ActType: true, ActSwipe: true, ActKey: true, ActWait: true}

// runSteps runs a batch in order and returns the screen the last step left, with a line for each
// step. A step that cannot be done (an element that is not there, a text that never appeared)
// ends the batch: the answer says which step it was and shows the screen as it stands, so the
// agent can carry on from there. A refusal (the person took over, stopped it, the phone is not
// ready) is an error as everywhere, and says which step it met.
func (a *Agent) runSteps(ctx context.Context, w worker, act AgentAction) (AgentResult, error) {
	steps := act.Steps
	if len(steps) == 0 || len(steps) > maxSteps {
		return AgentResult{}, fmt.Errorf("steps takes 1 to %d actions", maxSteps)
	}
	for i, step := range steps {
		if !stepActions[step.Action] {
			return AgentResult{}, fmt.Errorf("step %d: %q can't be a step; use launch, tap, type, swipe, key or wait", i+1, step.Action)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, stepsBudget)
	defer cancel()
	var lines []string
	var last AgentResult
	for i, step := range steps {
		// Only the screen the batch ends on is worth a picture.
		step.Image = act.Image && i == len(steps)-1
		res, err := a.runStep(ctx, w, step)
		if err != nil {
			var perr *Error
			if errors.As(err, &perr) {
				return AgentResult{}, &Error{perr.Code, fmt.Sprintf("step %d (%s): %s", i+1, step.Action, perr.Message)}
			}
			if ctx.Err() != nil {
				return AgentResult{}, fmt.Errorf("step %d (%s): the steps ran out of their %s", i+1, step.Action, stepsBudget)
			}
			lines = append(lines, fmt.Sprintf("Step %d (%s) failed: %v", i+1, step.Action, err),
				fmt.Sprintf("The %d step(s) after it did not run. The screen as it stands:", len(steps)-i-1))
			shown, serr := a.screen(ctx, w, act.Image)
			if serr != nil {
				return AgentResult{Text: strings.Join(lines, "\n")}, nil
			}
			return AgentResult{Text: strings.Join(lines, "\n") + "\n" + shown.Text, Image: shown.Image}, nil
		}
		lines = append(lines, fmt.Sprintf("%d. %s: done", i+1, describeStep(step)))
		last = res
	}
	return AgentResult{Text: strings.Join(lines, "\n") + "\n" + last.Text, Image: last.Image}, nil
}

// runStep is one step as its own action, so each lands in the audit; a wait for text that never
// shows is a failure here, where on its own it is only an answer.
func (a *Agent) runStep(ctx context.Context, w worker, step AgentAction) (AgentResult, error) {
	if step.Action != ActWait || strings.TrimSpace(step.For) == "" {
		return a.Act(ctx, w.id, w.name, step)
	}
	var t trace
	res, found, err := a.waitFor(ctx, w, step)
	if err == nil && !found {
		err = fmt.Errorf("%q did not appear", step.For)
	}
	a.audit.Record(w.id, step, t, err)
	return res, err
}

func describeStep(step AgentAction) string {
	switch step.Action {
	case ActLaunch:
		return "launch " + step.App
	case ActTap:
		if step.Match != "" && step.Element == 0 {
			return fmt.Sprintf("tap %q", step.Match)
		}
	case ActType:
		return fmt.Sprintf("type %d characters", len([]rune(step.Text)))
	case ActKey:
		return "key " + step.Key
	case ActWait:
		if step.For != "" {
			return fmt.Sprintf("wait for %q", step.For)
		}
	}
	return step.Action
}
