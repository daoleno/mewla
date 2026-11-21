package main

import (
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// workProviderActivityProbe is a thin bridge to daemon/work's authoritative
// provider-native Activity parsers. Watcher calls it only after finding a
// nonterminal accepted delegated-turn marker.
type workProviderActivityProbe struct {
	mu      sync.Mutex
	readers map[string]*providerActivityReader
	// lost is the last loss reported per Worker, so a source that stays lost
	// is logged once, and logged again when it is lost for another reason.
	lost map[string]string
	// progress is the status a Session's program reported about itself.
	progress func(sessionID string) (state string, at time.Time, ok bool)
}

// SetProgressSource gives the probe the Session host's program status, which is
// the Activity of an agent that reports its own turns (agy).
func (p *workProviderActivityProbe) SetProgressSource(progress func(sessionID string) (string, time.Time, bool)) {
	p.mu.Lock()
	p.progress = progress
	p.mu.Unlock()
}

type providerActivityReader struct {
	mu     sync.Mutex
	reader *work.ProviderConversationReader
}

func newWorkProviderActivityProbe() *workProviderActivityProbe {
	return &workProviderActivityProbe{
		readers: make(map[string]*providerActivityReader),
	}
}

func (p *workProviderActivityProbe) ObserveProviderActivity(
	worker classifier.Worker,
	now time.Time,
) watcher.ProviderActivityObservation {
	provider := work.InferWorkerProvider(worker.Command, worker.Name)
	if provider == "" {
		return watcher.ProviderActivityObservation{FallbackAllowed: true}
	}

	p.mu.Lock()
	reader := p.readers[worker.ID]
	if reader == nil {
		reader = &providerActivityReader{reader: work.NewProviderConversationReader()}
		p.readers[worker.ID] = reader
	}
	p.mu.Unlock()

	reader.mu.Lock()
	defer reader.mu.Unlock()
	conversation, err := reader.reader.Load(worker, provider, now)
	if err != nil {
		// Channel health: a failed read is a bounded evidence loss — the
		// transcript is provably unlocatable (missing file) or unreadable
		// (stat/open/parse/sqlite failure). "Read successfully, no new
		// fact" is never a loss.
		state := probeStateForConversation(work.CodexConversation{}, err)
		p.noteLoss(worker, provider, state, err.Error())
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
			ProbeState:      state,
		}
	}
	if !conversation.Available {
		// The reader succeeded but the source is not available: distinguish
		// the declared non-structured shapes (healthy no-fact) from a
		// provably unlocatable/unreadable transcript. The Pi reader fails
		// closed on a malformed header at the exact owned --session path; the
		// file existing proves the source is unreadable, not unlocatable.
		state := probeStateForConversation(conversation, nil)
		if state == watcher.ProbeStateUnlocatable && provider == work.WorkerProviderPi {
			if ownedPath := work.PiOwnedSessionPath(worker.Command); ownedPath != "" {
				if info, statErr := os.Stat(ownedPath); statErr == nil && !info.IsDir() {
					state = watcher.ProbeStateUnreadable
				}
			}
		}
		p.noteLoss(worker, provider, state, conversation.Reason)
		return watcher.ProviderActivityObservation{
			Structured:      true,
			FallbackAllowed: true,
			ProbeState:      state,
		}
	}
	p.noteLoss(worker, provider, watcher.ProbeStateOK, "")
	if provider == work.WorkerProviderAntigravity {
		p.mu.Lock()
		progress := p.progress
		p.mu.Unlock()
		if progress != nil {
			if state, at, ok := progress(worker.ID); ok {
				work.ApplyAntigravityProgress(&conversation, state, at)
			}
		}
	}
	observation := watcher.ProviderActivityObservation{
		Structured:      true,
		FallbackAllowed: true,
		ProbeState:      watcher.ProbeStateOK,
	}
	observation.CompactedAt = parseProviderActivityTime(conversation.CompactedAt)
	observation.CompactFailedAt = parseProviderActivityTime(conversation.CompactFailedAt)
	observation.CompactFailure = conversation.CompactFailure
	if conversation.Activity != nil {
		observation.ID = strings.TrimSpace(conversation.Activity.ID)
		observation.Status = string(conversation.Activity.Status)
		observation.StartedAt = parseProviderActivityTime(conversation.Activity.StartedAt)
		observation.SettledAt = parseProviderActivityTime(conversation.Activity.SettledAt)
		if failure := conversation.Activity.Error; failure != nil {
			observation.Error = &watcher.ProviderActivityError{
				Code: failure.Code, HTTPStatus: failure.HTTPStatus, Message: failure.Message,
			}
		}
	}
	for _, activity := range conversation.ProviderActivities {
		status := strings.TrimSpace(string(activity.Status))
		switch status {
		case string(work.ProviderActivityCompleted),
			string(work.ProviderActivityFailed),
			string(work.ProviderActivityInterrupted),
			string(work.ProviderActivityCancelled):
			observation.TerminalActivities = append(
				observation.TerminalActivities,
				watcher.ProviderTerminalActivity{
					ID:        strings.TrimSpace(activity.ID),
					Status:    status,
					StartedAt: parseProviderActivityTime(activity.StartedAt),
					SettledAt: parseProviderActivityTime(activity.SettledAt),
				},
			)
		}
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		event := conversation.Events[index]
		at := parseProviderActivityTime(event.Timestamp)
		if !observation.StartedAt.IsZero() && !at.Before(observation.StartedAt) && at.After(observation.ProgressAt) {
			observation.ProgressAt = at
		}
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		event := conversation.Events[index]
		if event.Kind != "user_message" || strings.TrimSpace(event.Body) == "" {
			continue
		}
		observation.AdmissionStream = strings.Join([]string{
			strings.TrimSpace(conversation.Source),
			strings.TrimSpace(conversation.SessionID),
			strings.TrimSpace(conversation.Path),
		}, "\x00")
		observation.AdmissionID = strings.TrimSpace(event.ID)
		if event.Seq > 0 {
			observation.AdmissionCursor = uint64(event.Seq)
		}
		if provider != work.WorkerProviderCursor {
			observation.AdmissionAt = parseProviderActivityTime(event.Timestamp)
		}
		observation.InputSHA256 = strings.TrimSpace(event.AdmissionSHA256)
		observation.InputUnwrappedSHA256 = strings.TrimSpace(event.AdmissionUnwrappedSHA256)
		break
	}
	return observation
}

// noteLoss logs why a Worker's provider conversation could not be read, once
// per change, so a delegated input that is refused for "provider activity is
// unavailable" can be traced to the reader that said so.
func (p *workProviderActivityProbe) noteLoss(worker classifier.Worker, provider string, state watcher.ProviderProbeState, detail string) {
	loss := state.Loss()
	key := ""
	if loss {
		key = string(state) + ": " + detail
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lost == nil {
		p.lost = map[string]string{}
	}
	previous := p.lost[worker.ID]
	if key == previous {
		return
	}
	if loss {
		p.lost[worker.ID] = key
		log.Printf("WARN provider activity probe: %s Worker %s (%s) is %s", provider, worker.ID, worker.Name, key)
		return
	}
	delete(p.lost, worker.ID)
	log.Printf("provider activity probe: %s Worker %s is readable again", provider, worker.ID)
}

// probeStateForConversation classifies the provider-channel health of one
// read: a successful read with no new fact is ProbeStateOK; a provably lost
// source is unlocatable (missing) or unreadable (present but unreadable/
// malformed). Only loss states may drive the bounded session.uncertain.
func probeStateForConversation(conversation work.CodexConversation, err error) watcher.ProviderProbeState {
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return watcher.ProbeStateUnlocatable
		}
		return watcher.ProbeStateUnreadable
	}
	reason := strings.TrimSpace(conversation.Reason)
	switch {
	case reason == "", reason == "not_structured_agent", reason == "missing_cwd":
		return watcher.ProbeStateOK
	case reason == "transcript_malformed", strings.HasPrefix(reason, "conversation_unreadable"):
		// The source is there and cannot be read (agy answered another way
		// than Mewla reads): a loss of evidence, not a missing transcript.
		return watcher.ProbeStateUnreadable
	default:
		// transcript_not_found, session_not_found, db_unavailable, ...
		return watcher.ProbeStateUnlocatable
	}
}

func (p *workProviderActivityProbe) ForgetProviderActivity(workerID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	delete(p.readers, strings.TrimSpace(workerID))
	p.mu.Unlock()
}

func parseProviderActivityTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed.UTC()
}
