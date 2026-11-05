package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/work"
)

func TestCalendarControlToolsCRUDAndResolvedTimeConfirmation(t *testing.T) {
	store, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{calendarStore: store}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	notify := time.Date(2026, time.July, 15, 9, 30, 0, 0, loc)
	resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: &calendar.Item{Title: "提交周报", Kind: calendar.KindReminder, NotifyAt: &notify, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceWeekdays, SourceThreadID: "brain-thread-1"}})
	if !resp.OK || resp.CalendarItem == nil {
		t.Fatalf("create = %#v", resp)
	}
	for _, want := range []string{"2026-07-15 09:30:00 CST", "Asia/Shanghai", "Mewla will notify you"} {
		if !strings.Contains(resp.Confirmation, want) {
			t.Fatalf("confirmation %q missing %q", resp.Confirmation, want)
		}
	}
	created := *resp.CalendarItem
	got := app.HandleControlRequest(control.Request{Type: "calendar_get", ID: created.ID})
	if !got.OK || got.CalendarItem.SourceThreadID != "brain-thread-1" {
		t.Fatalf("get = %#v", got)
	}
	created.Title = "提交项目周报"
	updated := app.HandleControlRequest(control.Request{Type: "calendar_update", Revision: created.Revision, CalendarItem: &created})
	if !updated.OK || updated.CalendarItem.Title != "提交项目周报" {
		t.Fatalf("update = %#v", updated)
	}
	conflict := app.HandleControlRequest(control.Request{Type: "calendar_update", Revision: created.Revision, CalendarItem: &created})
	if conflict.OK || conflict.Error.Code != "conflict" {
		t.Fatalf("conflict = %#v", conflict)
	}
	listed := app.HandleControlRequest(control.Request{Type: "calendar_list"})
	if len(listed.CalendarItems) != 1 {
		t.Fatalf("list = %#v", listed)
	}
	cancelled := app.HandleControlRequest(control.Request{Type: "calendar_cancel", ID: created.ID, Revision: updated.CalendarItem.Revision})
	if !cancelled.OK || cancelled.CalendarItem.Status != calendar.StatusCancelled {
		t.Fatalf("cancel = %#v", cancelled)
	}
	if !strings.Contains(cancelled.Confirmation, "will not act on it") {
		t.Fatalf("cancel confirmation = %q", cancelled.Confirmation)
	}
}

type calendarControlRunner struct{}

func (calendarControlRunner) RunScheduledAction(context.Context, calendar.Item, calendar.Run) (calendar.ActionResult, error) {
	return calendar.ActionResult{WorkID: "work-1", WorkerSession: "agent-1", Launched: true}, nil
}

func TestCalendarRunControlConfirmationDescribesLaunchNotCompletion(t *testing.T) {
	store, _ := calendar.NewStore(t.TempDir())
	loc, _ := time.LoadLocation("Asia/Shanghai")
	due := time.Now().In(loc).Add(time.Hour)
	item, err := store.Create(calendar.Item{Title: "Prepare report", Kind: calendar.KindScheduledAction, DueAt: &due, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone, ActionInstruction: "Write the report", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{calendarStore: store, calendarScheduler: calendar.NewScheduler(store, calendarControlRunner{})}
	resp := app.HandleControlRequest(control.Request{Type: "calendar_run", ID: item.ID})
	if !resp.OK || resp.CalendarItem.Status != calendar.StatusRunning {
		t.Fatalf("run = %#v", resp)
	}
	for _, want := range []string{"Asia/Shanghai", "launched visible Work", "reconcile it to completed or failed"} {
		if !strings.Contains(resp.Confirmation, want) {
			t.Fatalf("confirmation %q missing %q", resp.Confirmation, want)
		}
	}
	if strings.Contains(strings.ToLower(resp.Confirmation), "completed") && !strings.Contains(resp.Confirmation, "completed or failed") {
		t.Fatalf("confirmation reports false completion: %q", resp.Confirmation)
	}
}

func TestCalendarControlToolRejectsAmbiguousOrMismatchedTime(t *testing.T) {
	store, _ := calendar.NewStore(t.TempDir())
	app := &controlApp{calendarStore: store}
	wrong := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)
	resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: &calendar.Item{Title: "Bad", Kind: calendar.KindDeadline, DueAt: &wrong, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone}})
	if resp.OK || resp.Error == nil || !strings.Contains(resp.Error.Message, "offset does not match") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestCalendarSaveResolvesWorkerRouteOnEveryPath(t *testing.T) {
	store, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execs := work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude --model claude-opus-5-5"},
		"codex":  {Name: "codex", Command: "codex"},
	})
	store.SetRouteCheck(func(item calendar.Item) error {
		_, _, err := execs.ResolveScheduledRoute(work.LaunchRoute{Executor: item.ActionExecutor, Model: item.ActionModel, Reasoning: item.ActionReasoning})
		return err
	})
	app := &controlApp{calendarStore: store}
	due := time.Now().UTC().Add(time.Hour)
	item := func(executor, model, reasoning string) *calendar.Item {
		return &calendar.Item{Title: "Routed", Kind: calendar.KindScheduledAction, DueAt: &due, Timezone: "UTC", Recurrence: calendar.RecurrenceNone,
			ActionInstruction: "Say hi", SourceThreadID: "thread-1", ActionExecutor: executor, ActionModel: model, ActionReasoning: reasoning}
	}
	// No item route and no default: refused on the control path and on the
	// store call the phone WebSocket handlers make.
	if resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: item("", "", "")}); resp.OK || resp.Error == nil || !strings.Contains(resp.Error.Message, "no Worker route") {
		t.Fatalf("route-less create without a default: %#v", resp)
	}
	if _, err := store.Create(*item("", "", "")); !errors.Is(err, work.ErrNoScheduledRoute) {
		t.Fatalf("store.Create route-less without a default: %v", err)
	}
	execs.ScheduledAction = work.LaunchRoute{Executor: "claude", Model: "claude-sonnet-5-5"}
	for _, bad := range []*calendar.Item{item("nope", "", ""), item("claude", "", "ultra"), item("claude", "bad\nmodel", "")} {
		if resp := app.HandleControlRequest(control.Request{Type: "calendar_create", CalendarItem: bad}); resp.OK {
			t.Fatalf("control saved %q/%q/%q: %#v", bad.ActionExecutor, bad.ActionModel, bad.ActionReasoning, resp)
		}
		if _, err := store.Create(*bad); err == nil {
			t.Fatalf("store saved %q/%q/%q", bad.ActionExecutor, bad.ActionModel, bad.ActionReasoning)
		}
	}
	if len(store.List()) != 0 {
		t.Fatalf("a refused item was saved: %#v", store.List())
	}
	// A phone-created item names no route and uses the default.
	phone, err := store.Create(*item("", "", ""))
	if err != nil || phone.ActionExecutor != "" {
		t.Fatalf("phone create = %#v, %v", phone, err)
	}
	// A phone edit that sends the item back keeps an explicit route, and one
	// to an unknown executor is refused.
	explicit, err := store.Create(*item("codex", "gpt-6-astra", "max"))
	if err != nil {
		t.Fatal(err)
	}
	edit := explicit
	edit.Title = "Renamed on the phone"
	edited, err := store.Update(edit, explicit.Revision)
	if err != nil || edited.ActionExecutor != "codex" || edited.ActionModel != "gpt-6-astra" || edited.ActionReasoning != "max" {
		t.Fatalf("phone edit lost the route: %#v, %v", edited, err)
	}
	bad := edited
	bad.ActionExecutor = "nope"
	if _, err := store.Update(bad, edited.Revision); err == nil {
		t.Fatal("update to an unknown executor saved")
	}
}
