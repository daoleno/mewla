package watch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/push"
)

// drain waits for the collector's notification bookkeeping to finish.
func drain(t *testing.T, c *Collector) {
	t.Helper()
	done := make(chan struct{})
	c.enqueue(func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notification queue did not drain")
	}
}

func notificationSignals(store *Store) []Signal {
	return store.Signals(Filter{Source: SourceNotification})
}

func TestNotificationRecordsTextContextAndNeverTheToken(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.works = &fakeWorks{works: []brain.Work{
		{ID: "work-1", Title: "Add dark mode", Status: brain.WorkRunning, AttemptSessionID: "main:7"},
		{ID: "work-2", Title: "Ship v2", Status: brain.WorkNeedsInput, Question: "Release now or wait?"},
	}}
	collector.SetPresence(func(workerID string) Presence {
		return Presence{Connected: true, ForegroundKnown: true, Viewing: workerID == "main:7"}
	})
	collector.NotificationSent(push.Sent{
		ID: "n_abc", Kind: push.KindWorkerDone, At: time.Now().UTC(),
		Title: "mewla finished", Body: "Session completed. token ExponentPushToken[abcdefghijklmnopqrstuv]",
		Priority: "default", Refs: map[string]string{"worker_id": "main:7", "screen": "terminal"},
		Outcome: push.OutcomeAccepted,
	})
	drain(t, collector)
	signals := notificationSignals(store)
	if len(signals) != 1 {
		t.Fatalf("signals = %#v", signals)
	}
	signal := signals[0]
	if signal.Source != SourceNotification || signal.Kind != push.KindWorkerDone || signal.Refs["notification"] != "n_abc" {
		t.Fatalf("signal = %#v", signal)
	}
	if signal.Refs["session"] != "main:7" || signal.Refs["work"] != "work-1" || signal.Refs["work_title"] != "Add dark mode" ||
		signal.Refs["viewing_target"] != "yes" || signal.Refs["app_foreground"] != "no" || signal.Refs["delivery"] != "accepted" {
		t.Fatalf("refs = %#v", signal.Refs)
	}
	if !strings.Contains(signal.Refs["brain_waiting"], "Release now or wait?") {
		t.Fatalf("brain_waiting = %q", signal.Refs["brain_waiting"])
	}
	if strings.Contains(signal.Excerpt, "abcdefghijklmnopqrstuv") {
		t.Fatalf("push token stored: %q", signal.Excerpt)
	}
}

func TestNotificationWithoutAPhoneNeedsNoVerdict(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.NotificationSent(push.Sent{
		ID: "n_none", Kind: push.KindWorkerDone, At: time.Now().UTC(), Title: "t", Body: "b",
		Outcome: push.OutcomeNotSent, Reason: push.ReasonNoRegistration,
	})
	drain(t, collector)
	if len(notificationSignals(store)) != 1 {
		t.Fatal("not-sent notification was not recorded")
	}
	if len(store.Unjudged()) != 0 {
		t.Fatalf("a daemon with no phone queued %d signals for judgment", len(store.Unjudged()))
	}
}

func TestNotificationResponsesSettleAndHoldJudgment(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.engageDelay = 0
	now := time.Now().UTC()
	collector.now = func() time.Time { return now }
	sent := func(id, worker string, ago time.Duration) {
		collector.NotificationSent(push.Sent{
			ID: id, Kind: push.KindWorkerDone, At: now.Add(-ago), Title: "x finished", Body: "done",
			Refs: map[string]string{"worker_id": worker}, Outcome: push.OutcomeAccepted,
		})
	}
	sent("n_old", "w1", 20*time.Minute)
	sent("n_new", "w2", time.Minute)
	drain(t, collector)
	if got := len(store.Unjudged()); got != 0 {
		t.Fatalf("judgment saw %d unsettled notifications", got)
	}

	collector.NotificationResponse("n_old", "tapped", now.Add(-18*time.Minute))
	collector.NotificationResponse("n_unknown", "tapped", now)
	collector.NotificationReceipt(push.Receipt{ID: "n_old", OK: true})
	collector.NotificationEngaged(ResponseViewed, "w2")
	drain(t, collector)
	collector.settleNotifications()

	unjudged := store.Unjudged()
	if len(unjudged) != 1 || unjudged[0].Refs["notification"] != "n_old" {
		t.Fatalf("unjudged = %#v", unjudged)
	}
	state := unjudged[0].Notification
	if state == nil || state.Receipt != "ok" || len(state.Responses) != 1 || state.Responses[0].Action != ResponseTapped ||
		state.Responses[0].At.Sub(unjudged[0].Time) != 2*time.Minute {
		t.Fatalf("state = %#v", state)
	}
	newer, _ := store.Signal(signalID(SourceNotification, "n_new"))
	if newer.Notification == nil || len(newer.Notification.Responses) != 1 || newer.Notification.Responses[0].Action != ResponseViewed {
		t.Fatalf("the viewed session was not recorded: %#v", newer.Notification)
	}
	// A tap after the window closed is still recorded.
	collector.NotificationResponse("n_old", "tapped", now)
	drain(t, collector)
	late, _ := store.Signal(signalID(SourceNotification, "n_old"))
	if len(late.Notification.Responses) != 2 {
		t.Fatalf("late tap lost: %#v", late.Notification)
	}
}

func TestForegroundRightAfterATapIsNotCountedForOtherPushes(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.engageDelay = 0
	now := time.Now().UTC()
	collector.now = func() time.Time { return now }
	for id, ago := range map[string]time.Duration{"n_tapped": 2 * time.Minute, "n_other": time.Minute} {
		collector.NotificationSent(push.Sent{ID: id, Kind: push.KindBrainReply, At: now.Add(-ago), Title: "Brain", Body: "q", Outcome: push.OutcomeAccepted})
	}
	collector.NotificationResponse("n_tapped", ResponseTapped, now.Add(-2*time.Second))
	collector.NotificationEngaged(ResponseOpenedApp, "")
	drain(t, collector)
	other, _ := store.Signal(signalID(SourceNotification, "n_other"))
	if other.Notification != nil && len(other.Notification.Responses) > 0 {
		t.Fatalf("the foreground that a tap caused was counted for another push: %#v", other.Notification)
	}
}

func TestNotificationEngagementIgnoresOtherSessionsAndOldPushes(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.engageDelay = 0
	now := time.Now().UTC()
	collector.now = func() time.Time { return now }
	for id, ago := range map[string]time.Duration{"n_recent": time.Minute, "n_stale": time.Hour} {
		collector.NotificationSent(push.Sent{ID: id, Kind: push.KindWorkerDone, At: now.Add(-ago), Title: "t", Body: "b",
			Refs: map[string]string{"worker_id": "w1"}, Outcome: push.OutcomeAccepted})
	}
	drain(t, collector)
	collector.NotificationEngaged(ResponseViewed, "w-other")
	collector.NotificationEngaged(ResponseOpenedApp, "")
	drain(t, collector)
	recent, _ := store.Signal(signalID(SourceNotification, "n_recent"))
	stale, _ := store.Signal(signalID(SourceNotification, "n_stale"))
	if recent.Notification == nil || len(recent.Notification.Responses) != 1 || recent.Notification.Responses[0].Action != ResponseOpenedApp {
		t.Fatalf("recent = %#v", recent.Notification)
	}
	if stale.Notification != nil && len(stale.Notification.Responses) > 0 {
		t.Fatalf("an old push counted as answered: %#v", stale.Notification)
	}
}

func TestNotificationDeliveryRuleFlagsOnlyARunOfFailures(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	now := time.Now().UTC()
	collector.now = func() time.Time { return now }
	send := func(id string, ago time.Duration, outcome, reason string) {
		collector.NotificationSent(push.Sent{ID: id, Kind: push.KindBrainReply, At: now.Add(-ago), Title: "Brain", Body: "q",
			Outcome: outcome, Reason: reason})
	}
	// A daemon with no phone is not a delivery problem, however long it goes on.
	for i := 0; i < 5; i++ {
		send("n_nophone"+string(rune('a'+i)), time.Duration(40+i)*time.Minute, push.OutcomeNotSent, push.ReasonNoRegistration)
	}
	send("n_f1", 30*time.Minute, push.OutcomeFailed, "InvalidCredentials")
	send("n_f2", 20*time.Minute, push.OutcomeFailed, "InvalidCredentials")
	drain(t, collector)
	if got := ruleNotificationDelivery(store, now); len(got) != 0 {
		t.Fatalf("two failures flagged: %#v", got)
	}
	send("n_f3", 10*time.Minute, push.OutcomeFailed, "InvalidCredentials")
	drain(t, collector)
	got := ruleNotificationDelivery(store, now)
	if len(got) != 1 || got[0].Rule != RuleNotificationDelivery || got[0].Count != 3 || !strings.Contains(got[0].Title, "InvalidCredentials") {
		t.Fatalf("problems = %#v", got)
	}
	// A notification that arrives clears it.
	send("n_ok", time.Minute, push.OutcomeAccepted, "")
	collector.NotificationReceipt(push.Receipt{ID: "n_ok", OK: true})
	drain(t, collector)
	if got := ruleNotificationDelivery(store, now); len(got) != 0 {
		t.Fatalf("still flagged after a delivery: %#v", got)
	}
}

func TestNotificationReceiptFailureCountsAsFailedDelivery(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	now := time.Now().UTC()
	for i, id := range []string{"n_r1", "n_r2", "n_r3"} {
		collector.NotificationSent(push.Sent{ID: id, Kind: push.KindScheduledResult, At: now.Add(-time.Duration(30-i*5) * time.Minute),
			Title: "Report is ready", Body: "Open Brain", Outcome: push.OutcomeAccepted})
		collector.NotificationReceipt(push.Receipt{ID: id, Reason: "DeviceNotRegistered"})
	}
	drain(t, collector)
	got := ruleNotificationDelivery(store, now)
	if len(got) != 1 || got[0].Subject != "DeviceNotRegistered" {
		t.Fatalf("problems = %#v", got)
	}
}

func TestNotificationsDoNotCountAsRepeatedDaemonErrors(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		collector.NotificationSent(push.Sent{ID: "n_" + string(rune('a'+i)), Kind: push.KindWorkerDone, At: now.Add(-time.Duration(i) * time.Minute),
			Title: "t", Body: "b", Outcome: push.OutcomeFailed, Reason: "InvalidCredentials"})
	}
	drain(t, collector)
	for _, problem := range ruleRepeatedError(store, now) {
		t.Fatalf("notifications raised a repeated error: %#v", problem)
	}
}

func TestSuppressedNotificationIsRecordedAsHeldBack(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.NotificationSuppressed(push.KindWorkerBlocked, "w1", "mewla needs input", "Which branch?", "person_viewing_session")
	drain(t, collector)
	signals := notificationSignals(store)
	if len(signals) != 1 || signals[0].Refs["delivery"] != "suppressed" || signals[0].Refs["reason"] != "person_viewing_session" ||
		signals[0].Kind != push.KindWorkerBlocked || !strings.Contains(signals[0].Excerpt, "Which branch?") {
		t.Fatalf("signals = %#v", signals)
	}
}

func TestJudgmentReadsNotificationsInContextAndStoresVerdicts(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	now := time.Now().UTC()
	collector.now = func() time.Time { return now }
	collector.works = &fakeWorks{works: []brain.Work{{ID: "work-1", Title: "Add dark mode", Status: brain.WorkRunning, AttemptSessionID: "main:7"}}}
	collector.SetPresence(func(string) Presence {
		return Presence{Connected: true, Foreground: true, ForegroundKnown: true, Viewing: true}
	})
	collector.NotificationSent(push.Sent{ID: "n_1", Kind: push.KindWorkerDone, At: now.Add(-40 * time.Minute), Title: "mewla finished",
		Body: "Session completed.", Priority: "default", Refs: map[string]string{"worker_id": "main:7"}, Outcome: push.OutcomeAccepted})
	collector.NotificationSent(push.Sent{ID: "n_2", Kind: push.KindWorkerDone, At: now.Add(-35 * time.Minute), Title: "mewla finished",
		Body: "Session completed.", Priority: "default", Refs: map[string]string{"worker_id": "main:7"}, Outcome: push.OutcomeAccepted})
	collector.NotificationSent(push.Sent{ID: "n_3", Kind: push.KindBrainReply, At: now.Add(-30 * time.Minute), Title: "Brain",
		Body: "Release v2 now or wait for QA?", Priority: "high", Refs: map[string]string{"brain_thread_id": "t1", "screen": "brain"}, Outcome: push.OutcomeAccepted})
	drain(t, collector)
	if _, err := store.Append([]Signal{{ID: "h1", Source: SourceHuman, Kind: kindUserMessage, Time: now.Add(-29 * time.Minute),
		Excerpt: "wait for QA", Refs: map[string]string{"author": "user", "session_role": "direct"}}}); err != nil {
		t.Fatal(err)
	}
	collector.NotificationResponse("n_3", "tapped", now.Add(-28*time.Minute))
	drain(t, collector)
	collector.settleNotifications()
	if _, err := store.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	output := `{"attributions":[],"authors":[],"notifications":[
{"signals":[1,2],"verdict":"noise","reason":"Brain reviews finished Work; the generic text asked nothing of the person"},
{"signals":[3],"verdict":"meaningful","reason":"A release decision the person answered within a minute"},
{"signals":[3],"verdict":"bogus","reason":"x"}],
"problems":[{"id":"","project":"","title":"Worker finished pushes carry no news","evidence":[1,2],"merges":[],"cost_score":30,"cost_kind":"attention","cost":"two buzzes in five minutes for nothing to act on","reason":"Both pushes said the same generic text for Work Brain reviews.","suggestion":"Stop pushing worker_done for delegated Workers unless review fails.","status":"open"}]}`
	execute, calls := fakeJudge(output)
	collector.judge.execute = execute
	runs, err := collector.Handle(context.Background(), Request{Action: "judge", Batches: 1})
	if err != nil || len(runs.Runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	prompt := (*calls)[0].prompt
	for _, want := range []string{
		"notification/worker_done", `"mewla finished — Session completed."`, "app in foreground=yes",
		"person had this session open=yes", "Add dark mode", "person's response: tapped",
		"earlier pushes about the same session", "identical text was pushed 1 other", "wait for QA",
		"Release v2 now or wait for QA?",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	first, _ := store.Signal(signalID(SourceNotification, "n_1"))
	third, _ := store.Signal(signalID(SourceNotification, "n_3"))
	if first.Notification.Verdict != VerdictNoise || !strings.Contains(first.Notification.VerdictReason, "Brain reviews") ||
		third.Notification.Verdict != VerdictMeaningful || len(third.Notification.Responses) != 1 {
		t.Fatalf("verdicts: first=%#v third=%#v", first.Notification, third.Notification)
	}
	if runs.Runs[0].Verdicts != 3 || runs.Runs[0].Dropped != 1 {
		t.Fatalf("run = %#v", runs.Runs[0])
	}
	problems := store.Problems(Filter{Kind: RuleJudgment})
	if len(problems) != 1 || !strings.Contains(problems[0].Suggestion, "Stop pushing worker_done") {
		t.Fatalf("problems = %#v", problems)
	}
	if len(store.Unjudged()) != 0 {
		t.Fatalf("still unjudged: %d", len(store.Unjudged()))
	}
}

func TestForegroundIsUnknownWhenTheAppNeverReportsIt(t *testing.T) {
	collector, store := newTestCollector(t, t.TempDir())
	collector.SetPresence(func(string) Presence { return Presence{Connected: true} })
	collector.NotificationSent(push.Sent{ID: "n_old_app", Kind: push.KindBrainReply, At: time.Now().UTC(), Title: "Brain", Body: "q", Outcome: push.OutcomeAccepted})
	drain(t, collector)
	signal, _ := store.Signal(signalID(SourceNotification, "n_old_app"))
	if signal.Refs["app_foreground"] != "unknown" {
		t.Fatalf("app_foreground = %q, want unknown", signal.Refs["app_foreground"])
	}
}
