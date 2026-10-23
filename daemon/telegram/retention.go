package telegram

import (
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// Retention of the projection checkpoints.
//
// Projection, TopicProjection, TopicMessages and WorkMessages remember, per
// projected message, which digest Telegram already shows and which Telegram
// message to edit. They are consulted only for timeline items the projection
// still visits, so the projection and the pruning share one rule: an item is
// retained when it is newer than the delivery boundary and either among the
// newest projectionKeepNewest of its stream or younger than projectionKeepFor.
// The projection skips every item that is not retained and the pruning removes
// exactly the checkpoints of those items, so a pruned checkpoint is never one
// the projection would act on again (it would re-send the message).
//
// Pruning runs inside the mutation that grows the maps, so the deletions commit
// in the same transaction as the write that made them necessary.
const (
	// projectionKeepNewest is how many of the newest items of one stream (the
	// Brain thread, or one Session) keep their checkpoints whatever their age.
	projectionKeepNewest = 500
	// projectionKeepFor is how long an item keeps its checkpoints, whatever
	// the count: a message younger than this can still be edited or answered.
	projectionKeepFor = 14 * 24 * time.Hour
)

const (
	brainMessagePrefix = "outbox:assistant:"
	workKeyPrefix      = "work:"
	topicMessagePrefix = "topic:msg:"
	privateMsgPrefix   = "private:msg:"
	topicMarkPrefix    = "topic:mark:"
	topicLifePrefix    = "topic:life:"
)

// editableKey reports a checkpoint whose Telegram message is edited as the
// source revises it. Lifecycle markers and notices are sent once, so their
// message ids are never read back and are not kept.
func editableKey(key string) bool {
	return strings.HasPrefix(key, brainMessagePrefix) || strings.HasPrefix(key, topicMessagePrefix) || strings.HasPrefix(key, privateMsgPrefix)
}

// retainedItems says, for each created time, whether the item stays projected.
func retainedItems(created []time.Time, boundary, now time.Time) []bool {
	order := make([]int, len(created))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(a, b int) bool { return created[order[a]].After(created[order[b]]) })
	cutoff := now.Add(-projectionKeepFor)
	retained := make([]bool, len(created))
	for rank, index := range order {
		if !boundary.IsZero() && !created[index].After(boundary) {
			continue
		}
		retained[index] = rank < projectionKeepNewest || created[index].After(cutoff)
	}
	return retained
}

// protectedKeys are the checkpoints an outbox row still refers to, in any
// state: a pending, in-flight or ambiguous row is delivered or resolved
// against them, and a failed one decides on them whether to retry.
func protectedKeys(state *durableState) map[string]struct{} {
	keys := make(map[string]struct{}, len(state.Outbox))
	for _, row := range state.Outbox {
		if row.TopicKey != "" {
			keys[row.TopicKey] = struct{}{}
		}
		if row.WorkID != "" {
			keys[workKeyPrefix+row.WorkID] = struct{}{}
		}
	}
	return keys
}

// checkpointItem returns the item id of a message checkpoint
// prefix+itemID+":"+chunk, or false when key is not one.
func checkpointItem(key, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(key, prefix)
	if !ok {
		return "", false
	}
	cut := strings.LastIndex(rest, ":")
	if cut < 0 {
		return rest, true
	}
	return rest[:cut], true
}

// brainRetention is the part of the Brain thread the projection still visits.
type brainRetention struct {
	items map[string]bool // assistant item ids
	works map[string]bool // Work ids of retained Work cards
	// known is false when the timeline held nothing to project; nothing is
	// then pruned, because an empty read proves nothing about old checkpoints.
	known bool
	// retained is indexed like the timeline.
	retained []bool
}

func projectableBrainItem(item brain.TimelineItem) bool {
	return item.Kind == "assistant_message" || (item.Role == "assistant" && item.Kind == "") || (item.Kind == "work_card" && item.WorkID != "")
}

func planBrainRetention(items []brain.TimelineItem, boundary, now time.Time) brainRetention {
	plan := brainRetention{items: map[string]bool{}, works: map[string]bool{}, retained: make([]bool, len(items))}
	var indexes []int
	var created []time.Time
	for index, item := range items {
		if projectableBrainItem(item) {
			indexes = append(indexes, index)
			created = append(created, item.CreatedAt)
		}
	}
	if len(indexes) == 0 {
		return plan
	}
	plan.known = true
	for position, keep := range retainedItems(created, boundary, now) {
		if !keep {
			continue
		}
		item := items[indexes[position]]
		plan.retained[indexes[position]] = true
		if item.Kind == "work_card" {
			plan.works[item.WorkID] = true
		} else {
			plan.items[item.ID] = true
		}
	}
	return plan
}

// pruneBrainCheckpoints removes the Brain thread's checkpoints of items the
// plan no longer retains.
func pruneBrainCheckpoints(state *durableState, plan brainRetention) {
	if !plan.known {
		return
	}
	protected := protectedKeys(state)
	for _, table := range []map[string]string{state.Projection, state.TopicProjection} {
		for key := range table {
			if dropBrainKey(key, plan, protected) {
				delete(table, key)
			}
		}
	}
	for key := range state.TopicMessages {
		if dropBrainKey(key, plan, protected) {
			delete(state.TopicMessages, key)
		}
	}
	for workID := range state.WorkMessages {
		if _, held := protected[workKeyPrefix+workID]; !held && !plan.works[workID] {
			delete(state.WorkMessages, workID)
		}
	}
}

func dropBrainKey(key string, plan brainRetention, protected map[string]struct{}) bool {
	if _, held := protected[key]; held {
		return false
	}
	if id, ok := checkpointItem(key, brainMessagePrefix); ok {
		return !plan.items[id]
	}
	if workID, ok := strings.CutPrefix(key, workKeyPrefix); ok {
		return !plan.works[workID]
	}
	return false
}

// sessionRetention says which of a Session's assistant items stay projected
// (live) and which item ids this read showed at all (seen).
func sessionRetention(items []brain.SessionAssistantItem, boundary, now time.Time) (live, seen map[string]bool) {
	var ids []string
	var created []time.Time
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
			created = append(created, item.CreatedAt)
		}
	}
	live = make(map[string]bool, len(ids))
	seen = make(map[string]bool, len(ids))
	for position, keep := range retainedItems(created, boundary, now) {
		seen[ids[position]] = true
		if keep {
			live[ids[position]] = true
		}
	}
	return live, seen
}

// pruneSessionCheckpoints removes one Session's message checkpoints of items
// this read showed and no longer retains. A checkpoint of an item the read did
// not show stays: the provider transcript behind the read can be missing or
// bound to the wrong conversation for a moment, and an absent item proves
// nothing about whether the projection will meet it again.
func pruneSessionCheckpoints(state *durableState, sessionID string, live, seen map[string]bool) {
	protected := protectedKeys(state)
	drop := func(key string) bool {
		if _, held := protected[key]; held {
			return false
		}
		for _, prefix := range []string{topicMessagePrefix + sessionID + ":", privateMsgPrefix + sessionID + ":"} {
			if id, ok := checkpointItem(key, prefix); ok {
				return seen[id] && !live[id]
			}
		}
		return false
	}
	for key := range state.TopicProjection {
		if drop(key) {
			delete(state.TopicProjection, key)
		}
	}
	for key := range state.TopicMessages {
		if drop(key) {
			delete(state.TopicMessages, key)
		}
	}
}

// pruneSessionMarkers removes the lifecycle markers of a Session's earlier
// Turns and Works. The projection reports only the current Turn and Work, so
// a marker of an earlier one is never compared again. SessionProjection leaves
// an ID empty when it could not read the Turn or the Work, and a marker's key
// does not say which of the two it belongs to, so nothing is pruned unless both
// are known: a marker pruned on a half-empty read would be sent again.
func pruneSessionMarkers(state *durableState, sessionID, turnID, workID string) {
	if turnID == "" || workID == "" {
		return
	}
	current := []string{topicMarkPrefix + sessionID + ":" + turnID + ":", topicMarkPrefix + sessionID + ":" + workID + ":"}
	protected := protectedKeys(state)
	own := topicMarkPrefix + sessionID + ":"
	for key := range state.TopicProjection {
		if !strings.HasPrefix(key, own) {
			continue
		}
		if _, held := protected[key]; held {
			continue
		}
		if !strings.HasPrefix(key, current[0]) && !strings.HasPrefix(key, current[1]) {
			delete(state.TopicProjection, key)
		}
	}
}

// pruneOrphanCheckpoints removes the Session checkpoints of Sessions Telegram
// no longer has a topic, operation or fallback for, and the message ids kept
// for keys that are never edited.
func pruneOrphanCheckpoints(state *durableState) {
	known := map[string]struct{}{}
	var colon []string
	add := func(sessionID string) {
		if sessionID == "" {
			return
		}
		known[sessionID] = struct{}{}
		if strings.Contains(sessionID, ":") {
			colon = append(colon, sessionID)
		}
	}
	for _, mapping := range state.Topics {
		add(mapping.SessionID)
	}
	for _, op := range state.TopicOps {
		add(op.SessionID)
	}
	add(state.FallbackSessionID)
	orphan := func(key string) bool {
		for _, prefix := range []string{topicMessagePrefix, privateMsgPrefix, topicMarkPrefix, topicLifePrefix} {
			rest, ok := strings.CutPrefix(key, prefix)
			if !ok {
				continue
			}
			head, _, _ := strings.Cut(rest, ":")
			if _, ok := known[head]; ok {
				return false
			}
			for _, sessionID := range colon {
				if strings.HasPrefix(rest, sessionID+":") {
					return false
				}
			}
			return true
		}
		return false
	}
	protected := protectedKeys(state)
	for key := range state.TopicProjection {
		if _, held := protected[key]; !held && orphan(key) {
			delete(state.TopicProjection, key)
		}
	}
	for key := range state.TopicMessages {
		if _, held := protected[key]; !editableKey(key) || (!held && orphan(key)) {
			delete(state.TopicMessages, key)
		}
	}
}
