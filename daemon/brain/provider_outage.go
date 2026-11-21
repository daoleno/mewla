package brain

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// A provider outage (a rate limit, an overload, a dropped network) makes every
// turn fail at once: Brain's, every Worker's, the judges'. A failed turn is not
// an ended turn. While the Host's agent family is unavailable Brain holds still:
//
//   - A delivered result whose Host turn the provider refused was never read.
//     Its handling ends without counting as handled and the same review is
//     delivered again (endHostHandling), so the Work is not settled on it.
//   - The Host lane delivers nothing while the family is unavailable, except
//     one probe per interval for the whole family (hostLaneHeld), and runs
//     again the moment the provider answers (ProviderAvailabilityChanged).
//   - The closure judge's failure decides nothing (settle.go).
//
// The person's own messages are never held: they go to the Host as typed.

// providerAvailability is the watcher's provider outage state
// (watcher/provider_availability.go). A Watcher without it has no outages.
type providerAvailability interface {
	ProviderOutage(provider string) (watcher.ProviderOutage, bool)
	ProviderOutages() []watcher.ProviderOutage
	ProviderProbe(provider string, now time.Time) (bool, time.Time)
}

func (s *Service) availability() providerAvailability {
	if s == nil || s.watcher == nil {
		return nil
	}
	availability, _ := s.watcher.(providerAvailability)
	return availability
}

// ProviderOutages lists the agent families refusing requests now.
func (s *Service) ProviderOutages() []watcher.ProviderOutage {
	if availability := s.availability(); availability != nil {
		if outages := availability.ProviderOutages(); len(outages) > 0 {
			return outages
		}
	}
	return nil
}

// hostTurnRefused reports the transient provider error that settled the
// Host's exact provider turn: the error must be on that turn's own Activity,
// so an older refusal never marks a good turn and a later answer never hides a
// refused one. Anything unreadable reports false (the turn ends as before).
func (s *Service) hostTurnRefused(hostSessionID, providerTurnID string) (watcher.ProviderActivityError, bool) {
	if s == nil || s.store == nil || s.watcher == nil {
		return watcher.ProviderActivityError{}, false
	}
	turn, found, err := s.store.TurnByID(hostSessionID, providerTurnID)
	activity := strings.TrimSpace(turn.ActivityID)
	if err != nil || !found || activity == "" {
		return watcher.ProviderActivityError{}, false
	}
	observation, _, err := s.watcher.ProbeProviderEvidence(hostSessionID)
	if err != nil || observation.ProbeState != watcher.ProbeStateOK || observation.Error == nil ||
		strings.TrimSpace(observation.ID) != activity {
		return watcher.ProviderActivityError{}, false
	}
	if watcher.ClassifyProviderError(*observation.Error) != watcher.ProviderErrorTransient {
		return watcher.ProviderActivityError{}, false
	}
	return *observation.Error, true
}

// endHostHandling ends one delivered handling whose Host turn is over. A turn
// the provider refused is replayed instead of ended (Brain never read it);
// every other ending is EndReviewDelivery's. It reports whether the handling
// ended either way.
func (s *Service) endHostHandling(workID, handlingID, providerTurnID, hostSessionID string) (bool, error) {
	if failure, refused := s.hostTurnRefused(hostSessionID, providerTurnID); refused {
		reason := "Brain's turn was refused by the provider (" + watcher.ProviderErrorSummary(failure) +
			"); the result is delivered again once it answers."
		replayed, err := s.store.ReplayInterruptedReviewDelivery(workID, handlingID, providerTurnID, reason)
		if err != nil {
			return false, err
		}
		if replayed {
			log.Printf("brain: Work %s result held for redelivery: Host turn %s refused by the provider (%s)",
				workID, providerTurnID, watcher.ProviderErrorSummary(failure))
			return true, nil
		}
	}
	_, ended, err := s.store.EndReviewDelivery(workID, handlingID, providerTurnID)
	return ended, err
}

// hostLaneHold is the timer that runs the Host lane again when the next probe
// of an unavailable provider is due.
type hostLaneHold struct {
	mu    sync.Mutex
	timer *time.Timer
	at    time.Time
}

// hostLaneHeld reports whether the Host lane must deliver nothing now: the
// Host's agent family is unavailable, a review is waiting, and this is not the
// family's probe. It arms a pass for the next probe.
func (s *Service) hostLaneHeld(hostID string) bool {
	availability := s.availability()
	if availability == nil {
		return false
	}
	command := s.hostExecutor().Command
	if worker := s.watcher.GetWorker(hostID); worker != nil && strings.TrimSpace(worker.Command) != "" {
		command = worker.Command
	}
	family := watcher.ProviderFamily(command)
	if _, unavailable := availability.ProviderOutage(family); !unavailable {
		return false
	}
	// A probe spent on an empty lane would delay noticing the provider is back.
	if !s.store.reviewClaimable(s.nowUTC()) {
		return true
	}
	granted, next := availability.ProviderProbe(family, s.nowUTC())
	if granted {
		return false
	}
	s.armHostLaneAt(next)
	return true
}

func (s *Service) armHostLaneAt(at time.Time) {
	if at.IsZero() {
		return
	}
	s.laneHold.mu.Lock()
	defer s.laneHold.mu.Unlock()
	if s.laneHold.timer != nil && !s.laneHold.at.After(at) && s.laneHold.at.After(s.nowUTC()) {
		return
	}
	if s.laneHold.timer != nil {
		s.laneHold.timer.Stop()
	}
	s.laneHold.at = at
	s.laneHold.timer = time.AfterFunc(time.Until(at)+time.Second, func() {
		if _, err := s.ReconcileHostLane(); err != nil {
			log.Printf("brain Host lane after a provider probe wait: %v", err)
		}
	})
}

// ProviderAvailabilityChanged runs when an agent family's provider became
// unavailable or answers again: whatever the lane held is delivered now.
func (s *Service) ProviderAvailabilityChanged() {
	if s == nil || s.store == nil {
		return
	}
	if _, err := s.ReconcileHostLane(); err != nil {
		log.Printf("brain Host lane after a provider availability change: %v", err)
	}
}

// reviewClaimable reports whether some open Work has a review the Host lane
// could claim now (the same test ClaimNextReviewAction applies).
func (s *Store) reviewClaimable(now time.Time) bool {
	if s == nil || s.fsm == nil {
		return false
	}
	claimable := false
	s.fsm.Each(func(st *lifecycle.State) {
		if !claimable && st.Review != nil && st.Review.Handler == nil && st.Review.DeliveryReady(now) &&
			!st.Status.Terminal() && st.ActiveAdmission() == nil {
			claimable = true
		}
	})
	return claimable
}

// suppressRefusedWorkReplies keeps out of Chat the provider's refusal text
// ("API Error: Request rejected (429) …") that ends a Host turn handed a Work
// event, when the refusal is transient: that delivery is replayed
// (endHostHandling), and Chat's one outage line says the provider is not
// answering. A refused reply to the person's own message stays, since that
// message is not replayed, and so do sign-in and other non-transient refusals,
// which nothing else reports.
func suppressRefusedWorkReplies(events []work.CodexConversationEvent) []work.CodexConversationEvent {
	var out []work.CodexConversationEvent
	answeringWork := false
	for index, event := range events {
		keep := true
		switch strings.TrimSpace(event.Kind) {
		case "user_message":
			answeringWork = work.IsDirectWorkEventPresentationInput(event.Body)
		case "assistant_message":
			keep = !(answeringWork && transientRefusal(event.ProviderError))
		}
		if !keep {
			if out == nil {
				out = append(make([]work.CodexConversationEvent, 0, len(events)), events[:index]...)
			}
			continue
		}
		if out != nil {
			out = append(out, event)
		}
	}
	if out == nil {
		return events
	}
	return out
}

func transientRefusal(failure *work.ProviderActivityError) bool {
	return failure != nil && watcher.ClassifyProviderError(watcher.ProviderActivityError{
		Code: failure.Code, HTTPStatus: failure.HTTPStatus, Message: failure.Message,
	}) == watcher.ProviderErrorTransient
}
