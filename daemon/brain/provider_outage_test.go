package brain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

var rateLimited = &watcher.ProviderActivityError{
	Code: "rate_limit", HTTPStatus: 429,
	Message: "API Error: Request rejected (429) · All credentials for model claude-fable-5-2 are cooling down",
}

// endHostTurnWith settles Brain's Host turn for the delivered review, with the
// provider observation failure on that turn's own Activity, and tells Brain
// the Host's turn is over.
func endHostTurnWith(t *testing.T, store *Store, service *Service, claimed WorkReviewAction, failure *watcher.ProviderActivityError) {
	t.Helper()
	settled := settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	fw := testFakeWatcher(service)
	if fw.providerEvidence == nil {
		fw.providerEvidence = map[string]watcher.ProviderActivityObservation{}
	}
	fw.providerEvidence["host"] = watcher.ProviderActivityObservation{
		ID: settled.ActivityID, Status: "completed", ProbeState: watcher.ProbeStateOK,
		StartedAt: settled.AcceptedAt, SettledAt: time.Now().UTC(), Error: failure,
	}
	host := &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateDone}
	if _, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "worker_state_change", WorkerID: "host", Worker: host, TurnID: claimed.ProviderTurnID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
	}); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-21 (Work 6dcd42a3): the result was delivered at 23:02:15, Brain's
// turn died on the 429 at 23:02:23, the handling ended at 23:02:24 as if Brain
// had read it, and the runtime closed the Work at 23:05:34. A refused turn
// read nothing: the review is delivered again, and nothing settles on it.
func TestAResultWhoseHostTurnTheProviderRefusedIsDeliveredAgainNotCountedAsRead(t *testing.T) {
	store, service, item, claimed := hostHandlingFixture(t)
	// The provider is known to be unavailable: the lane holds the replay.
	ow := &outageWatcher{fakeWatcher: service.watcher.(*fakeWatcher), unavailable: true}
	service.watcher = ow
	endHostTurnWith(t, store, service, claimed, rateLimited)
	if handlingEnded(t, store, item.ID) {
		t.Fatal("a Host turn the provider refused ended its handling as read")
	}
	got, err := store.Work(item.ID)
	if err != nil || got.Review == nil || got.Review.EventID != claimed.EventID || got.Review.Lease != nil {
		t.Fatalf("the refused review is not waiting for redelivery: %+v err=%v", got.Review, err)
	}
	if !store.reviewClaimable(time.Now()) {
		t.Fatal("the refused review cannot be delivered again")
	}
	// Even a judge that would close it never sees it: the result is unread.
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "delivered"}}
	service.SetWorkJudge(judge)
	later := time.Now().Add(settleDebounce + time.Hour)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled || len(judge.briefs) != 0 {
		t.Fatalf("an unread result was settled: %+v (judged %d times)", got, len(judge.briefs))
	}
	service.now = time.Now
	again, _ := claimAndDeliverTestReview(t, store, "host")
	if again.EventID != claimed.EventID || again.HandlingID == claimed.HandlingID {
		t.Fatalf("redelivery = %+v, want the same review in a new handling", again)
	}
}

func TestWithNoKnownOutageARefusedResultIsDeliveredAgainAtOnce(t *testing.T) {
	// One refusal is not yet an outage (it takes two): the lane delivers the
	// same review again in a new handling right away.
	store, service, item, claimed := hostHandlingFixture(t)
	endHostTurnWith(t, store, service, claimed, rateLimited)
	got, err := store.Work(item.ID)
	if err != nil || got.Review == nil || got.Review.EventID != claimed.EventID || got.Review.Lease == nil ||
		got.Review.Lease.HandlingID == claimed.HandlingID || got.Review.Lease.HandlingEndedAt != nil {
		t.Fatalf("the refused review was not delivered again: %+v err=%v", got.Review, err)
	}
}

func TestAHostTurnThatEndedWithoutAProviderRefusalStillEndsItsHandling(t *testing.T) {
	for name, failure := range map[string]*watcher.ProviderActivityError{
		"no error":               nil,
		"sign-in, not an outage": {Code: "authentication_error", HTTPStatus: 401},
	} {
		store, service, item, claimed := hostHandlingFixture(t)
		endHostTurnWith(t, store, service, claimed, failure)
		if !handlingEnded(t, store, item.ID) {
			t.Fatalf("%s: the handling did not end", name)
		}
	}
	// A refusal on an older Activity says nothing about this turn.
	store, service, item, claimed := hostHandlingFixture(t)
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	fw := testFakeWatcher(service)
	fw.providerEvidence = map[string]watcher.ProviderActivityObservation{"host": {
		ID: "an-older-activity", Status: "completed", ProbeState: watcher.ProbeStateOK, SettledAt: time.Now(), Error: rateLimited,
	}}
	if _, err := service.endHostHandling(item.ID, claimed.HandlingID, claimed.ProviderTurnID, "host"); err != nil {
		t.Fatal(err)
	}
	if !handlingEnded(t, store, item.ID) {
		t.Fatal("an old refusal on another Activity was taken for this turn's")
	}
}

func testFakeWatcher(service *Service) *fakeWatcher {
	if ow, ok := service.watcher.(*outageWatcher); ok {
		return ow.fakeWatcher
	}
	return service.watcher.(*fakeWatcher)
}

type outageWatcher struct {
	*fakeWatcher
	unavailable bool
	grant       bool
	probes      int
}

func (w *outageWatcher) ProviderOutage(string) (watcher.ProviderOutage, bool) {
	return watcher.ProviderOutage{Provider: "claude", Since: time.Now().Add(-time.Hour), Kind: "rate limit"}, w.unavailable
}

func (w *outageWatcher) ProviderOutages() []watcher.ProviderOutage {
	if !w.unavailable {
		return nil
	}
	outage, _ := w.ProviderOutage("claude")
	return []watcher.ProviderOutage{outage}
}

func (w *outageWatcher) ProviderProbe(string, time.Time) (bool, time.Time) {
	if !w.unavailable {
		return true, time.Time{}
	}
	if w.grant {
		w.probes++
		return true, time.Now().Add(5 * time.Minute)
	}
	return false, time.Now().Add(5 * time.Minute)
}

func TestTheHostLaneHoldsDeliveriesWhileTheHostsProviderIsUnavailable(t *testing.T) {
	store, service, item, claimed := hostHandlingFixture(t)
	ow := &outageWatcher{fakeWatcher: service.watcher.(*fakeWatcher), unavailable: true}
	service.watcher = ow
	endHostTurnWith(t, store, service, claimed, rateLimited)
	if !service.hostLaneHeld("host") {
		t.Fatal("the lane would deliver to a Host whose provider refuses every request")
	}
	ow.grant = true
	if service.hostLaneHeld("host") || ow.probes != 1 {
		t.Fatalf("the family's probe was not used to deliver (probes %d)", ow.probes)
	}
	ow.unavailable = false
	if service.hostLaneHeld("host") {
		t.Fatal("the lane stayed held after the provider answered")
	}
	if got := service.ProviderOutages(); got != nil {
		t.Fatalf("outages after recovery: %+v", got)
	}
	// A probe is never spent on an empty lane: it would delay noticing the
	// provider is back.
	ow.unavailable, ow.probes = true, 0
	claimAndDeliverTestReview(t, store, "host")
	resolveDeliveredReview(t, store, item.ID, WorkDispositionComplete)
	if store.reviewClaimable(time.Now()) {
		t.Fatal("setup: a review is still claimable")
	}
	if !service.hostLaneHeld("host") || ow.probes != 0 {
		t.Fatalf("an empty lane spent the family's probe (%d)", ow.probes)
	}
}

func TestBrainsSnapshotListsTheOutageAndAFailingJudgeClosesNothingMeanwhile(t *testing.T) {
	store, service, item := endedHandlingFixture(t, &fakeJudge{err: errors.New("headless judge: … answered HTTP 429: Request rejected (429)")})
	ow := &outageWatcher{fakeWatcher: service.watcher.(*fakeWatcher), unavailable: true}
	service.watcher = ow
	snapshot, err := service.ProjectionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ProviderOutages) != 1 || snapshot.ProviderOutages[0].Kind != "rate limit" {
		t.Fatalf("snapshot outages = %+v", snapshot.ProviderOutages)
	}
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
		t.Fatalf("a Work closed during the outage: %+v", got)
	}
}

// 2026-11-21: Brain's Chat showed "API Error: Request rejected (429) · All
// credentials … are cooling down" under every Work result Brain's refused turn
// was handed. That delivery is replayed and Chat's one outage line covers it,
// so the refusal text stays out of Chat. A refused reply to the person's own
// message, and a sign-in refusal, still show.
func TestBrainsChatLeavesOutTheProvidersRefusalOfAWorkDeliveryOnly(t *testing.T) {
	store, service, fw := captureFixture(t)
	const (
		threadID = "brain_thread_capture"
		hostID   = "mewla-worker-brain-capture:@1"
	)
	if err := store.SetHostSession(hostID, "claude"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	transcript := filepath.Join(dir, "host-claude.jsonl")
	event := work.FormatDirectWorkEventInput(work.DirectWorkEventInput{
		EventID: "0e56b60ceaeca351", WorkID: "6dcd42a3-0000-4000-8000-000000000001", WorkRevision: 3,
		HandlingID: "handling:1", ProviderTurnID: "turn:1", WorkTitle: "Hacker News briefing", Kind: "turn_done",
		Source: "worker", Summary: "Result ready", NextAction: "read the result", ContextRef: "work:6dcd42a3", PayloadRef: "work:6dcd42a3",
	})
	at := time.Date(2026, 11, 20, 23, 2, 15, 0, time.UTC)
	stamp := func(seconds int) string { return at.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339Nano) }
	user := func(id string, seconds int, text string) map[string]any {
		return map[string]any{"type": "user", "uuid": id, "timestamp": stamp(seconds), "sessionId": "host-claude", "cwd": dir,
			"message": map[string]any{"role": "user", "content": text}}
	}
	reply := func(id string, seconds int, text string) map[string]any {
		return map[string]any{"type": "assistant", "uuid": id, "timestamp": stamp(seconds), "sessionId": "host-claude", "cwd": dir,
			"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]string{{"type": "text", "text": text}}}}
	}
	refusal := func(id string, seconds int, code string, status int, text string) map[string]any {
		record := reply(id, seconds, text)
		record["isApiErrorMessage"], record["error"], record["apiErrorStatus"] = true, code, status
		record["message"].(map[string]any)["model"] = "<synthetic>"
		record["message"].(map[string]any)["stop_reason"] = "stop_sequence"
		return record
	}
	const limited = "API Error: Request rejected (429) · All credentials for model claude-fable-5-2 are cooling down"
	records := []map[string]any{
		user("u1", 0, "What is on my plate today?"),
		reply("a1", 1, "Two Works are running."),
		user("u2", 2, event),
		refusal("a2", 8, "rate_limit", 429, limited), // Brain's turn handed the result: replayed
		user("u3", 60, "Are you there?"),
		refusal("a3", 61, "rate_limit", 429, limited), // the person's message: not replayed, stays
		user("u4", 120, event),
		refusal("a4", 121, "authentication_failed", 401, "API Error: 401 · Please run /login"), // stays
	}
	var lines []string
	parent := any(nil)
	for _, record := range records {
		record["parentUuid"], parent = parent, record["uuid"]
		if record["type"] == "user" {
			record["promptId"], record["userType"] = "prompt-"+record["uuid"].(string), "external"
		}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostProviderTranscript("host-claude", transcript, ""); err != nil {
		t.Fatal(err)
	}
	fw.sessions[hostID] = &classifier.Worker{
		ID: hostID, Name: "Brain", Hidden: true, State: classifier.StateDone,
		Command: "claude --session-id host-claude", Cwd: dir,
	}
	if captured, err := service.captureHostTranscript(work.NewProviderConversationReader(), &hostTranscriptCaptureState{}); err != nil || !captured {
		t.Fatalf("capture: captured=%v err=%v", captured, err)
	}
	var bodies []string
	for _, item := range waitForTimelineAssistant(t, store, threadID, 3) {
		bodies = append(bodies, item.Body)
	}
	want := []string{"Two Works are running.", limited, "API Error: 401 · Please run /login"}
	if strings.Join(bodies, "|") != strings.Join(want, "|") {
		t.Fatalf("Chat replies = %q, want %q", bodies, want)
	}
	// The App's live overlay of the same transcript, and a direct
	// materialization of it, leave it out too.
	live, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	bodies = nil
	for _, event := range live.Events {
		if event.Kind == "assistant_message" {
			bodies = append(bodies, event.Body)
		}
	}
	if strings.Join(bodies, "|") != strings.Join(want, "|") {
		t.Fatalf("live Chat replies = %q, want %q", bodies, want)
	}
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{SessionID: "host-claude", Events: append([]work.CodexConversationEvent(nil), live.Events...)}); err != nil {
		t.Fatal(err)
	}
	unfiltered, err := work.NewProviderConversationReader().LoadByIdentity(work.HostTranscriptIdentity{Provider: "claude", SessionID: "host-claude", Path: transcript})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MaterializeProviderConversation(threadID, unfiltered); err != nil {
		t.Fatal(err)
	}
	if rows := waitForTimelineAssistant(t, store, threadID, 3); len(rows) != 3 {
		t.Fatalf("materialized rows = %d", len(rows))
	}
}
