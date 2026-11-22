package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// AntigravitySessions reaches the Antigravity Sessions Mewla runs
// (cmd/mewla/agy_events.go): the models agy offers a Session and a change of
// them.
type AntigravitySessions interface {
	Models(ctx context.Context, workerID string) (json.RawMessage, error)
	SetModel(ctx context.Context, workerID, model, effort, mode string) (json.RawMessage, error)
}

// SetAntigravitySessions installs the link to the Antigravity Sessions.
func (s *Server) SetAntigravitySessions(sessions AntigravitySessions) {
	s.mu.Lock()
	s.agySessions = sessions
	s.mu.Unlock()
}

// agyModels is what a Session reports (work.antigravityModels).
type agyModels struct {
	Current   string `json:"current"`
	Effort    string `json:"effort"`
	ModelBase string `json:"model_base"`
	Efforts   []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"efforts"`
	Bases []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"bases"`
	Models []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"models"`
}

// handleAgyThreadRuntime answers the Model sheet of an Antigravity Session
// from agy itself: its model families, each with agy's thinking levels, the
// current one checked. A change goes to agy at once and is kept for every
// later start of the Session.
func (s *Server) handleAgyThreadRuntime(conn *websocket.Conn, raw clientMessage, mutation bool) bool {
	workerID := raw.WorkerID
	if workerID == "" {
		workerID = raw.SessionID
	}
	worker := s.lookupWorker(workerID)
	if worker == nil || !work.IsAntigravitySessionCommand(worker.Command) {
		return false
	}
	s.mu.Lock()
	sessions := s.agySessions
	s.mu.Unlock()
	if sessions == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "binding_not_found", "This Antigravity Session is not connected to Mewla.")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var (
		rawModels json.RawMessage
		err       error
	)
	if mutation {
		if raw.Runtime == nil || strings.TrimSpace(raw.Runtime.ModelID) == "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "profile_invalid", "runtime is required")
			return true
		}
		effort := ""
		if !raw.Runtime.UseDefaultEffect {
			effort = strings.TrimSpace(raw.Runtime.Effect)
		}
		rawModels, err = sessions.SetModel(ctx, workerID, strings.TrimSpace(raw.Runtime.ModelID), effort, "")
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "runtime_switch_failed", err.Error())
			return true
		}
	} else if rawModels, err = sessions.Models(ctx, workerID); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "binding_not_found", err.Error())
		return true
	}
	var models agyModels
	_ = json.Unmarshal(rawModels, &models)
	selection := agyRuntimeSelection(workerID, models)
	messageType := "thread_runtime"
	if mutation {
		messageType = "thread_runtime_set"
	}
	payload := map[string]any{"type": messageType, "request_id": raw.RequestID, "worker_id": workerID, "runtime": selection}
	if mutation {
		payload["persistence_outcome"] = "applied"
		payload["persistence_durable"] = true
	}
	s.sendJSON(conn, payload)
	return true
}

func agyRuntimeSelection(workerID string, models agyModels) map[string]any {
	efforts := []string{}
	for _, effort := range models.Efforts {
		efforts = append(efforts, effort.ID)
	}
	choices := []map[string]any{}
	current := models.ModelBase
	for _, base := range models.Bases {
		choices = append(choices, map[string]any{"id": base.ID, "display_name": base.Name, "available": true, "source": "antigravity_native", "reasoning_efforts": efforts})
	}
	if len(choices) == 0 {
		// An agy without model families: its full model ids.
		current = models.Current
		for _, model := range models.Models {
			choices = append(choices, map[string]any{"id": model.ID, "display_name": model.Name, "available": true, "source": "antigravity_native"})
		}
		efforts = []string{}
	}
	return map[string]any{
		"session_id": workerID, "client": "antigravity", "connection_id": "antigravity", "connection_name": "Antigravity",
		"provider_label": "Antigravity", "model_id": current, "reasoning_effort": models.Effort, "reasoning_efforts": efforts,
		"credential_ready": true, "hot_switchable": true, "native_models": choices,
	}
}
