package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/trigger"
)

// brainTriggerSink reports a Trigger's events into its Brain Work.
type brainTriggerSink struct {
	service *brain.Service
}

func (s brainTriggerSink) Append(event trigger.Event) (trigger.Recorded, error) {
	recorded, opened, err := s.service.RaiseWorkEvent(brain.WorkEvent{
		WorkID: event.WorkID, Kind: event.Kind, DedupeKey: event.DedupeKey,
		PayloadRef: event.PayloadRef, SourceName: "trigger", Summary: event.Summary,
		DetailsJSON: event.DetailsJSON, Actionable: event.Actionable,
	})
	if err != nil {
		return trigger.Recorded{}, err
	}
	return trigger.Recorded{ID: recorded.ID, Actionable: opened}, nil
}

func (s brainTriggerSink) EventHandled(workID, eventID string) (bool, bool, error) {
	return s.service.WorkEventHandled(workID, eventID)
}

// openTriggers opens the Trigger store under the state directory and makes
// every Trigger's Work standing, so the closure judge leaves it open.
func openTriggers(stateDir string, store *brain.Store, service *brain.Service) (*trigger.Engine, error) {
	triggers, err := trigger.OpenStore(filepath.Join(stateDir, "triggers"))
	if err != nil {
		return nil, err
	}
	service.SetStandingWork(triggers.HasWork)
	return trigger.NewEngine(triggers, brainTriggerSink{service: service}, nil), nil
}

func triggerControlError(err error) control.Response {
	code := "trigger_request_failed"
	switch {
	case errors.Is(err, trigger.ErrNotFound):
		code = "trigger_not_found"
	case errors.Is(err, trigger.ErrInvalid):
		code = "invalid_trigger"
	case errors.Is(err, trigger.ErrBusy):
		code = "already_running"
	}
	return control.ErrorResponse(code, err.Error())
}

func (a *controlApp) handleTrigger(req control.Request) control.Response {
	if a == nil || a.triggers == nil {
		return control.ErrorResponse("trigger_unavailable", "Triggers are not configured.")
	}
	engine := a.triggers
	id := strings.TrimSpace(req.ID)
	respond := func(item trigger.Trigger, err error, confirmation string) control.Response {
		if err != nil {
			return triggerControlError(err)
		}
		view, err := engine.View(item.ID)
		if err != nil {
			view = trigger.ViewOf(item, time.Now())
		}
		return control.Response{OK: true, Trigger: &view, Confirmation: confirmation}
	}
	switch req.Type {
	case "trigger_list":
		return control.Response{OK: true, Triggers: engine.Views()}
	case "trigger_get":
		view, err := engine.View(id)
		if err != nil {
			return triggerControlError(err)
		}
		return control.Response{OK: true, Trigger: &view}
	case "trigger_create":
		if req.Trigger == nil {
			return control.ErrorResponse("invalid_trigger", "A Trigger is required.")
		}
		item, err := a.createTrigger(*req.Trigger)
		if err != nil {
			return triggerControlError(err)
		}
		return respond(item, nil, fmt.Sprintf("Watching %q every %s: %s. When it prints something new, Brain gets it on Work %s.",
			item.Title, trigger.Words(item.Interval()), item.Check, item.WorkID))
	case "trigger_pause":
		item, err := engine.Pause(id)
		return respond(item, err, "Paused. It checks again after resume.")
	case "trigger_resume":
		item, err := engine.Resume(id)
		return respond(item, err, "Resumed. It checks now.")
	case "trigger_check":
		ctx, cancel := context.WithTimeout(context.Background(), trigger.MaxTimeout+time.Minute)
		defer cancel()
		item, result, err := engine.CheckNow(ctx, id)
		if err != nil {
			return triggerControlError(err)
		}
		confirmation := "Checked: it printed nothing."
		switch {
		case result.Failed():
			confirmation = "The check failed: " + strings.TrimSpace(result.Failure())
		case strings.TrimSpace(result.Stdout) != "":
			confirmation = "Checked: it printed output (a firing when it differs from the previous check)."
		}
		return respond(item, nil, confirmation)
	case "trigger_delete":
		item, closeErr, err := a.deleteTrigger(id)
		if err != nil {
			return triggerControlError(err)
		}
		confirmation := "Deleted. It no longer watches."
		if closeErr != nil {
			confirmation += " Its Work stays open: " + closeErr.Error()
		}
		view := trigger.ViewOf(item, time.Now())
		return control.Response{OK: true, Trigger: &view, Confirmation: confirmation}
	}
	return control.ErrorResponse("unknown_request", req.Type)
}

// deleteTrigger removes a Trigger and closes the Work it created. closeErr
// says why that Work stays open.
func (a *controlApp) deleteTrigger(id string) (item trigger.Trigger, closeErr, err error) {
	item, err = a.triggers.Store().Delete(id)
	if err != nil {
		return item, nil, err
	}
	if item.OwnsWork {
		closeErr = a.closeTriggerWork(item.WorkID, "Trigger "+item.Title+" deleted")
	}
	return item, closeErr, nil
}

// createTrigger binds the Trigger to Work: the one it names, or a new
// standing Work in the Brain thread it came from.
func (a *controlApp) createTrigger(item trigger.Trigger) (trigger.Trigger, error) {
	item, err := item.Normalize()
	if err != nil {
		return trigger.Trigger{}, err
	}
	if a.brainStore == nil || a.brainService == nil {
		return trigger.Trigger{}, fmt.Errorf("Brain is not configured")
	}
	if item.WorkID != "" {
		existing, err := a.brainStore.Work(item.WorkID)
		if err != nil {
			return trigger.Trigger{}, fmt.Errorf("%w: Work %s: %v", trigger.ErrInvalid, item.WorkID, err)
		}
		if existing.Status == brain.WorkDone || existing.Status == brain.WorkCancelled {
			return trigger.Trigger{}, fmt.Errorf("%w: Work %s is %s", trigger.ErrInvalid, item.WorkID, existing.Status)
		}
		item.OwnsWork = false
		return a.triggers.Store().Create(item)
	}
	if ok, err := a.brainService.HasChatThread(item.SourceThreadID); err != nil || !ok {
		return trigger.Trigger{}, fmt.Errorf("%w: thread %s is not a Brain chat thread", trigger.ErrInvalid, item.SourceThreadID)
	}
	created, err := a.brainStore.CreateWork(brain.Work{
		Title:            "Trigger: " + item.Title,
		Objective:        fmt.Sprintf("Standing Trigger %q: every %s Mewla runs `%s` and appends a trigger.fired event here when its output changes (payload_ref is the output). When it fires: %s", item.Title, trigger.Words(item.Interval()), item.Check, item.Instruction),
		SourceThreadID:   item.SourceThreadID,
		CompletionPolicy: brain.CompletionBounded,
		NextAction:       "On a trigger.fired event: " + item.Instruction,
		WaitFor:          "trigger.fired / trigger.failing / trigger.paused events",
	})
	if err != nil {
		return trigger.Trigger{}, err
	}
	item.WorkID, item.OwnsWork = created.ID, true
	saved, err := a.triggers.Store().Create(item)
	if err != nil {
		_ = a.closeTriggerWork(created.ID, "Trigger was not created: "+err.Error())
		return trigger.Trigger{}, err
	}
	return saved, nil
}

func (a *controlApp) closeTriggerWork(workID, reason string) error {
	item, err := a.brainStore.Work(workID)
	if err != nil {
		return err
	}
	if item.Status == brain.WorkDone || item.Status == brain.WorkCancelled {
		return nil
	}
	_, err = a.brainService.CloseWork(brain.WorkCloseRequest{
		WorkID: workID, ExpectedRevision: item.Revision, Status: brain.WorkCancelled,
		Actor: "trigger", Reason: reason,
	})
	return err
}

func runTriggerCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: mewla trigger <list|get|create|pause|resume|check|delete> [flags]")
		return flag.ErrHelp
	}
	switch args[0] {
	case "list":
		return runCalendarSimple("trigger_list", args[1:], stderr)
	case "get", "pause", "resume", "check", "delete":
		return runTriggerID("trigger_"+args[0], args[1:], stderr)
	case "create":
		return runTriggerCreate(args[1:], stderr)
	default:
		return fmt.Errorf("unknown trigger command: %s", args[0])
	}
}

func runTriggerID(requestType string, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla trigger", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: requestType}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.ID, "id", "", "Trigger id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("Trigger -id is required")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runTriggerCreate(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla trigger create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	item := trigger.Trigger{}
	every := trigger.DefaultInterval
	var timeout time.Duration
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&item.Title, "title", "", "what it watches, one line")
	fs.StringVar(&item.Check, "check", "", "check command: silent when nothing happened, prints the evidence when something did")
	fs.StringVar(&item.Cwd, "cwd", "", "absolute directory the check runs in (default: home)")
	fs.DurationVar(&every, "every", trigger.DefaultInterval, "interval between checks (1m to 24h)")
	fs.DurationVar(&timeout, "timeout", 0, "check timeout (default 1m, or half the interval)")
	fs.StringVar(&item.Instruction, "instruction", "", "what Brain does when it fires")
	fs.StringVar(&item.SourceThreadID, "source-thread", "", "Brain thread the Trigger belongs to (required without -work)")
	fs.StringVar(&item.WorkID, "work", "", "existing Work that receives the firings instead of a new one")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	item.IntervalSeconds = int(every / time.Second)
	item.TimeoutSeconds = int(timeout / time.Second)
	resp, err := callControl(cfg, control.Request{Type: "trigger_create", Trigger: &item})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}
