package brain

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

// Where a file system records no reads (gc.SweepUnmarked cannot prove an old
// unmarked t/ item unread), the collector neither takes it nor keeps it for
// ever in silence: it puts the item to review (gc.SyncReviews) and Brain, the
// agent that owns t/, is asked once, in one Work, whether to keep it
// (`mewla gc keep`) or release it (`mewla gc release`). The Work lists the
// biggest first; the whole list is in a file beside it. It is raised only
// when an item is new to review and a day has passed since the last asking
// (gc.ReviewsDue), so the same set is never asked twice. A Work that is still
// open gets the new list as an Event instead of a second Work. Nothing is
// ever taken without an answer.
const (
	tReviewPrefix = "t-review:"
	tReviewKind   = "t_review"
	// tReviewListed is how many items the Work names in full.
	tReviewListed = 20
)

// TReviewDir is where the full list of each asking is kept, under the worklog.
func (s *Store) TReviewDir() string { return filepath.Join(s.WorklogPath(), "t-review") }

// raiseTReview asks Brain about the items in review when they are due.
func (s *Service) raiseTReview() error {
	if s == nil || s.store == nil {
		return nil
	}
	root := filepath.Dir(s.store.Root)
	now := s.nowUTC()
	open, due := gc.ReviewsDue(root, now)
	if !due {
		return nil
	}
	stamp := now.UTC().Format("20060102T150405Z")
	list := tReviewList(open, len(open))
	file := filepath.Join(s.store.TReviewDir(), stamp+".txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(list), 0o600); err != nil {
		return err
	}
	var total int64
	for _, r := range open {
		total += r.Bytes
	}
	items, err := s.store.ListWork()
	if err != nil {
		return err
	}
	id, key := tReviewPrefix+stamp, tReviewPrefix+stamp
	if last, found := lastTReview(items); found && last.Status != WorkDone && last.Status != WorkCancelled {
		id, key = last.ID, last.ID+":"+stamp
	} else if _, _, err := s.store.EnsureWork(Work{
		ID: id, Title: "Review what t/ holds", CompletionPolicy: CompletionBounded,
		Objective: fmt.Sprintf("%d old items in t/ (%s) hold files that cannot be proven unread: this file system records no reads, so Mewla neither takes them nor knows they are unused. Decide each one.\n\n%s\nThe whole list: %s",
			len(open), formatReviewBytes(total), tReviewList(open, tReviewListed), file),
		NextAction: "For each item, from your host: `mewla gc keep PATH --until DATE --why REASON` (30 days at most; it goes to the trash then) or `mewla gc release PATH` (to the trash for 3 days now). An item you leave stays listed; nothing is taken without an answer.",
	}); err != nil {
		return err
	}
	recorded, _, err := s.store.AppendWorkEvent(WorkEvent{
		WorkID: id, Kind: tReviewKind, DedupeKey: key, SourceName: "t/ review",
		Summary:    fmt.Sprintf("%d items in t/ (%s) wait for a keep or a release.", len(open), formatReviewBytes(total)),
		PayloadRef: file, Actionable: true,
	})
	if err != nil {
		return err
	}
	paths := make([]string, len(open))
	for i, r := range open {
		paths[i] = r.Path
	}
	gc.MarkReviewsAsked(root, paths, now)
	if recorded.HandledAt != nil || recorded.DiscardedAt != nil {
		return nil
	}
	st, err := s.store.fsmState(id)
	if err != nil || st == nil || st.Status.Terminal() || st.Review != nil {
		return err
	}
	if _, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(id), tReviewKind, recorded.ID, recorded.ID); err != nil {
		return err
	}
	if err := s.store.SyncWorkProjection(id); err != nil {
		return err
	}
	_, err = s.ReconcileHostLane()
	return err
}

func lastTReview(items []Work) (Work, bool) {
	var last Work
	found := false
	for _, item := range items {
		if strings.HasPrefix(item.ID, tReviewPrefix) && (!found || item.CreatedAt.After(last.CreatedAt)) {
			last, found = item, true
		}
	}
	return last, found
}

// tReviewList writes the first n items, biggest first, one per line: size,
// last write, who made it, what it looks like, and whether it was there
// before the sweep rule. The rest are one line.
func tReviewList(open []gc.Review, n int) string {
	var b strings.Builder
	for i, r := range open {
		if i == n {
			var rest int64
			for _, more := range open[i:] {
				rest += more.Bytes
			}
			fmt.Fprintf(&b, "and %d more, %s together\n", len(open)-i, formatReviewBytes(rest))
			break
		}
		parts := []string{formatReviewBytes(r.Bytes), "last written " + r.LastWrite.Local().Format("2006-01-02")}
		if r.Creator != "" {
			parts = append(parts, "made by "+r.Creator)
		}
		if r.Looks != "" {
			parts = append(parts, r.Looks)
		}
		if r.PreRule {
			parts = append(parts, "there before the t/ rule")
		}
		fmt.Fprintf(&b, "- %s: %s\n", r.Path, strings.Join(parts, "; "))
	}
	return b.String()
}

func formatReviewBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

// raiseTReviewDue is raiseTReview as the hourly pass calls it.
func (s *Service) raiseTReviewDue() {
	if err := s.raiseTReview(); err != nil {
		log.Printf("brain t/ review: %v", err)
	}
}
