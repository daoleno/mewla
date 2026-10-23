package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// checkpointFor is the digest the projection stores for a one-chunk body.
func checkpointFor(body string) string {
	return digestRichText(chunkRichText(renderMarkdown(body), maxMessageText)[0])
}

func TestRetainedItemsKeepsNewestAndRecent(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	old := now.Add(-2 * projectionKeepFor)
	created := make([]time.Time, projectionKeepNewest+10)
	for i := range created {
		created[i] = old.Add(time.Duration(i) * time.Second)
	}
	created = append(created, now.Add(-time.Hour)) // recent: kept whatever its rank
	got := retainedItems(created, time.Time{}, now)
	kept := 0
	for _, keep := range got {
		if keep {
			kept++
		}
	}
	// The newest projectionKeepNewest of all (the recent one among them) stay.
	if kept != projectionKeepNewest {
		t.Fatalf("kept %d, want %d", kept, projectionKeepNewest)
	}
	if !got[len(got)-1] || got[0] {
		t.Fatalf("recent=%v oldest=%v", got[len(got)-1], got[0])
	}

	recent := []time.Time{now.Add(-time.Hour), now.Add(-2 * time.Hour)}
	if r := retainedItems(recent, now.Add(-90*time.Minute), now); !r[0] || r[1] {
		t.Fatalf("the boundary must exclude items at or before it: %v", r)
	}
}

func projectWithClock(t *testing.T, manager *Manager, now time.Time) {
	t.Helper()
	manager.now = func() time.Time { return now }
	if err := manager.projectTimeline(); err != nil {
		t.Fatal(err)
	}
}

// Pruning a checkpoint the projection would still act on re-sends a message.
// After pruning, projecting the same timeline again enqueues nothing.
func TestBrainCheckpointsArePrunedWithoutResendingAnything(t *testing.T) {
	manager, fake, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	start := manager.now()
	total := projectionKeepNewest + 40
	if err := manager.store.mutate(func(state *durableState) error {
		for i := 0; i < total; i++ {
			body := fmt.Sprintf("answer %d", i)
			fake.timeline = append(fake.timeline, brain.TimelineItem{ID: fmt.Sprintf("a%d", i), Role: "assistant", Kind: "assistant_message", Body: body,
				CreatedAt: start.Add(time.Duration(i+1) * time.Second)})
			key := fmt.Sprintf("outbox:assistant:a%d:0", i)
			state.Projection[key] = checkpointFor(body)
			state.TopicProjection[key] = checkpointFor(body)
			state.TopicMessages[key] = int64(1000 + i)
		}
		// Work cards: one still retained, one whose cards fell out.
		state.Projection["work:old"], state.WorkMessages["old"] = "d", 5
		state.Projection["work:new"], state.WorkMessages["new"] = "d", 6
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fake.timeline = append(fake.timeline,
		brain.TimelineItem{ID: "w-old", Role: "assistant", Kind: "work_card", WorkID: "old", Body: "x", CreatedAt: start.Add(time.Second / 2)},
		brain.TimelineItem{ID: "w-new", Role: "assistant", Kind: "work_card", WorkID: "new", Body: "x", CreatedAt: start.Add(time.Duration(total+5) * time.Second)})

	// A month later every item is old; only the newest projectionKeepNewest stay.
	projectWithClock(t, manager, start.Add(30*24*time.Hour))

	state := manager.store.snapshot()
	// The newest projectionKeepNewest projectable items include the newest
	// Work card, so one fewer answer than that stays.
	if got := len(state.Projection); got != projectionKeepNewest { // answers + work:new
		t.Fatalf("Projection = %d, want %d", got, projectionKeepNewest)
	}
	if len(state.TopicProjection) != projectionKeepNewest-1 || len(state.TopicMessages) != projectionKeepNewest-1 {
		t.Fatalf("TopicProjection=%d TopicMessages=%d, want %d", len(state.TopicProjection), len(state.TopicMessages), projectionKeepNewest-1)
	}
	if _, ok := state.Projection["work:old"]; ok {
		t.Fatal("the checkpoint of a Work whose cards are gone survived")
	}
	if _, ok := state.WorkMessages["old"]; ok || state.WorkMessages["new"] != 6 {
		t.Fatalf("WorkMessages = %v", state.WorkMessages)
	}
	if _, ok := state.Projection["outbox:assistant:a0:0"]; ok {
		t.Fatal("the oldest answer kept its checkpoint")
	}
	if _, ok := state.Projection[fmt.Sprintf("outbox:assistant:a%d:0", total-1)]; !ok {
		t.Fatal("the newest answer lost its checkpoint")
	}
	rows := len(state.Outbox)

	// Projecting again must neither send what was pruned nor change anything.
	projectWithClock(t, manager, start.Add(30*24*time.Hour+time.Minute))
	again := manager.store.snapshot()
	if len(again.Outbox) != rows || len(again.Projection) != len(state.Projection) {
		t.Fatalf("a second pass changed state: outbox %d -> %d, projection %d -> %d", rows, len(again.Outbox), len(state.Projection), len(again.Projection))
	}
	for _, row := range again.Outbox {
		if row.CanonicalID == "a0" || row.WorkID == "old" {
			t.Fatalf("pruned item was re-sent: %+v", row)
		}
	}
}

// A row that is waiting for delivery, an edit in flight or an ambiguous send
// still needs its checkpoint, whatever the age of the item behind it.
func TestPruningNeverDropsACheckpointAnOutboxRowStillUses(t *testing.T) {
	manager, fake, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	start := manager.now()
	total := projectionKeepNewest + 5
	if err := manager.store.mutate(func(state *durableState) error {
		for i := 0; i < total; i++ {
			fake.timeline = append(fake.timeline, brain.TimelineItem{ID: fmt.Sprintf("a%d", i), Role: "assistant", Kind: "assistant_message", Body: "b",
				CreatedAt: start.Add(time.Duration(i+1) * time.Second)})
			key := fmt.Sprintf("outbox:assistant:a%d:0", i)
			state.Projection[key], state.TopicProjection[key], state.TopicMessages[key] = "d", "d", int64(i+1)
		}
		state.Projection["work:held"], state.WorkMessages["held"] = "d", 9
		state.Outbox = append(state.Outbox,
			outboxRecord{ID: "r1", Kind: "edit", State: "pending", TopicKey: "outbox:assistant:a0:0", CreatedAt: start},
			outboxRecord{ID: "r2", Kind: "send", State: "ambiguous", TopicKey: "outbox:assistant:a1:0", CreatedAt: start},
			outboxRecord{ID: "r3", Kind: "edit", State: "dispatching", WorkID: "held", CreatedAt: start})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projectWithClock(t, manager, start.Add(60*24*time.Hour))
	state := manager.store.snapshot()
	for _, key := range []string{"outbox:assistant:a0:0", "outbox:assistant:a1:0"} {
		if _, ok := state.Projection[key]; !ok || state.TopicMessages[key] == 0 {
			t.Fatalf("%s lost its checkpoint while an outbox row used it", key)
		}
	}
	if _, ok := state.Projection["work:held"]; !ok || state.WorkMessages["held"] != 9 {
		t.Fatal("a Work with an in-flight edit lost its message mapping")
	}
	if _, ok := state.Projection["outbox:assistant:a2:0"]; ok {
		t.Fatal("an unreferenced old checkpoint survived")
	}
}

// An empty timeline proves nothing about old checkpoints.
func TestEmptyTimelineDoesNotPrune(t *testing.T) {
	manager, _, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	if err := manager.store.mutate(func(state *durableState) error {
		state.Projection["outbox:assistant:gone:0"] = "d"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projectWithClock(t, manager, manager.now().Add(90*24*time.Hour))
	if _, ok := manager.store.snapshot().Projection["outbox:assistant:gone:0"]; !ok {
		t.Fatal("an empty timeline pruned checkpoints")
	}
}

func TestSessionCheckpointsAreRetainedPerSession(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	old := now.Add(-3 * projectionKeepFor)
	var items []brain.SessionAssistantItem
	state := newDurableState()
	for i := 0; i < projectionKeepNewest+3; i++ {
		id := fmt.Sprintf("e%d", i)
		items = append(items, brain.SessionAssistantItem{ID: id, CreatedAt: old.Add(time.Duration(i) * time.Second)})
		for _, prefix := range []string{"topic:msg:%1:", "private:msg:%1:", "topic:msg:%10:"} {
			key := prefix + id + ":0"
			state.TopicProjection[key], state.TopicMessages[key] = "d", 1
		}
	}
	state.Outbox = []outboxRecord{{ID: "held", State: "pending", TopicKey: "topic:msg:%1:e0:0"}}
	live, seen := sessionRetention(items, time.Time{}, now)
	if len(live) != projectionKeepNewest || len(seen) != len(items) {
		t.Fatalf("live = %d seen = %d", len(live), len(seen))
	}
	pruneSessionCheckpoints(&state, "%1", live, seen)
	count := func(prefix string) (n int) {
		for key := range state.TopicProjection {
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				n++
			}
		}
		return n
	}
	if got := count("topic:msg:%1:"); got != projectionKeepNewest+1 { // +1: the pending row's key
		t.Fatalf("topic:msg:%%1: = %d", got)
	}
	if got := count("private:msg:%1:"); got != projectionKeepNewest {
		t.Fatalf("private:msg:%%1: = %d", got)
	}
	// Another Session whose id merely starts the same is not touched.
	if got := count("topic:msg:%10:"); got != projectionKeepNewest+3 {
		t.Fatalf("topic:msg:%%10: = %d, a neighbouring Session was pruned", got)
	}
}

func TestMarkersOfEarlierTurnsArePruned(t *testing.T) {
	state := newDurableState()
	for _, key := range []string{
		"topic:mark:%1:turn:a:running", "topic:mark:%1:turn:a:done",
		"topic:mark:%1:turn:b:running", "topic:mark:%1:wk1:done", "topic:mark:%1:wk0:done",
		"topic:mark:%2:turn:a:done", "topic:life:%1:revive",
	} {
		state.TopicProjection[key] = "d"
	}
	state.Outbox = []outboxRecord{{ID: "x", State: "pending", TopicKey: "topic:mark:%1:turn:a:running"}}
	pruneSessionMarkers(&state, "%1", "turn:b", "wk1")
	want := map[string]bool{
		"topic:mark:%1:turn:a:running": true, // an outbox row still uses it
		"topic:mark:%1:turn:b:running": true, "topic:mark:%1:wk1:done": true,
		"topic:mark:%2:turn:a:done": true, "topic:life:%1:revive": true,
	}
	if len(state.TopicProjection) != len(want) {
		t.Fatalf("left %v", state.TopicProjection)
	}
	for key := range want {
		if _, ok := state.TopicProjection[key]; !ok {
			t.Fatalf("%s was pruned", key)
		}
	}
	// A read that could not name the Turn or the Work proves nothing about
	// which markers are old: nothing is pruned, so nothing is sent again.
	for _, ids := range [][2]string{{"", ""}, {"turn:b", ""}, {"", "wk1"}} {
		half := newDurableState()
		half.TopicProjection["topic:mark:%1:turn:a:done"] = "d"
		half.TopicProjection["topic:mark:%1:wk0:done"] = "d"
		pruneSessionMarkers(&half, "%1", ids[0], ids[1])
		if len(half.TopicProjection) != 2 {
			t.Fatalf("markers pruned on a half-empty read %v: %v", ids, half.TopicProjection)
		}
	}
}

func TestOrphanCheckpointsGoWithTheirSession(t *testing.T) {
	state := newDurableState()
	state.Topics = []topicMapping{{SessionID: "%1", MessageThreadID: 5, State: topicStateStale}}
	state.TopicOps = []topicOpRecord{{ID: "op", SessionID: "a:b"}}
	state.FallbackSessionID = "%3"
	for _, key := range []string{
		"topic:msg:%1:e:0", "topic:mark:%1:turn:x:done", // stale mapping: kept until its topic is deleted
		"topic:msg:a:b:e:0", "private:msg:%3:e:0",
		"topic:msg:%9:e:0", "topic:mark:%9:turn:x:done", "topic:life:%9:revive", // no mapping, op or fallback
	} {
		state.TopicProjection[key] = "d"
		state.TopicMessages[key] = 1
	}
	state.TopicMessages["outbox:assistant:x:0"] = 2
	pruneOrphanCheckpoints(&state)
	for _, key := range []string{"topic:msg:%9:e:0", "topic:mark:%9:turn:x:done", "topic:life:%9:revive"} {
		if _, ok := state.TopicProjection[key]; ok {
			t.Fatalf("orphan %s survived", key)
		}
	}
	for _, key := range []string{"topic:msg:%1:e:0", "topic:msg:a:b:e:0", "private:msg:%3:e:0", "topic:mark:%1:turn:x:done"} {
		if _, ok := state.TopicProjection[key]; !ok {
			t.Fatalf("%s was pruned though its Session is known", key)
		}
	}
	// Message ids are kept only for keys that are edited.
	if _, ok := state.TopicMessages["topic:mark:%1:turn:x:done"]; ok {
		t.Fatal("a lifecycle marker kept a message id")
	}
	if state.TopicMessages["outbox:assistant:x:0"] != 2 || state.TopicMessages["topic:msg:%1:e:0"] != 1 {
		t.Fatalf("editable message ids lost: %v", state.TopicMessages)
	}
}

// A reply target is the ReplySessions mapping, which pruning does not touch.
func TestPruningLeavesReplyTargetsAlone(t *testing.T) {
	manager, fake, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	start := manager.now()
	fake.timeline = append(fake.timeline, brain.TimelineItem{ID: "a", Role: "assistant", Kind: "assistant_message", Body: "b", CreatedAt: start.Add(time.Second)})
	if err := manager.store.mutate(func(state *durableState) error {
		state.ReplySessions[77] = "%1"
		state.MessageSources[78] = messageSource{SessionID: "%1"}
		state.Projection["outbox:assistant:old:0"] = "d"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projectWithClock(t, manager, start.Add(90*24*time.Hour))
	state := manager.store.snapshot()
	if state.ReplySessions[77] != "%1" || state.MessageSources[78].SessionID != "%1" {
		t.Fatalf("reply targets pruned: %v %v", state.ReplySessions, state.MessageSources)
	}
	if _, ok := state.Projection["outbox:assistant:old:0"]; ok {
		t.Fatal("the unreferenced checkpoint survived")
	}
}

// Fifty thousand checkpoints shrink to the retained set in one pass, on disk
// too, and the next pass finds nothing to do.
func TestPruningFiftyThousandCheckpoints(t *testing.T) {
	manager, fake, _, root := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	start := manager.now()
	const items = 17000
	if err := manager.store.mutate(func(state *durableState) error {
		for i := 0; i < items; i++ {
			fake.timeline = append(fake.timeline, brain.TimelineItem{ID: fmt.Sprintf("a%d", i), Role: "assistant", Kind: "assistant_message", Body: "b",
				CreatedAt: start.Add(time.Duration(i+1) * time.Second)})
			key := fmt.Sprintf("outbox:assistant:a%d:0", i)
			state.Projection[key], state.TopicProjection[key], state.TopicMessages[key] = checkpointFor("b"), checkpointFor("b"), int64(i+1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	projectWithClock(t, manager, start.Add(40*24*time.Hour))
	t.Logf("pruned %d checkpoints in %s", 3*items, time.Since(began))
	state := manager.store.snapshot()
	if len(state.Projection) != projectionKeepNewest || len(state.TopicProjection) != projectionKeepNewest || len(state.TopicMessages) != projectionKeepNewest {
		t.Fatalf("left %d/%d/%d, want %d each", len(state.Projection), len(state.TopicProjection), len(state.TopicMessages), projectionKeepNewest)
	}
	rows := len(state.Outbox)
	projectWithClock(t, manager, start.Add(40*24*time.Hour+time.Minute))
	if again := manager.store.snapshot(); len(again.Outbox) != rows || len(again.Projection) != projectionKeepNewest {
		t.Fatalf("second pass changed state: outbox %d -> %d", rows, len(again.Outbox))
	}
	// The deletions reached the database, not just memory.
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	if got := len(reopened.snapshot().Projection); got != projectionKeepNewest {
		t.Fatalf("database holds %d projection rows", got)
	}
}

// A Session's topic stops growing: output and lifecycle projection prune what
// the Session no longer needs in the same mutation that writes the new rows.
func TestSessionProjectionPrunesAsItWrites(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	createTopicFor(t, manager)
	start := manager.now()
	var assistant []brain.SessionAssistantItem
	if err := manager.store.mutate(func(state *durableState) error {
		for i := 0; i < projectionKeepNewest+20; i++ {
			id := fmt.Sprintf("e%d", i)
			assistant = append(assistant, brain.SessionAssistantItem{ID: id, Body: "b", CreatedAt: start.Add(time.Duration(i+1) * time.Second)})
			key := "topic:msg:sess-a:" + id + ":0"
			state.TopicProjection[key], state.TopicMessages[key] = checkpointFor("b"), int64(i+1)
		}
		state.TopicProjection["topic:mark:sess-a:turn-old:done"] = "d"
		state.TopicProjection["topic:mark:sess-a:turn-now:running"] = "d"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projection := owner.projections["sess-a"]
	projection.Assistant, projection.TurnID, projection.TurnStatus = assistant, "turn-now", "running"
	owner.projections["sess-a"] = projection
	manager.now = func() time.Time { return start.Add(45 * 24 * time.Hour) }
	if err := manager.projectSessionTopics(t.Context(), "token"); err != nil {
		t.Fatal(err)
	}
	state := manager.store.snapshot()
	messages := 0
	for key := range state.TopicProjection {
		if len(key) > len("topic:msg:") && key[:len("topic:msg:")] == "topic:msg:" {
			messages++
		}
	}
	if messages != projectionKeepNewest {
		t.Fatalf("%d message checkpoints, want %d", messages, projectionKeepNewest)
	}
	if _, ok := state.TopicProjection["topic:mark:sess-a:turn-old:done"]; ok {
		t.Fatal("a marker of an earlier Turn survived")
	}
	if _, ok := state.TopicProjection["topic:mark:sess-a:turn-now:running"]; !ok {
		t.Fatal("the current Turn's marker was pruned")
	}
	if _, ok := state.TopicMessages["topic:msg:sess-a:e0:0"]; ok {
		t.Fatal("an old message id survived")
	}
	for _, row := range state.Outbox {
		if row.CanonicalID == "session:sess-a:e0" {
			t.Fatalf("a pruned message was re-sent: %+v", row)
		}
	}
}

// Only a message that can be edited is remembered by its Telegram id.
func TestDeliveringAMarkerDoesNotKeepAMessageID(t *testing.T) {
	manager, _, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	if err := manager.store.mutate(func(state *durableState) error {
		enqueueTopicTextLocked(state, "topic:mark:sess-a:turn-1:done", 0, "Session completed.", manager.now())
		enqueue(state, outboxRecord{ID: "answer", Kind: "send", TopicKey: "topic:msg:sess-a:e1:0", Text: "hi", CreatedAt: manager.now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for pendingOutboxRows(manager.store.snapshot()) > 0 {
		if err := manager.deliverOne(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	state := manager.store.snapshot()
	if _, ok := state.TopicProjection["topic:mark:sess-a:turn-1:done"]; !ok {
		t.Fatalf("the marker's digest is needed to dedupe it: %v", state.TopicProjection)
	}
	if _, ok := state.TopicMessages["topic:mark:sess-a:turn-1:done"]; ok {
		t.Fatal("a marker kept a Telegram message id nothing reads")
	}
	if state.TopicMessages["topic:msg:sess-a:e1:0"] == 0 {
		t.Fatal("an editable message lost its Telegram message id")
	}
}

// A read of the wrong or a truncated conversation must not cost the real one
// its checkpoints, or the next good read sends everything in the window again.
func TestSessionReadThatMissesItemsDoesNotPruneThem(t *testing.T) {
	manager, owner, _, _ := topicFixture(t)
	createTopicFor(t, manager)
	start := manager.now()
	var original []brain.SessionAssistantItem
	if err := manager.store.mutate(func(state *durableState) error {
		for i := 0; i < projectionKeepNewest+20; i++ {
			id := fmt.Sprintf("e%d", i)
			original = append(original, brain.SessionAssistantItem{ID: id, Body: "b", CreatedAt: start.Add(time.Duration(i+1) * time.Second)})
			key := "topic:msg:sess-a:" + id + ":0"
			state.TopicProjection[key], state.TopicMessages[key] = checkpointFor("b"), int64(i+1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	later := start.Add(45 * 24 * time.Hour)
	manager.now = func() time.Time { return later }
	project := func(items []brain.SessionAssistantItem) {
		projection := owner.projections["sess-a"]
		projection.Assistant = items
		owner.projections["sess-a"] = projection
		if err := manager.projectSessionTopics(t.Context(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	checkpoints := func() (count int) {
		for key := range manager.store.snapshot().TopicProjection {
			if strings.HasPrefix(key, "topic:msg:sess-a:e") {
				count++
			}
		}
		return count
	}

	// A flap: the reader shows another conversation entirely.
	var other []brain.SessionAssistantItem
	for i := 0; i < 10; i++ {
		other = append(other, brain.SessionAssistantItem{ID: fmt.Sprintf("f%d", i), Body: "other", CreatedAt: later.Add(-time.Hour)})
	}
	project(other)
	if got := checkpoints(); got != projectionKeepNewest+20 {
		t.Fatalf("a read that showed none of the items pruned them: %d left", got)
	}

	// The real conversation is back: nothing is sent again, and now the old
	// ones the read does show are pruned.
	project(original)
	if got := checkpoints(); got != projectionKeepNewest {
		t.Fatalf("%d checkpoints after the real read, want %d", got, projectionKeepNewest)
	}
	for _, row := range manager.store.snapshot().Outbox {
		if strings.HasPrefix(row.CanonicalID, "session:sess-a:e") {
			t.Fatalf("an item was sent again: %+v", row)
		}
	}
}
