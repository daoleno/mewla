package work

import (
	"testing"
	"time"
)

func TestFilterCalendarWorkItemsKeepsCalendarActionsOnly(t *testing.T) {
	items := []*Item{
		nil,
		{Frontmatter: Frontmatter{ID: "other", Kind: "task"}},
		{Frontmatter: Frontmatter{ID: "calendar", Kind: " calendar_action "}},
	}

	filtered := FilterCalendarWorkItems(items)
	if len(filtered) != 1 || filtered[0].Frontmatter.ID != "calendar" {
		t.Fatalf("filtered = %#v, want only Calendar action", filtered)
	}
	if IsCalendarWorkItem(nil) || IsCalendarWorkItem(items[1]) || !IsCalendarWorkItem(items[2]) {
		t.Fatalf("Calendar predicate disagrees with filtered items")
	}
}

func TestInUseCalendarWorkItemsDropsFinishedUnreachableRuns(t *testing.T) {
	done := time.Now()
	items := []*Item{
		{ID: "other", Frontmatter: Frontmatter{ID: "other", Kind: "task"}},
		{ID: "running", Frontmatter: Frontmatter{Kind: "calendar_action"}},
		{ID: "linked", Frontmatter: Frontmatter{Kind: "calendar_action", Done: &done}},
		{ID: "live", Frontmatter: Frontmatter{Kind: "calendar_action", Done: &done, WorkerSession: "%7"}},
		{ID: "old", Frontmatter: Frontmatter{Kind: "calendar_action", Done: &done, WorkerSession: "%3"}},
		{ID: "older", Frontmatter: Frontmatter{Kind: "calendar_action", Done: &done}},
	}

	got := InUseCalendarWorkItems(items, map[string]bool{"linked": true}, map[string]bool{"%7": true})
	ids := []string{}
	for _, item := range got {
		ids = append(ids, item.ID)
	}
	if len(ids) != 3 || ids[0] != "running" || ids[1] != "linked" || ids[2] != "live" {
		t.Fatalf("in-use calendar Work = %v, want [running linked live]", ids)
	}
}
