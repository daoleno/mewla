package server

import (
	"context"
	"errors"
	"strings"

	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/gorilla/websocket"
)

// calendarItemWire is a calendar item as lists, broadcasts and mutation
// replies carry it: its newest runs, each with a one-line preview in place of
// its result. Every run keeps its full result, so a daily item grows by one
// deliverable a day; get_calendar_item still returns every run in full.
func calendarItemWire(item calendar.Item) calendar.Item {
	item = calendar.WithoutStaleFailure(item)
	runs := item.Runs
	if len(runs) > calendarWireRuns {
		runs = runs[len(runs)-calendarWireRuns:]
	}
	item.Runs = make([]calendar.Run, len(runs))
	for index, run := range runs {
		run.ResultPreview = calendar.ResultPreview(run.Result)
		run.Result = ""
		item.Runs[index] = run
	}
	if len(item.Runs) == 0 {
		item.Runs = nil
	}
	return item
}

// calendarWireRuns is how many runs a list or broadcast carries per item.
const calendarWireRuns = 10

func (s *Server) sendCalendarSnapshot(conn *websocket.Conn, requestID string) {
	if s.calendar == nil {
		s.sendErrorWithRequestID(conn, requestID, "calendar_unavailable", "calendar store not configured")
		return
	}
	items := s.calendar.List()
	for i := range items {
		items[i] = calendarItemWire(items[i])
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_items_snapshot", "request_id": requestID, "calendar_items": items})
}
func (s *Server) handleGetCalendarItem(conn *websocket.Conn, raw clientMessage) {
	if s.calendar == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "calendar_unavailable", "calendar store not configured")
		return
	}
	item, err := s.calendar.Get(strings.TrimSpace(raw.ID))
	if err != nil {
		s.sendCalendarError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_item", "request_id": raw.RequestID, "calendar_item": calendar.WithoutStaleFailure(item)})
}
func (s *Server) handleCreateCalendarItem(conn *websocket.Conn, raw clientMessage) {
	if s.calendar == nil || raw.CalendarItem == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_calendar_item", "calendar item is required")
		return
	}
	item, err := s.calendar.Create(*raw.CalendarItem)
	if err != nil {
		s.sendCalendarError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_item_created", "request_id": raw.RequestID, "calendar_item": calendarItemWire(item)})
}
func (s *Server) handleUpdateCalendarItem(conn *websocket.Conn, raw clientMessage) {
	if s.calendar == nil || raw.CalendarItem == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_calendar_item", "calendar item is required")
		return
	}
	item, err := s.calendar.Update(*raw.CalendarItem, raw.Revision)
	if err != nil {
		s.sendCalendarError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_item_updated", "request_id": raw.RequestID, "calendar_item": calendarItemWire(item)})
}
func (s *Server) handleCancelCalendarItem(conn *websocket.Conn, raw clientMessage) {
	if s.calendar == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "calendar_unavailable", "calendar store not configured")
		return
	}
	item, err := s.calendar.Cancel(strings.TrimSpace(raw.ID), raw.Revision)
	if err != nil {
		s.sendCalendarError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_item_cancelled", "request_id": raw.RequestID, "calendar_item": calendarItemWire(item)})
}
func (s *Server) handleRunCalendarItem(conn *websocket.Conn, raw clientMessage) {
	if s.calendarScheduler == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "calendar_unavailable", "calendar scheduler not configured")
		return
	}
	item, err := s.calendarScheduler.RunNow(context.Background(), strings.TrimSpace(raw.ID))
	if err != nil {
		s.sendCalendarError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "calendar_item_running", "request_id": raw.RequestID, "calendar_item": calendarItemWire(item)})
}
func (s *Server) sendCalendarError(conn *websocket.Conn, requestID string, err error) {
	code := "calendar_request_failed"
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		code = "calendar_not_found"
	case errors.Is(err, calendar.ErrConflict):
		code = "conflict"
	case errors.Is(err, calendar.ErrClaimed):
		code = "already_running"
	}
	s.sendErrorWithRequestID(conn, requestID, code, err.Error())
}
