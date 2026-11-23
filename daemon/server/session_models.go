package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// AgentModel is one model an agent offers a Session, in the agent's own
// words: its id, its name and the reasoning levels it takes.
type AgentModel struct {
	ID            string
	Name          string
	Efforts       []string
	DefaultEffort string
}

// AgentModels is an agent's own model menu for one Session: what the agent
// can use with the person's sign-in, and what the Session runs on now.
type AgentModels struct {
	// Label names where the models come from ("Codex", a DSH provider).
	Label string
	// Current is the id of the model the Session runs on; never empty.
	Current string
	// Effort is the Session's reasoning level ("" = the model's default).
	Effort string
	Models []AgentModel
	// Unsaved is set when the agent took a switch but keeps it later.
	Unsaved string
}

// SessionModels reaches the agent of a Session for its own models. Each agent
// that can list and change its models has one (cmd/mewla installs them); a
// Session on a Provider connection keeps the Provider path.
type SessionModels interface {
	Models(ctx context.Context, worker *classifier.Worker) (AgentModels, error)
	// SetModel switches the Session the agent's own way and answers the menu
	// after it. A refusal carries the agent's reason.
	SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (AgentModels, error)
}

// SetSessionModels installs the model menu of one agent (a work.WorkerProvider*).
func (s *Server) SetSessionModels(provider string, models SessionModels) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionModels == nil {
		s.sessionModels = map[string]SessionModels{}
	}
	if models == nil {
		delete(s.sessionModels, provider)
		return
	}
	s.sessionModels[provider] = models
}

// sessionModelsProvider is the agent whose own model menu serves a Session:
// "" when its agent has none, or when the Session runs on a Provider route.
func (s *Server) sessionModelsProvider(worker *classifier.Worker) (string, SessionModels) {
	if worker == nil {
		return "", nil
	}
	provider := ""
	switch {
	case work.IsAntigravitySessionCommand(worker.Command):
		provider = work.WorkerProviderAntigravity
	case work.DSHSessionID(worker.Command) != "":
		provider = work.WorkerProviderDSH
	default:
		if _, provider = s.workerExecutor(worker); provider == "" {
			provider = work.InferWorkerProvider(worker.Command)
		}
	}
	s.mu.Lock()
	models := s.sessionModels[provider]
	s.mu.Unlock()
	if models == nil {
		return "", nil
	}
	if owner := s.modelProfiles(); owner != nil {
		if _, routed := owner.ThreadRuntime(worker.ID); routed {
			return "", nil
		}
	}
	return provider, models
}

const sessionModelsBudget = 30 * time.Second

// handleSessionModels answers the Model sheet of a Session from its agent:
// the models the agent offers, each with its reasoning levels, the current
// one checked. It reports false when the Session is not served this way.
func (s *Server) handleSessionModels(conn *websocket.Conn, raw clientMessage, mutation bool) bool {
	workerID := raw.WorkerID
	if workerID == "" {
		workerID = raw.SessionID
	}
	worker := s.lookupWorker(workerID)
	provider, source := s.sessionModelsProvider(worker)
	if source == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionModelsBudget)
	defer cancel()
	var (
		models AgentModels
		err    error
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
		models, err = source.SetModel(ctx, worker, strings.TrimSpace(raw.Runtime.ModelID), effort)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "runtime_switch_failed", err.Error())
			return true
		}
	} else if models, err = source.Models(ctx, worker); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "binding_not_found", err.Error())
		return true
	}
	if strings.TrimSpace(models.Current) == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "binding_not_found", errNoCurrentModel.Error())
		return true
	}
	messageType := "thread_runtime"
	if mutation {
		messageType = "thread_runtime_set"
	}
	payload := map[string]any{"type": messageType, "request_id": raw.RequestID, "worker_id": workerID, "runtime": agentRuntimeSelection(workerID, provider, models)}
	if mutation {
		payload["persistence_outcome"] = "applied"
		payload["persistence_durable"] = models.Unsaved == ""
		if models.Unsaved != "" {
			payload["persistence_warning"] = models.Unsaved
		}
	}
	s.sendJSON(conn, payload)
	return true
}

var errNoCurrentModel = errors.New("the agent did not say which model this Session runs on")

// agentRuntimeSelection is the thread_runtime wire of an agent's own menu:
// one connection (the agent), its models as native_models.
func agentRuntimeSelection(workerID, provider string, models AgentModels) map[string]any {
	label := strings.TrimSpace(models.Label)
	if label == "" {
		label = work.AgentProductName(provider, "")
	}
	if label == "" {
		label = provider
	}
	choices := []map[string]any{}
	efforts, defaultEffort := []string{}, ""
	for _, model := range models.Models {
		choice := map[string]any{"id": model.ID, "available": true, "source": provider + "_native", "reasoning_efforts": nonNilStrings(model.Efforts)}
		if name := strings.TrimSpace(model.Name); name != "" {
			choice["display_name"] = name
		}
		if model.DefaultEffort != "" {
			choice["reasoning_effort_default"] = model.DefaultEffort
		}
		choices = append(choices, choice)
		if model.ID == models.Current {
			efforts, defaultEffort = nonNilStrings(model.Efforts), model.DefaultEffort
		}
	}
	return map[string]any{
		"session_id": workerID, "client": provider, "connection_id": provider, "connection_name": label,
		"provider_label": label, "model_id": models.Current, "reasoning_effort": models.Effort,
		"reasoning_efforts": efforts, "reasoning_effort_default": defaultEffort,
		"credential_ready": true, "hot_switchable": true, "native_models": choices,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
