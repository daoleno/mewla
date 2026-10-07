package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

func newUserActionService(t *testing.T) (*Store, *Service, *fakeWatcher, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "brain-host:@user-action"
	const threadID = "thread-user-action"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Worker{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateRunning},
		},
		ownedGenerations: map[string]string{hostID: "user-action-generation"},
		outcomes:         map[string]watcher.InputOutcome{},
		turnStore:        store,
	}
	return store, NewService(store, fw, nil), fw, threadID
}

func askUser(t *testing.T, store *Store, title string, choices ...string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{Title: title, Objective: "needs a call", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	status := WorkWaiting
	wait := "the user's call on " + title
	question := "Keep both copies, or the newest edit?"
	item, err = store.UpdateWork(item.ID, WorkUpdate{Status: &status, WaitFor: &wait, Question: &question, Choices: &choices})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func currentWorkByID(t *testing.T, store *Store, workID string) (CurrentWork, bool) {
	t.Helper()
	inventory, err := store.ProjectWorkInventory(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range inventory.Current {
		if item.ID == workID {
			return item, true
		}
	}
	return CurrentWork{}, false
}

func TestWorkQuestionAndChoicesProjectOnTheSlip(t *testing.T) {
	store, _, _, _ := newUserActionService(t)
	item := askUser(t, store, "sync fix", "Keep both", " Newest  wins ", "Keep both", "", "A", "B", "C")
	if item.Question == "" || len(item.Choices) != 4 || item.Choices[1] != "Newest wins" || item.QuestionAt == nil {
		t.Fatalf("question=%q choices=%q at=%v", item.Question, item.Choices, item.QuestionAt)
	}
	current, found := currentWorkByID(t, store, item.ID)
	if !found || current.Question != item.Question || len(current.Choices) != 4 || current.AttentionSince == nil {
		t.Fatalf("question Work must be current with its buttons: found=%v %+v", found, current)
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Work(item.ID)
	if err != nil || persisted.Question != item.Question || len(persisted.Choices) != 4 {
		t.Fatalf("question did not survive reopen: %+v err=%v", persisted, err)
	}
	empty := ""
	cleared, err := store.UpdateWork(item.ID, WorkUpdate{Question: &empty})
	if err != nil || cleared.Question != "" || cleared.Choices != nil || cleared.QuestionAt != nil {
		t.Fatalf("empty question must clear choices: %+v err=%v", cleared, err)
	}
}

func TestWorkReplyReachesBrainAsTurnInputAndReleasesTheWait(t *testing.T) {
	store, service, fw, threadID := newUserActionService(t)
	item := askUser(t, store, "sync fix", "Keep both", "Newest wins")
	if item.Wake == nil || item.Wake.Kind != WorkWakeUserInput {
		t.Fatalf("setup must wait on the user: %+v", item.Wake)
	}
	result, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserReply, Text: "Keep both", RequestID: "r1"})
	if err != nil || result.Admission != ExternalInputAccepted {
		t.Fatalf("reply admission=%q err=%v", result.Admission, err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("Brain inputs=%+v", fw.sentCalls)
	}
	body := fw.sentCalls[0].text
	if !strings.Contains(body, "(work "+item.ID+")") || !strings.HasSuffix(body, "\nKeep both") || !strings.Contains(body, "> Keep both copies") {
		t.Fatalf("reply must name the Work and quote the question: %q", body)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || !items[0].BrainAdmission {
		t.Fatalf("reply must be an admitted Brain input: %+v err=%v", items, err)
	}
	after, err := store.Work(item.ID)
	if err != nil || after.Wake != nil || after.Question != "" || after.Status == WorkWaiting {
		t.Fatalf("reply must release the wait and clear the question: %+v err=%v", after, err)
	}
	if result.Event.Kind != "user.replied" || result.Event.Summary != "Keep both" || result.Event.SourceName != WorkUserActor || result.Event.Actionable {
		t.Fatalf("reply event=%+v", result.Event)
	}
	current, found := currentWorkByID(t, store, item.ID)
	if found && (current.UserAction == nil || current.UserAction.Kind != WorkUserReply || current.UserAction.Admission != string(ExternalInputAccepted)) {
		t.Fatalf("slip must show the reply: %+v", current.UserAction)
	}

	// A retried request is the same input: no second Brain turn.
	if _, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserReply, Text: "Keep both", RequestID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("retried reply replayed Brain input: %+v", fw.sentCalls)
	}
}

func TestWorkReplyNotSubmittedRecordsNothing(t *testing.T) {
	store, service, fw, _ := newUserActionService(t)
	item := askUser(t, store, "sync fix")
	fw.sendErr = &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted}}
	result, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserReply, Text: "Keep both"})
	if err == nil || result.Admission != ExternalInputNotSubmitted {
		t.Fatalf("admission=%q err=%v", result.Admission, err)
	}
	after, _ := store.Work(item.ID)
	if after.Question == "" || after.Wake == nil {
		t.Fatalf("an unsent reply must leave the question open: %+v", after)
	}
	if _, found := store.latestWorkEvent(item.ID, "user.replied"); found {
		t.Fatal("an unsent reply recorded an Event")
	}
}

func TestUserCloseAndDismissAreAuditedFinalDecisions(t *testing.T) {
	for _, test := range []struct {
		kind   WorkUserActionKind
		status WorkStatus
	}{{WorkUserClose, WorkDone}, {WorkUserDismiss, WorkCancelled}} {
		t.Run(string(test.kind), func(t *testing.T) {
			store, service, fw, _ := newUserActionService(t)
			item := askUser(t, store, "stale")
			result, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: test.kind})
			if err != nil || result.Work.Status != test.status {
				t.Fatalf("status=%q err=%v", result.Work.Status, err)
			}
			if result.Event.Kind != "brain.work_closed" || result.Event.SourceName != WorkUserActor {
				t.Fatalf("close must be audited as the user: %+v", result.Event)
			}
			if len(fw.sentCalls) != 0 {
				t.Fatalf("closing sent Brain input: %+v", fw.sentCalls)
			}
			if _, found := currentWorkByID(t, store, item.ID); found {
				t.Fatal("closed Work stayed current")
			}
			if _, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserReply, Text: "late"}); err == nil {
				t.Fatal("a closed Work accepted a reply")
			}
		})
	}
}

func TestUserSnoozeRecordsTimeOnly(t *testing.T) {
	store, service, _, _ := newUserActionService(t)
	item := askUser(t, store, "later")
	if _, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserSnooze}); err == nil {
		t.Fatal("snooze without a time was accepted")
	}
	until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	result, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserSnooze, SnoozeUntil: &until})
	if err != nil || result.Event.Kind != "user.snoozed" {
		t.Fatalf("snooze event=%+v err=%v", result.Event, err)
	}
	current, found := currentWorkByID(t, store, item.ID)
	if !found || current.SnoozedUntil == nil || !current.SnoozedUntil.Equal(until) || current.Status != WorkWaiting {
		t.Fatalf("snooze must not change lifecycle state: found=%v %+v", found, current)
	}
}

func TestStopRequiresARunningDelegatedWorker(t *testing.T) {
	store, service, _, _ := newUserActionService(t)
	item := askUser(t, store, "not running")
	if _, err := service.ActOnWork(WorkUserActionRequest{WorkID: item.ID, Kind: WorkUserStop}); err == nil {
		t.Fatal("stop accepted Work with no Worker")
	}
	after, _ := store.Work(item.ID)
	if after.Status == WorkCancelled {
		t.Fatal("a refused stop closed the Work")
	}
}

func TestQuestionOnOwnedWorkNeedsNoWaitAndANewQuestionDropsOldChoices(t *testing.T) {
	store, _, _, _ := newUserActionService(t)
	item := askUser(t, store, "first", "A", "B")
	question := "Which branch?"
	updated, err := store.UpdateWork(item.ID, WorkUpdate{Question: &question})
	if err != nil || updated.Question != question || updated.Choices != nil {
		t.Fatalf("a new question must not inherit choices: %+v err=%v", updated, err)
	}
	owned := admittedOwnedWork(t, store, "owned question")
	q := "Ship it now?"
	choices := []string{"Ship", "Wait"}
	after, err := store.UpdateWork(owned.ID, WorkUpdate{Question: &q, Choices: &choices})
	if err != nil || after.Question != q || len(after.Choices) != 2 || after.Status != WorkRunning {
		t.Fatalf("question on owned Work: %+v err=%v", after, err)
	}
}

func admittedOwnedWork(t *testing.T, store *Store, title string) Work {
	t.Helper()
	item, err := store.CreateWork(Work{Title: title, Objective: "owned by a Worker", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.fsmAdmitTurn(item.ID, "%77", "turn-owned-question", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = store.Work(item.ID)
	if err != nil || item.AttemptSessionID != "%77" {
		t.Fatalf("setup must own the Work: %+v err=%v", item, err)
	}
	return item
}
