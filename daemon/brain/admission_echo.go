package brain

import (
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// IsBrainInputAdmission reports whether a durable user row was written by a
// Brain Interface send_input admission. Identity is solely the explicit
// brain_admission field; no pre-field heuristic exists.
func IsBrainInputAdmission(item TimelineItem) bool {
	return strings.TrimSpace(item.Kind) == timelineKindUserMessage && item.BrainAdmission
}

func admissionCorrelationDigest(item TimelineItem) string {
	if digest := strings.TrimSpace(item.AdmissionSHA256); digest != "" {
		return digest
	}
	if strings.TrimSpace(item.Body) == "" {
		return ""
	}
	return AdmissionDigest(item.Body)
}

func exactProviderEventTimestamp(event work.CodexConversationEvent) (time.Time, bool) {
	raw := strings.TrimSpace(event.Timestamp)
	if raw == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

// providerUserInput is one provider-native user input in a Brain thread: a
// durable provider row or a live provider event not yet materialized.
type providerUserInput struct {
	id        string
	sessionID string
	createdAt time.Time
	digests   []string
	durable   bool
}

// echoes reports whether this input is the provider's record of the admitted
// bytes. Provider and Host Session identity are deliberately not inputs: the
// admission is the user's message, and a Host whose native transcript is not
// (yet) bound must not turn its own echo into a second bubble. Provider clocks
// keep milliseconds, so the causal bound is the admission's prepare
// millisecond; the provider can only record the input after it was typed.
func (input providerUserInput) echoes(admission BrainInputAdmission) bool {
	if input.createdAt.Before(admission.CreatedAt.UTC().Truncate(time.Millisecond)) {
		return false
	}
	for _, digest := range input.digests {
		if digest == admission.BodySHA256 {
			return true
		}
	}
	return false
}

// reconcileAdmissionEchoes is the one rule that decides which provider user
// inputs are echoes of Brain input admissions in a thread. The admission row is
// the message; its echo only confirms delivery and never becomes a row.
//
// Accepted admissions without an echo are credited in prepare order, and
// provider inputs (durable rows and live events alike) consume them in
// provider time order: an input echoes the earliest open admission with the
// same native input bytes prepared at or before it. Inputs that echo nothing
// (Terminal typing into the Brain Session) stay independent rows.
//
// claimed holds every echo id consumed in the thread; claims maps each newly
// credited admission row to its echo; out drops durable rows claimed now.
func reconcileAdmissionEchoes(
	items []TimelineItem,
	admissions []BrainInputAdmission,
	threadID string,
	providerSessionID string,
	providerEvents []work.CodexConversationEvent,
) (claimed map[string]bool, claims map[string]string, out []TimelineItem) {
	claimed = map[string]bool{}
	claims = map[string]string{}
	for _, item := range items {
		if IsBrainInputAdmission(item) {
			if echoID := strings.TrimSpace(item.AdmissionEchoEventID); echoID != "" {
				claimed[echoID] = true
			}
		}
	}

	accepted := map[string]BrainInputAdmission{}
	for _, admission := range admissions {
		if admission.ThreadID == threadID && admission.State == BrainInputAdmissionAccepted && admission.AcceptedAt != nil {
			accepted[AdmissionTimelineItemID(admission.RequestID)] = admission
		}
	}
	type credit struct {
		index     int
		admission BrainInputAdmission
	}
	var credits []credit
	for index, item := range items {
		if !IsBrainInputAdmission(item) || strings.TrimSpace(item.AdmissionEchoEventID) != "" {
			continue
		}
		admission, ok := accepted[strings.TrimSpace(item.ID)]
		if ok && timelineItemMatchesBrainInputAdmission(item, admission) {
			credits = append(credits, credit{index: index, admission: admission})
		}
	}
	if len(credits) == 0 {
		return claimed, claims, items
	}
	sort.SliceStable(credits, func(left, right int) bool {
		return credits[left].admission.CreatedAt.Before(credits[right].admission.CreatedAt)
	})

	liveDigests := map[string][]string{}
	for _, event := range providerEvents {
		if strings.TrimSpace(event.Kind) == timelineKindUserMessage {
			liveDigests[strings.TrimSpace(event.ID)] = providerEventInputDigests(event)
		}
	}
	seen := map[string]bool{}
	var inputs []providerUserInput
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" || claimed[id] || IsBrainInputAdmission(item) ||
			strings.TrimSpace(item.Kind) != timelineKindUserMessage ||
			!strings.EqualFold(strings.TrimSpace(item.Role), "user") {
			continue
		}
		body := strings.TrimSpace(item.Body)
		digests := append(append([]string(nil), item.ProviderInputSHA256...), liveDigests[id]...)
		digests = append(digests, AdmissionDigest(body))
		if inner, ok := work.UnwrapClaudePasteDisplay(body); ok {
			// Rows materialized before paste unwrapping kept Claude's envelope.
			digests = append(digests, AdmissionDigest(strings.TrimSpace(inner)))
		}
		seen[id] = true
		inputs = append(inputs, providerUserInput{
			id: id, sessionID: strings.TrimSpace(item.SessionID), createdAt: item.CreatedAt.UTC(),
			digests: providerInputDigests(digests...), durable: true,
		})
	}
	for _, event := range providerEvents {
		id := strings.TrimSpace(event.ID)
		if id == "" || seen[id] || claimed[id] ||
			strings.TrimSpace(event.Kind) != timelineKindUserMessage || !providerEventMaterializable(event) {
			continue
		}
		createdAt, _ := exactProviderEventTimestamp(event)
		digests := append(providerEventInputDigests(event), AdmissionDigest(strings.TrimSpace(event.Body)))
		seen[id] = true
		inputs = append(inputs, providerUserInput{
			id: id, sessionID: providerSessionID, createdAt: createdAt,
			digests: providerInputDigests(digests...),
		})
	}
	sort.SliceStable(inputs, func(left, right int) bool {
		return inputs[left].createdAt.Before(inputs[right].createdAt)
	})

	out = append([]TimelineItem(nil), items...)
	removed := map[string]bool{}
	for _, input := range inputs {
		for cursor := range credits {
			if !input.echoes(credits[cursor].admission) {
				continue
			}
			row := &out[credits[cursor].index]
			row.AdmissionEchoEventID = input.id
			row.AdmissionEchoSessionID = input.sessionID
			claimed[input.id] = true
			claims[strings.TrimSpace(row.ID)] = input.id
			if input.durable {
				removed[input.id] = true
			}
			credits = append(credits[:cursor], credits[cursor+1:]...)
			break
		}
		if len(credits) == 0 {
			break
		}
	}
	if len(claims) == 0 {
		return claimed, claims, items
	}
	if len(removed) > 0 {
		kept := out[:0]
		for _, item := range out {
			if !IsBrainInputAdmission(item) && removed[strings.TrimSpace(item.ID)] {
				continue
			}
			kept = append(kept, item)
		}
		out = kept
	}
	return claimed, claims, out
}

// reconcileThreadAdmissionEchoes applies reconcileAdmissionEchoes to one
// thread inside the whole timeline ledger. Non-empty claims means next must be
// written back.
func reconcileThreadAdmissionEchoes(
	allItems []TimelineItem,
	admissions []BrainInputAdmission,
	threadID string,
	providerSessionID string,
	providerEvents []work.CodexConversationEvent,
) (claimed map[string]bool, claims map[string]string, next []TimelineItem) {
	var thread []TimelineItem
	for _, item := range allItems {
		if strings.TrimSpace(item.ThreadID) == threadID {
			normalizeLegacyTimelineKind(&item)
			thread = append(thread, item)
		}
	}
	claimed, claims, updated := reconcileAdmissionEchoes(thread, admissions, threadID, providerSessionID, providerEvents)
	if len(claims) == 0 {
		return claimed, claims, allItems
	}
	byID := make(map[string]TimelineItem, len(updated))
	for _, item := range updated {
		byID[strings.TrimSpace(item.ID)] = item
	}
	next = make([]TimelineItem, 0, len(allItems))
	for _, item := range allItems {
		if strings.TrimSpace(item.ThreadID) != threadID {
			next = append(next, item)
			continue
		}
		if updatedItem, ok := byID[strings.TrimSpace(item.ID)]; ok {
			next = append(next, updatedItem)
		}
	}
	return claimed, claims, next
}
