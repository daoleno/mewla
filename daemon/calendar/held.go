package calendar

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// A scheduled action whose agent is refused by its model provider (a rate
// limit, an overload, a dropped network) has not failed: it is held.
//
//   - While its Session lives, the run stays running and is continued in that
//     Session (the agent's own "continue" prompt) once the provider answers or
//     the family's probe is this run's (watcher/provider_availability.go). The
//     agent sees what it already did; nothing is replayed blindly.
//   - If its Session ends while held, the run starts a new agent only when it
//     did no work: every provider Activity the agent ever ran was refused. An
//     agent that had an Activity answered may have acted, so the run fails
//     with that reason instead of running twice.
//   - A held run is made up only within heldRunWindow of its scheduled time,
//     and never past the item's next occurrence: a daily briefing three days
//     late is worthless. After that it fails, saying the provider was
//     unavailable.
//   - Run now on a held run replaces it with a fresh run at once.

// heldRunWindow bounds how late a held scheduled action may still run.
const heldRunWindow = 12 * time.Hour

// RunHold is a run waiting for its agent's model provider to answer.
type RunHold struct {
	// Since is when the provider first refused this run's agent.
	Since time.Time `json:"since"`
	// Provider is the agent family and Kind the refusal in two words
	// (watcher.ProviderOutage).
	Provider string `json:"provider,omitempty"`
	Kind     string `json:"kind,omitempty"`
	// Refused lists the agent's provider Activities that were refused.
	Refused []string `json:"refused,omitempty"`
	// NoWork: every Activity the agent ran was refused, so it did nothing.
	NoWork bool `json:"no_work"`
	// Continued is the refused Activity the run was last continued after.
	Continued string `json:"continued,omitempty"`
}

// ProviderRefusal is what a runner observed of a run's agent being refused.
type ProviderRefusal struct {
	ActivityID string
	Provider   string
	Kind       string
	At         time.Time
	// Activities are every settled provider Activity the agent ran.
	Activities []string
}

// ActionHolder is implemented by a runner that can tell a run whose agent was
// refused by its model provider, and continue it when the provider answers.
type ActionHolder interface {
	// ScheduledActionRefused reports whether the run's Session lives and
	// whether its agent's current provider Activity was refused (transient).
	ScheduledActionRefused(item Item, run Run) (refusal ProviderRefusal, alive, refused bool)
	// ProviderMayAnswer is true while the family's provider is available, and
	// once per probe interval while it is not (the probe is then this run's).
	ProviderMayAnswer(provider string, now time.Time) bool
	// ContinueScheduledAction asks the run's agent to continue where it stopped.
	ContinueScheduledAction(item Item, run Run) error
}

// heldDeadline is the latest a held run may still run.
func heldDeadline(item Item, run Run) time.Time {
	deadline := run.ScheduledFor.Add(heldRunWindow)
	if item.Recurrence != RecurrenceNone {
		next := cloneItem(item)
		if advanceItem(&next) && next.NextAt.Before(deadline) {
			deadline = next.NextAt
		}
	}
	return deadline
}

func heldFailure(item Item, hold RunHold, deadline time.Time) string {
	loc := mustLocation(item.Timezone)
	kind := hold.Kind
	if kind == "" {
		kind = "unavailable"
	}
	who := "The model provider"
	if hold.Provider != "" {
		who = providerName(hold.Provider) + "'s model provider"
	}
	return fmt.Sprintf("Not run: %s refused requests (%s) from %s until %s, the latest this run could still be useful.",
		who, kind, hold.Since.In(loc).Format("Jan 2 15:04"), deadline.In(loc).Format("Jan 2 15:04"))
}

func providerName(provider string) string {
	switch provider {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "pi":
		return "Pi"
	case "opencode":
		return "OpenCode"
	case "":
		return "The agent"
	default:
		return strings.ToUpper(provider[:1]) + provider[1:]
	}
}

// nextHold folds one refusal into the run's hold.
func nextHold(previous *RunHold, refusal ProviderRefusal) RunHold {
	hold := RunHold{Since: refusal.At, Provider: refusal.Provider, Kind: refusal.Kind, NoWork: true}
	if previous != nil {
		hold = *previous
		hold.Refused = slices.Clone(previous.Refused)
		hold.Provider, hold.Kind = refusal.Provider, refusal.Kind
		if refusal.At.Before(hold.Since) || hold.Since.IsZero() {
			hold.Since = refusal.At
		}
	}
	if refusal.ActivityID != "" && !slices.Contains(hold.Refused, refusal.ActivityID) {
		hold.Refused = append(hold.Refused, refusal.ActivityID)
	}
	if hold.NoWork {
		for _, activity := range refusal.Activities {
			if !slices.Contains(hold.Refused, activity) {
				hold.NoWork = false // an Activity was answered: the agent may have acted
				break
			}
		}
	}
	return hold
}

func sameHold(a, b *RunHold) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Since.Equal(b.Since) && a.Provider == b.Provider && a.Kind == b.Kind && a.NoWork == b.NoWork &&
		a.Continued == b.Continued && slices.Equal(a.Refused, b.Refused)
}

// reconcileHeld handles a running action whose agent is or was refused by its
// provider. It reports whether it decided the run's state for this pass.
func (s *Scheduler) reconcileHeld(ctx context.Context, holder ActionHolder, item Item, run Run) bool {
	now := s.now()
	refusal, alive, refused := holder.ScheduledActionRefused(item, run)
	switch {
	case alive && !refused:
		if run.Held != nil {
			// The provider answered the agent: it is running again.
			_, err := s.store.SetRunHold(item.ID, run.ID, nil)
			s.note("calendar clear-hold "+item.ID, err)
		}
		return false
	case !alive && run.Held == nil:
		return false
	}
	hold := RunHold{}
	if run.Held != nil {
		hold = *run.Held
	}
	if refused {
		hold = nextHold(run.Held, refusal)
	}
	if deadline := heldDeadline(item, run); !now.Before(deadline) {
		s.finish(item, run, "", heldFailure(item, hold, deadline))
		return true
	}
	if refused {
		if !sameHold(run.Held, &hold) {
			if run.Held == nil {
				log.Printf("calendar: %s held: its agent was refused by the provider (%s); it continues once the provider answers", item.ID, hold.Kind)
			}
			if _, err := s.store.SetRunHold(item.ID, run.ID, &hold); err != nil {
				s.note("calendar set-hold "+item.ID, err)
				return true
			}
		}
		if hold.Continued == refusal.ActivityID || !holder.ProviderMayAnswer(hold.Provider, now) {
			return true
		}
		if err := holder.ContinueScheduledAction(item, run); err != nil {
			log.Printf("WARN calendar: continue held %s: %v", item.ID, err)
			return true
		}
		hold.Continued = refusal.ActivityID
		_, err := s.store.SetRunHold(item.ID, run.ID, &hold)
		s.note("calendar set-hold "+item.ID, err)
		return true
	}
	// The Session ended while the run was held.
	if !hold.NoWork {
		s.finish(item, run, "", "Its agent's Session ended while its model provider was refusing requests, after the agent had started working. It was not started again, so it would not run twice.")
		return true
	}
	if !holder.ProviderMayAnswer(hold.Provider, now) {
		return true
	}
	if !s.beginLaunch(item.ID) {
		return true
	}
	defer s.endLaunch(item.ID)
	result, err := s.runner.RunScheduledAction(ctx, item, run)
	if err != nil && !result.Launched {
		log.Printf("WARN calendar: start held %s again: %v", item.ID, err)
		return true
	}
	if _, err := s.store.RecordLaunch(item.ID, run.ID, result.WorkID, result.WorkerSession); err != nil {
		s.note("calendar record-relaunch "+item.ID, err)
		return true
	}
	log.Printf("calendar: %s started again in %s: its first agent was refused by the provider before doing any work", item.ID, result.WorkerSession)
	return true
}

// heldActive returns the running run of a running item when it is held.
func heldActive(item Item) (Run, bool) {
	if item.Status != StatusRunning {
		return Run{}, false
	}
	for index := len(item.Runs) - 1; index >= 0; index-- {
		if item.Runs[index].Status == StatusRunning {
			return item.Runs[index], item.Runs[index].Held != nil
		}
	}
	return Run{}, false
}

// providerEvidence, providerAvailability and sessionInput are the watcher's
// capabilities a WorkRunner uses for held runs when its Watcher has them.
type providerEvidence interface {
	ProbeProviderEvidence(sessionID string) (watcher.ProviderActivityObservation, bool, error)
}

type providerAvailability interface {
	ProviderOutage(provider string) (watcher.ProviderOutage, bool)
	ProviderProbe(provider string, now time.Time) (bool, time.Time)
}

type sessionInput interface {
	SendInput(sessionID, text string) error
}

// ScheduledActionRefused implements ActionHolder from the agent's own
// structured provider record: its current Activity settled with a transient
// provider error (watcher.ClassifyProviderError).
func (r *WorkRunner) ScheduledActionRefused(_ Item, run Run) (ProviderRefusal, bool, bool) {
	session := strings.TrimSpace(run.WorkerSession)
	if r == nil || r.Watcher == nil || session == "" {
		return ProviderRefusal{}, false, false
	}
	worker := r.Watcher.GetWorker(session)
	if worker == nil {
		return ProviderRefusal{}, r.Watcher.HasSession(session), false
	}
	probe, ok := r.Watcher.(providerEvidence)
	if !ok {
		return ProviderRefusal{}, true, false
	}
	observation, _, err := probe.ProbeProviderEvidence(session)
	if err != nil || observation.ProbeState != watcher.ProbeStateOK || observation.Error == nil ||
		observation.SettledAt.IsZero() || watcher.ClassifyProviderError(*observation.Error) != watcher.ProviderErrorTransient {
		return ProviderRefusal{}, true, false
	}
	refusal := ProviderRefusal{
		ActivityID: strings.TrimSpace(observation.ID),
		Provider:   watcher.ProviderFamily(worker.Command),
		Kind:       watcher.ProviderRefusalKind(*observation.Error),
		At:         observation.SettledAt,
	}
	for _, activity := range observation.TerminalActivities {
		if id := strings.TrimSpace(activity.ID); id != "" {
			refusal.Activities = append(refusal.Activities, id)
		}
	}
	return refusal, true, true
}

// ProviderMayAnswer implements ActionHolder.
func (r *WorkRunner) ProviderMayAnswer(provider string, now time.Time) bool {
	availability, ok := r.Watcher.(providerAvailability)
	if !ok {
		return true
	}
	granted, _ := availability.ProviderProbe(provider, now)
	return granted
}

// ContinueScheduledAction implements ActionHolder: the agent's own
// continuation prompt, submitted in its live Session (the trailing newline is
// the submit; without it the prompt is only typed).
func (r *WorkRunner) ContinueScheduledAction(_ Item, run Run) error {
	session := strings.TrimSpace(run.WorkerSession)
	input, ok := r.Watcher.(sessionInput)
	if !ok || session == "" {
		return fmt.Errorf("this Watcher cannot type into a Session")
	}
	worker := r.Watcher.GetWorker(session)
	if worker == nil {
		return fmt.Errorf("Session %s is gone", session)
	}
	return input.SendInput(session, watcher.TurnContinuationPrompt+"\n")
}
