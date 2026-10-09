package work

import "strings"

func IsCalendarWorkItem(item *Item) bool {
	return item != nil && strings.TrimSpace(item.Frontmatter.Kind) == "calendar_action"
}

func FilterCalendarWorkItems(items []*Item) []*Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]*Item, 0, len(items))
	for _, item := range items {
		if IsCalendarWorkItem(item) {
			out = append(out, item)
		}
	}
	return out
}

// InUseCalendarWorkItems keeps the calendar Work a client can still reach:
// unfinished Work, the Work a calendar item links to, and Work tied to a live
// worker session. Each finished run leaves a Work file holding its full
// result, so the rest only grows the client snapshot run after run.
func InUseCalendarWorkItems(items []*Item, linkedWorkIDs, liveSessions map[string]bool) []*Item {
	out := make([]*Item, 0, len(items))
	for _, item := range FilterCalendarWorkItems(items) {
		session := strings.TrimSpace(item.Frontmatter.WorkerSession)
		if item.Frontmatter.Done == nil ||
			linkedWorkIDs[item.ID] ||
			(session != "" && liveSessions[session]) {
			out = append(out, item)
		}
	}
	return out
}
