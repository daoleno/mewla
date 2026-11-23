package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// A Work is on the board only while a Session works on it or it waits for
// something that ends by itself: an event the runtime delivers, or a time.
// Brain's named wait (-wait-for) and the closure judge's keep have no producer,
// so each carries an end: -until, else WaitDefaultEnd from when it was set.
// Nothing happens to a Work at its end. Once a day at most, every Work whose
// wait has run out is listed for Brain in one event (one cheap wake), and Brain
// closes it, does the step, or sets a new wait with a time. Trigger Works are
// watchers and never listed; a question waits on the person's reply, which the
// runtime delivers, and has no end.
const (
	// WaitDefaultEnd is the end of a wait set without -until and of every keep
	// verdict: the judge's own rejudge interval and the batch interval, so a
	// shorter default would only land in the same daily event.
	WaitDefaultEnd = 24 * time.Hour
	// overdueEvery is the least time between two overdue-wait events.
	overdueEvery = 24 * time.Hour
	// overdueTick is how often the runtime looks for waits past their end.
	overdueTick = time.Minute
	// overdueListLimit bounds the event; the rest are counted.
	overdueListLimit = 12

	overdueWorkPrefix = "overdue-waits:"
	overdueEventKind  = "work.waits_overdue"
)

// withWaitEnd gives a named wait its end: until when given (it must be in the
// future), else the default end from now. Any other kind has a producer that
// ends it and takes no end.
func withWaitEnd(wake *WorkWake, until *time.Time, now time.Time) (*WorkWake, error) {
	if wake == nil {
		return nil, nil
	}
	out := cloneWorkWake(wake)
	if out.Kind != WorkWakeNamed {
		if until != nil {
			return nil, fmt.Errorf("%w: only a named wait (-wait-for) has an end; a %s wait ends when its producer answers", ErrWorkConflict, out.Kind)
		}
		out.Until = nil
		return out, nil
	}
	if until != nil {
		if !until.After(now) {
			return nil, fmt.Errorf("%w: -until %s is not in the future", ErrWorkConflict, until.Format(time.RFC3339))
		}
		at := until.UTC()
		out.Until = &at
		return out, nil
	}
	at := now.Add(WaitDefaultEnd).UTC()
	out.Until = &at
	return out, nil
}

// nextKept is a keep verdict at revision: judged again at the same revision it
// keeps its first time and its end; a new revision starts a new wait.
func nextKept(prior *WorkKept, reason string, revision uint64, at time.Time) *WorkKept {
	since, until := at, at.Add(WaitDefaultEnd)
	if prior != nil && prior.Revision == revision {
		if prior.Since != nil {
			since = *prior.Since
		} else {
			since = prior.At
		}
		if prior.Until != nil {
			until = *prior.Until
		}
	}
	return &WorkKept{Reason: reason, Revision: revision, At: at, Since: &since, Until: &until}
}

// OverdueWait is one Work whose wait has run out, as the event lists it.
type OverdueWait struct {
	WorkID   string `json:"work_id"`
	Title    string `json:"title"`
	WaitsFor string `json:"waits_for"`
	// SetBy is who set the wait: brain (-wait-for) or judge (a keep verdict).
	SetBy string    `json:"set_by"`
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	Age   string    `json:"waiting_for"`
}

// overdueWaits lists open Works whose named wait or keep verdict has ended,
// oldest wait first.
func (s *Service) overdueWaits(now time.Time) []OverdueWait {
	items, err := s.store.ListWork()
	if err != nil {
		return nil
	}
	var out []OverdueWait
	for _, item := range items {
		if entry, ok := s.overdueWait(item, now); ok {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

func (s *Service) overdueWait(item Work, now time.Time) (OverdueWait, bool) {
	if item.Status == WorkDone || item.Status == WorkCancelled || readOnceWork(item.ID) ||
		(s.standing != nil && s.standing(item.ID)) {
		return OverdueWait{}, false
	}
	st, err := s.store.fsmState(item.ID)
	if err != nil || st == nil || st.Status.Terminal() || st.Attempt != nil || st.ActiveAdmission() != nil {
		return OverdueWait{}, false
	}
	entry := OverdueWait{WorkID: item.ID, Title: judgeClip(item.Title, 120)}
	switch {
	case st.Wake != nil:
		if st.Wake.Kind != lifecycle.WakeNamed || st.Wake.Until == nil {
			return OverdueWait{}, false
		}
		ref := st.Wake.Ref
		if ref == "operator:"+item.ID {
			ref = "(nothing named)"
		}
		entry.WaitsFor, entry.SetBy, entry.Since, entry.Until = ref, "brain", st.Wake.Since, *st.Wake.Until
	case item.Kept != nil && item.Kept.Revision == st.Revision && item.Kept.Until != nil && strings.TrimSpace(item.Question) == "":
		// A keep holds only for the revision it judged: any lifecycle change
		// since (a result, Brain's update) retired it.
		since := item.Kept.At
		if item.Kept.Since != nil {
			since = *item.Kept.Since
		}
		entry.WaitsFor, entry.SetBy, entry.Since, entry.Until = item.Kept.Reason, "judge", since, *item.Kept.Until
	default:
		return OverdueWait{}, false
	}
	if now.Before(entry.Until) {
		return OverdueWait{}, false
	}
	entry.WaitsFor = judgeClip(entry.WaitsFor, 300)
	entry.Age = waitAge(now.Sub(entry.Since))
	return entry, true
}

func waitAge(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// RunWaitEnds gives waits that predate their ends the default end once, then
// looks each minute for waits past their end until ctx ends.
func (s *Service) RunWaitEnds(ctx context.Context) {
	if err := s.backfillWaitEnds(); err != nil {
		log.Printf("brain wait ends backfill: %v", err)
	}
	go s.runGrantAnswers(ctx)
	ticker := time.NewTicker(overdueTick)
	defer ticker.Stop()
	for {
		if err := s.raiseOverdueWaits(); err != nil {
			log.Printf("brain overdue waits: %v", err)
		}
		s.raiseNoteCompactionsDue()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// backfillWaitEnds gives every open wait without an end (set before ends
// existed) the default end from now. It writes the end, so a later start finds
// it set and changes nothing: the upgrade raises no event of its own.
func (s *Service) backfillWaitEnds() error {
	items, err := s.store.ListWork()
	if err != nil {
		return err
	}
	now := s.nowUTC()
	until := now.Add(WaitDefaultEnd)
	for _, item := range items {
		if item.Status == WorkDone || item.Status == WorkCancelled || (s.standing != nil && s.standing(item.ID)) {
			continue
		}
		st, err := s.store.fsmState(item.ID)
		if err != nil || st == nil || st.Status.Terminal() {
			continue
		}
		if wake := st.Wake; wake != nil && wake.Kind == lifecycle.WakeNamed && wake.Until == nil && st.Attempt == nil {
			if _, err := s.store.fsm.SetWaitUntil(st.ID, wake.Kind, wake.Ref, &until); err != nil {
				return fmt.Errorf("Work %s: %w", item.ID, err)
			}
			if err := s.store.SyncWorkProjection(item.ID); err != nil {
				return err
			}
		}
		if kept := item.Kept; kept != nil && kept.Until == nil && kept.Revision == st.Revision {
			if err := s.store.setKeptEnd(item.ID, kept.Revision, until); err != nil {
				return err
			}
		}
	}
	return nil
}

// setKeptEnd gives a keep verdict that predates ends its end, keeping its time.
func (s *Store) setKeptEnd(workID string, revision uint64, until time.Time) error {
	return s.updateWorkProgressLocked(workID, func(item *Work) (*WorkEvent, bool) {
		kept := item.Kept
		if kept == nil || kept.Revision != revision || kept.Until != nil {
			return nil, false
		}
		next := *cloneWorkKept(kept)
		since := next.At
		next.Since, next.Until = &since, &until
		item.Kept = &next
		return nil, true
	})
}

// raiseOverdueWaits wakes Brain once for every Work whose wait has run out: at
// most one such event per overdueEvery, none when nothing is overdue. The
// event lives on its own Work (overdue-waits:<time>), whose creation time is
// the durable record of the last raise, so a restart neither repeats it nor
// loses one half made.
func (s *Service) raiseOverdueWaits() error {
	if s == nil || s.store == nil {
		return nil
	}
	now := s.nowUTC()
	last, found, err := s.lastOverdueBatch()
	if err != nil {
		return err
	}
	if found && now.Sub(last.CreatedAt) < overdueEvery {
		if last.Status != WorkDone && last.Status != WorkCancelled {
			return s.deliverOverdueBatch(last.ID, now) // finishes one a restart cut short
		}
		return nil
	}
	if len(s.overdueWaits(now)) == 0 {
		return nil
	}
	if found && last.Status != WorkDone && last.Status != WorkCancelled && !overdueBatchReading(s, last.ID) {
		// Yesterday's list was never read (an outage): today's replaces it.
		if _, err := s.CloseWork(WorkCloseRequest{
			WorkID: last.ID, ExpectedRevision: last.Revision, Status: WorkCancelled,
			Actor: lifecycle.ActorRuntime, Reason: "Replaced by a newer list of waits past their end.",
		}); err != nil && !errors.Is(err, ErrWorkCleanupPending) {
			return err
		}
	}
	id := overdueWorkPrefix + now.UTC().Format("20060102T150405Z")
	if _, _, err := s.store.EnsureWork(Work{
		ID: id, Title: "Waits past their end", CompletionPolicy: CompletionBounded,
		Objective:  "Decide each listed Work whose wait has run out.",
		NextAction: "For each Work listed: close it if it is finished (work update -status done), do the owed step now, or set a new wait with a time (-status waiting -wait-for \"<what>\" -until <time>).",
	}); err != nil {
		return err
	}
	return s.deliverOverdueBatch(id, now)
}

// deliverOverdueBatch records the list on the batch Work and opens its Review,
// each step idempotent. A batch with nothing left to list is closed.
func (s *Service) deliverOverdueBatch(id string, now time.Time) error {
	events, err := s.store.ListWorkEvents(id)
	if err != nil {
		return err
	}
	var recorded *WorkEvent
	for index := range events {
		if events[index].DedupeKey == id {
			recorded = &events[index]
		}
	}
	if recorded == nil {
		overdue := s.overdueWaits(now)
		if len(overdue) == 0 {
			item, err := s.store.Work(id)
			if err != nil {
				return err
			}
			_, err = s.CloseWork(WorkCloseRequest{WorkID: id, ExpectedRevision: item.Revision, Status: WorkCancelled,
				Actor: lifecycle.ActorRuntime, Reason: "No wait is past its end any more."})
			if errors.Is(err, ErrWorkCleanupPending) {
				err = nil
			}
			return err
		}
		listed := overdue
		if len(listed) > overdueListLimit {
			listed = listed[:overdueListLimit]
		}
		details, err := json.Marshal(overdueBrief{Overdue: listed, More: len(overdue) - len(listed)})
		if err != nil {
			return err
		}
		event, _, err := s.store.AppendWorkEvent(WorkEvent{
			WorkID: id, Kind: overdueEventKind, DedupeKey: id, SourceName: "Work wait ends",
			Summary:     overdueSummary(overdue),
			DetailsJSON: string(details), PayloadRef: "mewla brain work list --json", Actionable: true,
		})
		if err != nil {
			return err
		}
		recorded = &event
	}
	if recorded.HandledAt != nil || recorded.DiscardedAt != nil {
		return nil
	}
	st, err := s.store.fsmState(id)
	if err != nil || st == nil || st.Status.Terminal() || (st.Review != nil && st.Review.EventID == recorded.ID) {
		return err
	}
	if st.Review != nil {
		return nil // an earlier event is with Brain
	}
	if _, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(id), overdueEventKind, recorded.ID, recorded.ID); err != nil {
		return err
	}
	if err := s.store.SyncWorkProjection(id); err != nil {
		return err
	}
	_, err = s.ReconcileHostLane()
	return err
}

// overdueBrief is the list Brain reads in the event (overdue_waits).
type overdueBrief struct {
	Overdue []OverdueWait `json:"overdue"`
	More    int           `json:"more,omitempty"`
}

func overdueSummary(overdue []OverdueWait) string {
	noun := "Works have"
	if len(overdue) == 1 {
		noun = "Work has"
	}
	return fmt.Sprintf("%d %s waited past the end of their wait. For each: close it if it is finished, do the owed step now, or set a new wait with a time.", len(overdue), noun)
}

// lastOverdueBatch is the newest overdue-waits Work.
func (s *Service) lastOverdueBatch() (Work, bool, error) {
	items, err := s.store.ListWork()
	if err != nil {
		return Work{}, false, err
	}
	var last Work
	found := false
	for _, item := range items {
		if strings.HasPrefix(item.ID, overdueWorkPrefix) && (!found || item.CreatedAt.After(last.CreatedAt)) {
			last, found = item, true
		}
	}
	return last, found, nil
}

// overdueBatchReading says Brain is reading the batch right now.
func overdueBatchReading(s *Service, id string) bool {
	st, err := s.store.fsmState(id)
	if err != nil || st == nil || st.Review == nil || st.Review.Handler == nil {
		return false
	}
	return st.Review.Handler.EndedAt == nil
}

func overdueDetails(kind, details string) json.RawMessage {
	if kind != overdueEventKind || !json.Valid([]byte(details)) {
		return nil
	}
	return json.RawMessage(strings.TrimSpace(details))
}
