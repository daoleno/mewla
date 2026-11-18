package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/trigger"
	"github.com/gorilla/websocket"
)

// SetTriggers serves the current computer's Triggers to clients: a snapshot
// on connect and on list_triggers, trigger_changed / trigger_removed as they
// change, and pause, resume, check and delete.
func (s *Server) SetTriggers(engine *trigger.Engine) {
	s.triggers = engine
	if engine != nil {
		s.triggerSubID, s.triggerSub = engine.Store().Subscribe()
	}
}

// triggerWireFirings is how many firings a Trigger carries to clients.
const triggerWireFirings = 10

// triggerWire is a Trigger as clients see it: its newest firings, and the
// count of gathered changes in place of their text.
func triggerWire(view trigger.View) trigger.View {
	if view.Pending != nil {
		view.PendingChanges = len(view.Pending.Outputs)
		view.Pending = nil
	}
	if len(view.Firings) > triggerWireFirings {
		view.Firings = append([]trigger.Firing(nil), view.Firings[len(view.Firings)-triggerWireFirings:]...)
	}
	view.Wakes = nil
	view.LastOutputHash = ""
	return view
}

func (s *Server) triggerViews() []trigger.View {
	views := s.triggers.Views()
	for index := range views {
		views[index] = triggerWire(views[index])
	}
	return views
}

func (s *Server) sendTriggersSnapshot(conn *websocket.Conn, requestID string) {
	if s.triggers == nil {
		s.sendErrorWithRequestID(conn, requestID, "triggers_unavailable", "triggers are not configured")
		return
	}
	s.sendJSON(conn, map[string]any{"type": "triggers_snapshot", "request_id": requestID, "triggers": s.triggerViews()})
}

// handleTriggerRequest is pause_trigger, resume_trigger, check_trigger and
// delete_trigger; each answers trigger_updated (or trigger_removed) with the
// Trigger, and every client also gets the broadcast.
func (s *Server) handleTriggerRequest(conn *websocket.Conn, raw clientMessage) {
	if s.triggers == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "triggers_unavailable", "triggers are not configured")
		return
	}
	id := strings.TrimSpace(raw.ID)
	var err error
	switch raw.Type {
	case "pause_trigger":
		_, err = s.triggers.Pause(id)
	case "resume_trigger":
		_, err = s.triggers.Resume(id)
	case "check_trigger":
		ctx, cancel := context.WithTimeout(context.Background(), trigger.MaxTimeout+time.Minute)
		_, _, err = s.triggers.CheckNow(ctx, id)
		cancel()
	case "delete_trigger":
		if s.deleteTrigger != nil {
			err = s.deleteTrigger(id)
		} else {
			_, err = s.triggers.Store().Delete(id)
		}
		if err == nil {
			s.sendJSON(conn, map[string]any{"type": "trigger_removed", "request_id": raw.RequestID, "trigger_id": id})
			return
		}
	}
	if err != nil {
		s.sendTriggerError(conn, raw.RequestID, err)
		return
	}
	view, err := s.triggers.View(id)
	if err != nil {
		s.sendTriggerError(conn, raw.RequestID, err)
		return
	}
	s.sendJSON(conn, map[string]any{"type": "trigger_updated", "request_id": raw.RequestID, "trigger": triggerWire(view)})
}

// SetTriggerDelete names how a client's delete runs (the daemon also closes
// the Work the Trigger created).
func (s *Server) SetTriggerDelete(remove func(id string) error) { s.deleteTrigger = remove }

func (s *Server) sendTriggerError(conn *websocket.Conn, requestID string, err error) {
	code := "trigger_request_failed"
	switch {
	case errors.Is(err, trigger.ErrNotFound):
		code = "trigger_not_found"
	case errors.Is(err, trigger.ErrInvalid):
		code = "invalid_trigger"
	case errors.Is(err, trigger.ErrBusy):
		code = "already_running"
	}
	s.sendErrorWithRequestID(conn, requestID, code, err.Error())
}

func (s *Server) handleTriggerEvent(item trigger.Trigger) {
	if item.Revision == 0 {
		s.broadcastJSON(map[string]any{"type": "trigger_removed", "trigger_id": item.ID})
		return
	}
	view, err := s.triggers.View(item.ID)
	if err != nil {
		return
	}
	s.broadcastJSON(map[string]any{"type": "trigger_changed", "trigger": triggerWire(view)})
}
