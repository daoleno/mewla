package brain

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// A Brain note over its budget (workspaceNoteBudgets) is archived as it is
// and Brain gets one Work to compact it: the compaction is a judgment about
// what still matters, so Brain does it, from a copy nothing can lose. The
// note itself is never cut or rewritten here.
const (
	noteCompactionPrefix = "note-compaction:"
	noteCompactionKind   = "note_over_budget"
	// noteCompactionEvery is the least time between two compaction Works for
	// one note.
	noteCompactionEvery = 24 * time.Hour
	// noteBudgetCheckEvery is how often the note sizes are looked at.
	noteBudgetCheckEvery = time.Hour
)

// NoteArchiveDir is where over-budget notes are archived before Brain
// compacts them, under the worklog.
func (s *Store) NoteArchiveDir() string { return filepath.Join(s.WorklogPath(), "note-archive") }

// raiseNoteCompactionsDue checks the notes at most once per
// noteBudgetCheckEvery (the first call checks).
func (s *Service) raiseNoteCompactionsDue() {
	now := s.nowUTC()
	s.noteCheckMu.Lock()
	due := s.noteCheckedAt.IsZero() || now.Sub(s.noteCheckedAt) >= noteBudgetCheckEvery
	if due {
		s.noteCheckedAt = now
	}
	s.noteCheckMu.Unlock()
	if !due {
		return
	}
	if err := s.raiseNoteCompactions(); err != nil {
		log.Printf("brain note budgets: %v", err)
	}
}

// raiseNoteCompactions opens one compaction Work for each note over its
// budget that has none open and had none in the last noteCompactionEvery.
// The existing Works are the durable record, so a restart raises no second.
func (s *Service) raiseNoteCompactions() error {
	if s == nil || s.store == nil {
		return nil
	}
	notes, err := s.store.WorkspaceNotes()
	if err != nil {
		return err
	}
	var items []Work
	for _, note := range notes {
		if !note.OverBudget {
			continue
		}
		if items == nil {
			if items, err = s.store.ListWork(); err != nil {
				return err
			}
		}
		now := s.nowUTC()
		if last, found := lastNoteCompaction(items, note.Path); found {
			open := last.Status != WorkDone && last.Status != WorkCancelled
			if open || now.Sub(last.CreatedAt) < noteCompactionEvery {
				continue
			}
		}
		if err := s.raiseNoteCompaction(note, now); err != nil {
			return fmt.Errorf("%s: %w", note.Path, err)
		}
	}
	return nil
}

func lastNoteCompaction(items []Work, notePath string) (Work, bool) {
	prefix := noteCompactionPrefix + notePath + ":"
	var last Work
	found := false
	for _, item := range items {
		if strings.HasPrefix(item.ID, prefix) && (!found || item.CreatedAt.After(last.CreatedAt)) {
			last, found = item, true
		}
	}
	return last, found
}

// raiseNoteCompaction archives the note, then opens its Work and the review
// that brings it to Brain.
func (s *Service) raiseNoteCompaction(note WorkspaceNote, now time.Time) error {
	stamp := now.UTC().Format("20060102T150405Z")
	raw, err := os.ReadFile(filepath.Join(s.store.WorkspacePath(), note.Path))
	if err != nil {
		return err
	}
	archive := filepath.Join(s.store.NoteArchiveDir(), stamp+"-"+filepath.Base(note.Path))
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(archive, raw, 0o600); err != nil {
		return err
	}
	id := noteCompactionPrefix + note.Path + ":" + stamp
	budget := note.BudgetBytes >> 10
	if _, _, err := s.store.EnsureWork(Work{
		ID: id, Title: "Compact " + note.Path, CompletionPolicy: CompletionBounded,
		Objective:  fmt.Sprintf("Bring %s under its %d KiB budget (now %d KiB) without losing what still matters; the full version is archived at %s.", note.Path, budget, int64(len(raw))>>10, archive),
		NextAction: fmt.Sprintf("Rewrite %s: drop finished, stale or superseded entries; Work, Event and worklog keep the history.", note.Path),
	}); err != nil {
		return err
	}
	recorded, _, err := s.store.AppendWorkEvent(WorkEvent{
		WorkID: id, Kind: noteCompactionKind, DedupeKey: id, SourceName: "Brain note budgets",
		Summary:    fmt.Sprintf("%s is %d bytes, over its %d-byte budget; archived as it was.", note.Path, len(raw), note.BudgetBytes),
		PayloadRef: archive, Actionable: true,
	})
	if err != nil {
		return err
	}
	if recorded.HandledAt != nil || recorded.DiscardedAt != nil {
		return nil
	}
	st, err := s.store.fsmState(id)
	if err != nil || st == nil || st.Status.Terminal() || st.Review != nil {
		return err
	}
	if _, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(id), noteCompactionKind, recorded.ID, recorded.ID); err != nil {
		return err
	}
	if err := s.store.SyncWorkProjection(id); err != nil {
		return err
	}
	_, err = s.ReconcileHostLane()
	return err
}
