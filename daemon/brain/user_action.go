package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/google/uuid"
)

// WorkUserActionKind is what the user did to a Work from its slip. Every
// action appends one user.* Event, so Brain reads the decision as a fact.
//
//   - reply: an answer (free text or one of Brain's choices). It reaches Brain
//     as a turn input naming the Work, clears Brain's question and releases a
//     user_input wait. Brain still decides what happens next.
//   - close: the user is satisfied. The Work closes as done, by the user.
//   - dismiss: not needed anymore. The Work closes as cancelled, by the user.
//   - stop: cancel running Work and close its delegated Worker Session.
//   - snooze: hide the Work from Needs you until a time. Nothing is
//     scheduled; the app compares the time, the daemon only records it.
//
// A user close is final: Brain records nothing more and never reopens it.
type WorkUserActionKind string

const (
	WorkUserReply   WorkUserActionKind = "reply"
	WorkUserClose   WorkUserActionKind = "close"
	WorkUserDismiss WorkUserActionKind = "dismiss"
	WorkUserStop    WorkUserActionKind = "stop"
	WorkUserSnooze  WorkUserActionKind = "snooze"
)

// WorkUserActor names the user on every Event and close they make.
const WorkUserActor = "user"

const workChoiceLimit = 4

var ErrWorkUserAction = errors.New("Brain Work user action")

// WorkUserAction is the user's newest decision on a Work, projected on its
// slip until a newer result comes back.
type WorkUserAction struct {
	Kind      WorkUserActionKind `json:"kind"`
	Text      string             `json:"text,omitempty"`
	At        time.Time          `json:"at"`
	EventID   string             `json:"event_id"`
	Admission string             `json:"admission,omitempty"`
}

type WorkUserActionRequest struct {
	WorkID string             `json:"work_id"`
	Kind   WorkUserActionKind `json:"kind"`
	// Text is the reply; for a choice it is the choice itself.
	Text        string     `json:"text,omitempty"`
	SnoozeUntil *time.Time `json:"snooze_until,omitempty"`
	// RequestID makes a retried reply the same Brain input.
	RequestID string `json:"request_id,omitempty"`
}

type WorkUserActionResult struct {
	Work      Work                     `json:"work"`
	Event     WorkEvent                `json:"event"`
	Admission ExternalInputDisposition `json:"admission,omitempty"`
}

// applyWorkQuestion sets or clears what Brain asks the user. Choices are
// trimmed, de-duplicated and bounded so the slip stays one row of buttons.
func applyWorkQuestion(item Work, question *string, choices *[]string, now time.Time) Work {
	if question != nil {
		item.Question = strings.TrimSpace(*question)
	}
	if choices != nil {
		item.Choices = normalizeWorkChoices(*choices)
	}
	if item.Question == "" {
		item.Question, item.Choices, item.QuestionAt = "", nil, nil
		return item
	}
	at := now.UTC()
	item.QuestionAt = &at
	return item
}

func normalizeWorkChoices(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" || seen[value] || len(out) == workChoiceLimit {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// WorkReplyBody is the Brain turn input for a reply: the Work it answers,
// by title and id, then the user's words.
func WorkReplyBody(item Work, text string) string {
	header := "Re: " + firstNonEmpty(strings.TrimSpace(item.Title), item.ID) + " (work " + item.ID + ")"
	if question := strings.TrimSpace(item.Question); question != "" {
		header += "\n> " + question
	}
	return header + "\n" + strings.TrimSpace(text)
}

// RecordWorkUserAction appends the user's decision for a non-closing action
// (reply or snooze) and applies its effect in one replacement: a reply clears
// Brain's question and releases a user_input wait; a snooze records its time.
func (s *Store) RecordWorkUserAction(request WorkUserActionRequest, admission ExternalInputDisposition, receipt string) (Work, WorkEvent, error) {
	request.WorkID = strings.TrimSpace(request.WorkID)
	request.Text = strings.TrimSpace(request.Text)
	now := s.nowUTC()
	s.mu.Lock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, err
	}
	index := workIndex(database.BrainWork, request.WorkID)
	if index < 0 {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, ErrWorkNotFound
	}
	item := database.BrainWork[index]
	if item.Status == WorkDone || item.Status == WorkCancelled {
		s.mu.Unlock()
		return Work{}, WorkEvent{}, fmt.Errorf("%w: Work %s is already closed", ErrWorkUserAction, item.ID)
	}
	event := WorkEvent{
		ID: uuid.NewString(), WorkID: item.ID, SourceName: WorkUserActor,
		Actionable: false, CreatedAt: now,
	}
	switch request.Kind {
	case WorkUserReply:
		event.Kind = "user.replied"
		event.DedupeKey = "user:reply:" + receipt
		event.PayloadRef = receipt
		event.Summary = request.Text
		details, _ := json.Marshal(map[string]string{"admission": string(admission), "question": item.Question})
		event.DetailsJSON = string(details)
		if st, stateErr := s.fsmState(item.ID); stateErr == nil && st != nil && st.Wake != nil && st.Wake.Kind == lifecycle.WakeKind(WorkWakeUserInput) {
			// The user answered what the Work waited on.
			if _, err := s.fsm.ClearWait(st.ID, st.Wake.Kind, st.Wake.Ref, receipt); err != nil {
				s.mu.Unlock()
				return Work{}, WorkEvent{}, err
			}
			if err := s.fsmSyncWorkLocked(&database, item.ID, now); err != nil {
				s.mu.Unlock()
				return Work{}, WorkEvent{}, err
			}
		}
		database.BrainWork[index] = applyWorkQuestion(database.BrainWork[index], new(string), nil, now)
	case WorkUserSnooze:
		if request.SnoozeUntil == nil || !request.SnoozeUntil.After(now) {
			s.mu.Unlock()
			return Work{}, WorkEvent{}, fmt.Errorf("%w: snooze needs a future time", ErrWorkUserAction)
		}
		until := request.SnoozeUntil.UTC()
		event.Kind = "user.snoozed"
		event.DedupeKey = "user:snooze:" + until.Format(time.RFC3339Nano)
		event.Summary = "Snoozed until " + until.Format(time.RFC3339)
		database.BrainWork[index].SnoozedUntil = &until
	default:
		s.mu.Unlock()
		return Work{}, WorkEvent{}, fmt.Errorf("%w: %q is not recorded here", ErrWorkUserAction, request.Kind)
	}
	for _, current := range database.BrainWorkEvents {
		if current.WorkID == item.ID && current.DedupeKey == event.DedupeKey {
			s.mu.Unlock()
			return database.BrainWork[index], current, nil
		}
	}
	recorded, err := appendWorkEventLocked(&database, index, event, true)
	if err == nil {
		err = s.persistPresentationLocked(database)
	}
	item = database.BrainWork[index]
	s.mu.Unlock()
	if err != nil {
		return Work{}, WorkEvent{}, err
	}
	s.broadcastWorkChange(item.ID)
	return item, recorded, nil
}

// latestWorkUserAction is the user's newest decision that no newer result
// has superseded.
func latestWorkUserAction(database presentationDatabase, workID string) *WorkUserAction {
	var latest *WorkEvent
	for index := range database.BrainWorkEvents {
		event := &database.BrainWorkEvents[index]
		if event.WorkID != workID {
			continue
		}
		if isResultEvent(event.Kind) {
			latest = nil
			continue
		}
		if strings.HasPrefix(event.Kind, "user.") ||
			(event.Kind == "brain.work_closed" && event.SourceName == WorkUserActor) {
			latest = event
		}
	}
	if latest == nil {
		return nil
	}
	action := &WorkUserAction{Text: latest.Summary, At: latest.CreatedAt.UTC(), EventID: latest.ID}
	switch latest.Kind {
	case "user.replied":
		action.Kind = WorkUserReply
		var details struct {
			Admission string `json:"admission"`
		}
		if json.Unmarshal([]byte(latest.DetailsJSON), &details) == nil {
			action.Admission = details.Admission
		}
	case "user.snoozed":
		action.Kind = WorkUserSnooze
	default:
		action.Kind = WorkUserClose
	}
	return action
}

// ActOnWork applies one user action from a Work slip. Closing actions use the
// audited close path with the user as actor; a reply is submitted to Brain
// through the same admission as any user message, then recorded on the Work.
func (s *Service) ActOnWork(request WorkUserActionRequest) (WorkUserActionResult, error) {
	if s == nil || s.store == nil {
		return WorkUserActionResult{}, fmt.Errorf("brain service is not configured")
	}
	request.WorkID = strings.TrimSpace(request.WorkID)
	request.Text = strings.TrimSpace(request.Text)
	item, err := s.store.Work(request.WorkID)
	if err != nil {
		return WorkUserActionResult{}, err
	}
	switch request.Kind {
	case WorkUserReply:
		if request.Text == "" {
			return WorkUserActionResult{}, fmt.Errorf("%w: a reply needs text", ErrWorkUserAction)
		}
		requestID := firstNonEmpty(strings.TrimSpace(request.RequestID), uuid.NewString())
		receipt := "work-reply:" + item.ID + ":" + requestID
		if event, found := s.store.workEventByDedupe(item.ID, "user:reply:"+receipt); found {
			// A retried request: Brain already has this reply.
			return WorkUserActionResult{Work: item, Event: event, Admission: ExternalInputAccepted}, nil
		}
		admission, submitErr := s.SubmitExternalUserInput(receipt, WorkReplyBody(item, request.Text))
		if submitErr != nil || admission == ExternalInputNotSubmitted {
			if submitErr == nil {
				submitErr = fmt.Errorf("Brain did not receive the reply")
			}
			return WorkUserActionResult{Admission: admission}, submitErr
		}
		updated, event, err := s.store.RecordWorkUserAction(request, admission, receipt)
		return WorkUserActionResult{Work: updated, Event: event, Admission: admission}, err
	case WorkUserSnooze:
		updated, event, err := s.store.RecordWorkUserAction(request, "", "")
		return WorkUserActionResult{Work: updated, Event: event}, err
	case WorkUserClose, WorkUserDismiss, WorkUserStop:
		status := WorkDone
		reason := firstNonEmpty(request.Text, "Closed by the user: done.")
		if request.Kind != WorkUserClose {
			status = WorkCancelled
			reason = firstNonEmpty(request.Text, "Dismissed by the user: not needed anymore.")
		}
		session := ""
		if request.Kind == WorkUserStop {
			if item.AttemptSessionID == "" || !item.AttemptDelegated {
				return WorkUserActionResult{}, fmt.Errorf("%w: Work %s has no running Worker", ErrWorkUserAction, item.ID)
			}
			session = item.AttemptSessionID
			reason = firstNonEmpty(request.Text, "Stopped by the user.")
		}
		closed, err := s.CloseWork(WorkCloseRequest{
			WorkID: item.ID, ExpectedRevision: item.Revision, Status: status,
			Actor: WorkUserActor, Reason: reason,
		})
		if err != nil && !errors.Is(err, ErrWorkCleanupPending) {
			return WorkUserActionResult{}, err
		}
		if session != "" && !s.isHostSession(session) {
			// Cancelling released the Attempt; the Worker it ran in goes too.
			if teardownErr := s.teardownOwnedSession(session); teardownErr != nil {
				err = errors.Join(err, fmt.Errorf("%w: %s: %w", ErrWorkCleanupPending, item.ID, teardownErr))
			}
		}
		event, _ := s.store.latestWorkEvent(closed.ID, "brain.work_closed")
		return WorkUserActionResult{Work: closed, Event: event}, err
	default:
		return WorkUserActionResult{}, fmt.Errorf("%w: unknown action %q", ErrWorkUserAction, request.Kind)
	}
}

func (s *Service) isHostSession(sessionID string) bool {
	host, err := s.store.HostSession()
	return err != nil || strings.TrimSpace(host.ID) == strings.TrimSpace(sessionID)
}

func (s *Store) workEventByDedupe(workID, dedupeKey string) (WorkEvent, bool) {
	events, err := s.ListWorkEvents(workID)
	if err != nil {
		return WorkEvent{}, false
	}
	for _, event := range events {
		if event.DedupeKey == dedupeKey {
			return event, true
		}
	}
	return WorkEvent{}, false
}

func (s *Store) latestWorkEvent(workID, kind string) (WorkEvent, bool) {
	events, err := s.ListWorkEvents(workID)
	if err != nil {
		return WorkEvent{}, false
	}
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Kind == kind {
			return events[index], true
		}
	}
	return WorkEvent{}, false
}
