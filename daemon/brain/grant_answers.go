package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// How the person's answers to permission cards reach Brain: the lane of the
// overdue-wait list (wait_end.go). One grant-answers Work carries one event
// that lists every card sealed since Brain last read one; a card sealed within
// grantAnswersQuiet of the last joins the list that has not been opened yet,
// and a list Brain has not started reading is replaced by a newer one that
// carries its lines too. The runtime closes the Work as a fact once Brain's
// reading ended (settle.go), no judge call, so nothing stays on the board.
const (
	// grantAnswersQuiet is how long the list waits for more answers before it
	// wakes Brain: cards sealed in one go arrive as one event.
	grantAnswersQuiet = 20 * time.Second
	// grantAnswersLimit bounds the lines an event carries; the rest are counted.
	grantAnswersLimit = 8

	grantAnswersWorkPrefix = "grant-answers:"
	grantAnswersEventKind  = "work.grants_answered"
)

// grantAnswersBrief is what the event carries, besides its summary.
type grantAnswersBrief struct {
	Answers []string `json:"answers"`
	More    int      `json:"more,omitempty"`
}

// readOnceWork says a Work is one of the lists the runtime closes by fact once
// Brain has read it, and that no judge, stale sweep or objective looks at.
func readOnceWork(id string) bool {
	return strings.HasPrefix(id, overdueWorkPrefix) || strings.HasPrefix(id, grantAnswersWorkPrefix)
}

// NoteGrantAnswered tells Brain, once per sealed card, how a grant request
// ended, in one line (the line is addressed to Brain). It writes the line to
// the grant-answers list and returns; the list wakes Brain after
// grantAnswersQuiet (RunWaitEnds), or at the next look after a restart.
func (s *Service) NoteGrantAnswered(card TimelineItem, line string) error {
	if s == nil || s.store == nil {
		return errors.New("brain service is not configured")
	}
	line = strings.Join(strings.Fields(line), " ")
	if line == "" {
		return errors.New("a grant answer needs a line")
	}
	s.grantAnswersMu.Lock()
	defer s.grantAnswersMu.Unlock()
	now := s.nowUTC()
	lines := []string{line}
	last, found, err := s.lastGrantAnswers()
	if err != nil {
		return err
	}
	if found && last.Status != WorkDone && last.Status != WorkCancelled && !overdueBatchReading(s, last.ID) {
		// The older list has not been read (it may not even be open yet): its
		// lines move to the newer one, which replaces it.
		prior, err := s.grantAnswersLines(last.ID)
		if err != nil {
			return err
		}
		lines = append(prior, line)
		if _, err := s.CloseWork(WorkCloseRequest{
			WorkID: last.ID, ExpectedRevision: last.Revision, Status: WorkCancelled,
			Actor: lifecycle.ActorRuntime, Reason: "Replaced by a newer list of permission answers.",
		}); err != nil && !errors.Is(err, ErrWorkCleanupPending) {
			return err
		}
	}
	id := grantAnswersWorkPrefix + now.Format("20060102T150405.000000000Z")
	if _, _, err := s.store.EnsureWork(Work{
		ID: id, Title: "Permission answers", CompletionPolicy: CompletionBounded,
		Objective:  "Act on the person's answers to permission requests.",
		NextAction: "Each line says what the person chose. If a permission was granted, start the Worker again with its tool; if they chose Not now, stop asking for it on this task. Nothing to close: this ends when you have read it.",
	}); err != nil {
		return fmt.Errorf("grant answers Work: %w", err)
	}
	listed, more := lines, 0
	if len(listed) > grantAnswersLimit {
		listed, more = listed[len(listed)-grantAnswersLimit:], len(lines)-grantAnswersLimit
	}
	details, err := json.Marshal(grantAnswersBrief{Answers: listed, More: more})
	if err != nil {
		return err
	}
	summary := strings.Join(listed, " ")
	if more > 0 {
		summary = fmt.Sprintf("(%d earlier answers not listed.) %s", more, summary)
	}
	if _, _, err := s.store.AppendWorkEvent(WorkEvent{
		WorkID: id, Kind: grantAnswersEventKind, DedupeKey: id, SourceName: "Mewla",
		Summary: summary, DetailsJSON: string(details), Actionable: true,
	}); err != nil {
		return err
	}
	select {
	case s.grantWake <- struct{}{}:
	default:
	}
	return nil
}

// grantAnswersLines are the lines the list's event carries.
func (s *Service) grantAnswersLines(id string) ([]string, error) {
	events, err := s.store.ListWorkEvents(id)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, event := range events {
		var brief grantAnswersBrief
		if event.Kind == grantAnswersEventKind && json.Unmarshal([]byte(event.DetailsJSON), &brief) == nil {
			lines = brief.Answers
		}
	}
	return lines, nil
}

func (s *Service) lastGrantAnswers() (Work, bool, error) {
	items, err := s.store.ListWork()
	if err != nil {
		return Work{}, false, err
	}
	var last Work
	found := false
	for _, item := range items {
		if strings.HasPrefix(item.ID, grantAnswersWorkPrefix) && (!found || item.ID > last.ID) {
			last, found = item, true
		}
	}
	return last, found, nil
}

// openGrantAnswers wakes Brain with the newest list once it has been quiet for
// grantAnswersQuiet; a list already with Brain, or still gathering, is left.
func (s *Service) openGrantAnswers() error {
	if s == nil || s.store == nil {
		return nil
	}
	s.grantAnswersMu.Lock()
	defer s.grantAnswersMu.Unlock()
	now := s.nowUTC()
	last, found, err := s.lastGrantAnswers()
	if err != nil || !found || last.Status == WorkDone || last.Status == WorkCancelled {
		return err
	}
	events, err := s.store.ListWorkEvents(last.ID)
	if err != nil {
		return err
	}
	var recorded *WorkEvent
	for index := range events {
		if events[index].DedupeKey == last.ID {
			recorded = &events[index]
		}
	}
	if recorded == nil || recorded.HandledAt != nil || recorded.DiscardedAt != nil || now.Sub(recorded.CreatedAt) < grantAnswersQuiet {
		return nil
	}
	st, err := s.store.fsmState(last.ID)
	if err != nil || st == nil || st.Status.Terminal() || st.Review != nil {
		return err // already with Brain
	}
	if _, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(last.ID), grantAnswersEventKind, recorded.ID, recorded.ID); err != nil {
		return err
	}
	if err := s.store.SyncWorkProjection(last.ID); err != nil {
		return err
	}
	_, err = s.ReconcileHostLane()
	return err
}

// runGrantAnswers opens the list after a quiet spell: when an answer wakes it,
// and at start and each minute (a restart that cut the wait short).
func (s *Service) runGrantAnswers(ctx context.Context) {
	look := time.NewTicker(overdueTick)
	defer look.Stop()
	for {
		if err := s.openGrantAnswers(); err != nil {
			log.Printf("brain grant answers: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.grantWake:
			select {
			case <-ctx.Done():
				return
			case <-time.After(grantAnswersQuiet + time.Second):
			}
		case <-look.C:
		}
	}
}
