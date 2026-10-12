package watch

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

// Process signals come from the Work store: turns lost or stalled, automatic
// resumes, reconcile requests, ambiguous deliveries, input requests, failed
// turns, repeated follow-up sends and Work reopened after closing.

// processKinds maps Work event kinds to signal kinds.
var processKinds = map[string]string{
	"turn_lost":                "turn_lost",
	"lease_expired":            "lease_expired",
	"session.stale":            "stalled",
	"session.auto_resumed":     "auto_resumed",
	"brain.reconcile_required": "reconcile_required",
	"delivery.ambiguous":       "delivery_ambiguous",
	"session.needs_input":      "needs_input",
	"session.failed":           "session_failed",
	"turn_failed":              "turn_failed",
	"submission_failed":        "submission_failed",
}

const (
	kindFollowUp = "follow_up"
	kindReopened = "reopened"
)

// WorkSource is the Brain Work store.
type WorkSource interface {
	ListWork() ([]brain.Work, error)
	ListWorkEvents(workID string) ([]brain.WorkEvent, error)
}

// AdmissionSource lists the lifecycle aggregates with their turn admissions.
type AdmissionSource interface {
	ListStates() []*lifecycle.State
}

type processCursor struct {
	Sequence uint64          `json:"sequence"`
	Closed   map[string]bool `json:"closed,omitempty"`
}

func (c *Collector) admittedDigests() map[string]bool {
	out := map[string]bool{}
	if c.admissions == nil {
		return out
	}
	for _, state := range c.admissions.ListStates() {
		for _, admission := range state.Admissions {
			if admission != nil && admission.PayloadSHA256 != "" && admission.Purpose != lifecycle.AdmissionPurposeReview {
				out[admission.PayloadSHA256] = true
			}
		}
	}
	return out
}

func (c *Collector) collectProcess(ctx context.Context) (int, error) {
	if c.works == nil {
		return 0, nil
	}
	works, err := c.works.ListWork()
	if err != nil {
		return 0, err
	}
	titles := make(map[string]brain.Work, len(works))
	for _, item := range works {
		titles[item.ID] = item
	}
	states := map[string]*lifecycle.State{}
	if c.admissions != nil {
		for _, state := range c.admissions.ListStates() {
			states[string(state.ID)] = state
		}
	}
	since := c.horizon()
	var signals []Signal

	// Follow-ups: every Worker turn admitted after a Work's first one.
	for id, state := range states {
		var admissions []*lifecycle.AdmissionState
		for _, admission := range state.Admissions {
			if admission == nil || admission.Purpose == lifecycle.AdmissionPurposeReview {
				continue
			}
			if admission.Status == lifecycle.AdmissionAccepted || admission.Status == lifecycle.AdmissionAmbiguous {
				admissions = append(admissions, admission)
			}
		}
		sort.Slice(admissions, func(i, j int) bool { return admissions[i].AttemptedAt.Before(admissions[j].AttemptedAt) })
		for index, admission := range admissions {
			if index == 0 || admission.AttemptedAt.Before(since) {
				continue
			}
			signals = append(signals, c.processSignal(id, state.Title, kindFollowUp, admission.AttemptedAt,
				"Follow-up turn sent to the Worker", string(admission.TurnToken), admission.SessionID,
				map[string]string{"turn": string(admission.TurnToken), "attempt": strconv.Itoa(index + 1)}))
		}
	}

	var cursor processCursor
	c.store.Cursor("process:work_events", &cursor)
	if cursor.Closed == nil {
		cursor.Closed = map[string]bool{}
	}
	events, err := c.works.ListWorkEvents("")
	if err != nil {
		return 0, err
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	next := cursor.Sequence
	for _, event := range events {
		if event.Sequence <= cursor.Sequence {
			continue
		}
		next = event.Sequence
		title := titles[event.WorkID].Title
		if title == "" {
			if state := states[event.WorkID]; state != nil {
				title = state.Title
			}
		}
		switch {
		case event.Kind == "brain.work_closed":
			cursor.Closed[event.WorkID] = true
		case cursor.Closed[event.WorkID] && (event.Kind == "session.running" || event.Kind == "user.input"):
			delete(cursor.Closed, event.WorkID)
			if !event.CreatedAt.Before(since) {
				signals = append(signals, c.processSignal(event.WorkID, title, kindReopened, event.CreatedAt,
					"Work resumed after it was closed", event.ID, event.SourceName, map[string]string{"event": event.ID}))
			}
		}
		kind, tracked := processKinds[event.Kind]
		if !tracked || event.CreatedAt.Before(since) {
			continue
		}
		refs := map[string]string{"event": event.ID, "work_event_kind": event.Kind}
		if token := turnTokenPattern.FindString(event.PayloadRef + " " + event.DedupeKey); token != "" {
			refs["turn"] = token
		}
		signals = append(signals, c.processSignal(event.WorkID, title, kind, event.CreatedAt,
			event.Summary, event.ID, event.SourceName, refs))
	}
	added, err := c.store.Append(signals)
	if err != nil {
		return 0, err
	}
	cursor.Sequence = next
	if err := c.store.SetCursor("process:work_events", cursor); err != nil {
		return added, err
	}
	return added, ctx.Err()
}

func (c *Collector) processSignal(workID, title, kind string, at time.Time, summary, identity, sessionID string, refs map[string]string) Signal {
	refs["work"] = workID
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		refs["session"] = sessionID
	}
	if title != "" {
		refs["work_title"] = excerpt(title, 120)
	}
	cwd := c.workPlace(workID, sessionID, refs["turn"])
	projectID := ""
	if cwd != "" {
		refs["cwd"] = cwd
		projectID, _ = c.store.ProjectForCWD(cwd, time.Time{})
	}
	return Signal{
		ID:        signalID(SourceProcess, kind+"|"+workID+"|"+identity),
		Source:    SourceProcess,
		ProjectID: projectID,
		Time:      at,
		Kind:      kind,
		Excerpt:   excerpt(firstNonEmpty(summary, title), 400),
		Refs:      refs,
		Pointer:   "work:" + workID,
	}
}

// workPlace resolves where a Work ran: a remembered answer, the turn named by
// the event, any turn the Work admitted, or the live Session's directory.
func (c *Collector) workPlace(workID, sessionID, turn string) string {
	if cwd := c.store.PlaceOf("work", workID); cwd != "" {
		return cwd
	}
	cwd := ""
	if match := turnTokenPattern.FindStringSubmatch(turn); match != nil {
		cwd = c.store.PlaceOf("turn", match[1])
	}
	if cwd == "" && c.admissions != nil {
		cwd = c.admissionPlace(workID)
	}
	if cwd == "" && sessionID != "" {
		cwd = c.store.PlaceOf("session", sessionID)
	}
	if cwd != "" {
		c.store.Place("work", workID, cwd)
	}
	return cwd
}

func (c *Collector) admissionPlace(workID string) string {
	if c.statesByID == nil {
		c.statesByID = map[string]*lifecycle.State{}
		for _, state := range c.admissions.ListStates() {
			c.statesByID[string(state.ID)] = state
		}
	}
	state := c.statesByID[workID]
	if state == nil {
		return ""
	}
	for token, admission := range state.Admissions {
		if match := turnTokenPattern.FindStringSubmatch(string(token)); match != nil {
			if cwd := c.store.PlaceOf("turn", match[1]); cwd != "" {
				return cwd
			}
		}
		if admission != nil {
			if cwd := c.store.PlaceOf("session", admission.SessionID); cwd != "" {
				return cwd
			}
		}
	}
	return ""
}
