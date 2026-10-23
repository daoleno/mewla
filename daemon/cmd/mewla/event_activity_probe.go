package main

import (
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// eventActivityProbe answers the watcher's provider-activity question from
// what an agent pushed to the Hub, and reads the transcript (the fallback)
// only for a Worker the agent has not described.
//
// The two sources name activities differently, so a turn must be seen by one
// of them from start to end. The Hub answers only for a Worker whose current
// turn it watched begin; a Worker the daemon met mid-turn (after a restart)
// stays on the fallback until its next turn starts.
type eventActivityProbe struct {
	hub      *agentevents.Hub
	fallback watcher.ProviderActivityProbe
}

func newEventActivityProbe(hub *agentevents.Hub, fallback watcher.ProviderActivityProbe) *eventActivityProbe {
	return &eventActivityProbe{hub: hub, fallback: fallback}
}

func (p *eventActivityProbe) ObserveProviderActivity(worker classifier.Worker, now time.Time) watcher.ProviderActivityObservation {
	if observation, ok := observationFromEvents(p.hub, worker.ID); ok {
		return observation
	}
	if awaitingThread(p.hub, worker, now) {
		return watcher.ProviderActivityObservation{Structured: true, FallbackAllowed: true, ProbeState: watcher.ProbeStateOK}
	}
	return p.fallback.ObserveProviderActivity(worker, now)
}

// threadGrace is how long a Codex that Mewla started may take to announce its
// thread before the transcript is searched for after all.
const threadGrace = 30 * time.Second

// awaitingThread says a Codex that Mewla started has not announced its thread
// yet. Its conversation is named by that announcement, so there is nothing to
// search for in the meantime; the answer is "no turn yet". Past the grace
// period the app-server is taken to be unreachable and the transcript answers.
func awaitingThread(hub *agentevents.Hub, worker classifier.Worker, now time.Time) bool {
	if worker.AgentControl == "" || worker.StartedAt.IsZero() || now.Sub(worker.StartedAt) > threadGrace {
		return false
	}
	if work.InferWorkerProvider(worker.Command, worker.Name) != work.WorkerProviderCodex {
		return false
	}
	state, ok := hub.State(worker.ID)
	return !ok || !state.Bound
}

// ForgetProviderActivity is the watcher saying a Worker has no turn in flight,
// not that it is gone: the Hub keeps the Worker's binding for its next turn
// and is pruned by the event service when the Worker disappears.
func (p *eventActivityProbe) ForgetProviderActivity(workerID string) {
	p.fallback.ForgetProviderActivity(workerID)
}

func observationFromEvents(hub *agentevents.Hub, workerID string) (watcher.ProviderActivityObservation, bool) {
	state, ok := hub.State(workerID)
	if !ok || state.Exited || state.Current == nil || state.Current.StartedAt.IsZero() {
		return watcher.ProviderActivityObservation{}, false
	}
	stream := "events\x00" + state.Binding.ConversationID
	observation := watcher.ProviderActivityObservation{
		ID:              state.Current.ID,
		Status:          string(state.Current.Status),
		StartedAt:       state.Current.StartedAt.UTC(),
		ProgressAt:      state.Current.ProgressAt.UTC(),
		Structured:      true,
		FallbackAllowed: true,
		ProbeState:      watcher.ProbeStateOK,
	}
	if state.Current.Status.Terminal() {
		observation.SettledAt = state.Current.SettledAt.UTC()
	}
	if failure := state.Current.Error; failure != nil && state.Current.Status == agentevents.TurnFailed {
		observation.Error = &watcher.ProviderActivityError{Code: errorCode(failure), HTTPStatus: failure.HTTPStatus, Message: ""}
	}
	if admission := state.Admission; admission != nil {
		observation.AdmissionStream = stream
		observation.AdmissionID = admission.ID
		observation.AdmissionCursor = admission.Cursor
		observation.AdmissionAt = admission.At.UTC()
		observation.InputSHA256 = admission.InputSHA256
		observation.InputUnwrappedSHA256 = admission.InputUnwrappedSHA256
	}
	for _, turn := range state.Terminals {
		observation.TerminalActivities = append(observation.TerminalActivities, watcher.ProviderTerminalActivity{
			ID: turn.ID, Status: string(turn.Status), StartedAt: turn.StartedAt.UTC(), SettledAt: turn.SettledAt.UTC(),
		})
	}
	return observation, true
}

// errorCode names an agent error class with the code the turn-recovery
// supervisor already understands, so its decision rests on the structured
// class and status, never on message text.
func errorCode(failure *agentevents.TurnError) string {
	switch failure.Class {
	case agentevents.ErrorAuth:
		return "authentication_failed"
	case agentevents.ErrorBilling:
		return "billing_error"
	case agentevents.ErrorRateLimit:
		return "rate_limit"
	case agentevents.ErrorOverloaded:
		return "overloaded"
	case agentevents.ErrorInvalidRequest:
		return "invalid_request"
	case agentevents.ErrorContext:
		return "request_too_large"
	case agentevents.ErrorNetwork:
		return "response_stream_disconnected"
	case agentevents.ErrorServer:
		return "server_error"
	case agentevents.ErrorSandbox:
		return "permission_error"
	}
	return strings.TrimSpace(failure.Code)
}
