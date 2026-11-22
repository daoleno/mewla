package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func refused(at time.Time) ProviderActivityObservation {
	return ProviderActivityObservation{
		ID: "a:" + at.Format(time.RFC3339Nano), Status: "completed", ProbeState: ProbeStateOK, SettledAt: at,
		Error: &ProviderActivityError{Code: "rate_limit", HTTPStatus: 429, Message: "Request rejected (429) · All credentials are cooling down"},
	}
}

func answered(at time.Time) ProviderActivityObservation {
	return ProviderActivityObservation{ID: "ok:" + at.Format(time.RFC3339Nano), Status: "completed", ProbeState: ProbeStateOK, SettledAt: at}
}

func TestProviderOutageStartsOnTheSecondRefusalAndEndsOnTheFirstAnswer(t *testing.T) {
	base := time.Date(2026, 11, 20, 23, 3, 0, 0, time.UTC)
	a := &providerAvailability{}
	if a.observe("claude", refused(base), base) {
		t.Fatal("one refused request is not an outage")
	}
	if a.observe("claude", refused(base), base.Add(time.Second)) {
		t.Fatal("the same refusal seen again changed availability")
	}
	if !a.observe("claude", refused(base.Add(30*time.Second)), base.Add(31*time.Second)) {
		t.Fatal("a second refusal with no answer between did not start an outage")
	}
	outage, ok := a.outage("claude")
	if !ok || !outage.Since.Equal(base) || outage.Cause == "" {
		t.Fatalf("outage = %+v %v", outage, ok)
	}
	if _, ok := a.outage("codex"); ok {
		t.Fatal("another agent family is not affected")
	}
	if !a.observe("claude", answered(base.Add(2*time.Hour)), base.Add(2*time.Hour)) {
		t.Fatal("an answer did not end the outage")
	}
	if _, ok := a.outage("claude"); ok {
		t.Fatal("outage still listed after an answer")
	}
}

func TestOneDroppedRequestBetweenAnswersIsNotAnOutage(t *testing.T) {
	base := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	a := &providerAvailability{}
	a.observe("claude", refused(base), base)
	a.observe("claude", answered(base.Add(time.Minute)), base.Add(time.Minute))
	if a.observe("claude", refused(base.Add(2*time.Minute)), base.Add(2*time.Minute)) {
		t.Fatal("refusals separated by an answer started an outage")
	}
}

func TestOnlyTransientRefusalsCountAndOldOnesAreIgnored(t *testing.T) {
	base := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	a := &providerAvailability{}
	auth := func(at time.Time) ProviderActivityObservation {
		o := refused(at)
		o.Error = &ProviderActivityError{Code: "authentication_error", HTTPStatus: 401}
		return o
	}
	a.observe("claude", auth(base), base)
	a.observe("claude", auth(base.Add(time.Minute)), base.Add(time.Minute))
	if _, ok := a.outage("claude"); ok {
		t.Fatal("a sign-in failure is not a provider outage")
	}
	b := &providerAvailability{}
	now := base.Add(48 * time.Hour)
	b.observe("claude", refused(base), now)
	b.observe("claude", refused(base.Add(time.Minute)), now)
	if _, ok := b.outage("claude"); ok {
		t.Fatal("refusals from two days ago started an outage now")
	}
}

func TestAnUnavailableProviderGrantsOneProbePerGrowingInterval(t *testing.T) {
	base := time.Date(2026, 11, 20, 23, 3, 0, 0, time.UTC)
	a := &providerAvailability{}
	if ok, _ := a.probe("claude", base); !ok {
		t.Fatal("an available provider must always be asked")
	}
	a.observe("claude", refused(base), base)
	a.observe("claude", refused(base.Add(30*time.Second)), base.Add(30*time.Second))
	if ok, next := a.probe("claude", base.Add(time.Minute)); ok || !next.Equal(base.Add(30*time.Second+5*time.Minute)) {
		t.Fatalf("probe before the first interval = %v next %v", ok, next)
	}
	at := base.Add(30*time.Second + 5*time.Minute)
	if ok, _ := a.probe("claude", at); !ok {
		t.Fatal("the due probe was not granted")
	}
	if ok, _ := a.probe("claude", at); ok {
		t.Fatal("a second Session got the same probe")
	}
	want := []time.Duration{10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for _, gap := range want {
		if ok, _ := a.probe("claude", at.Add(gap-time.Second)); ok {
			t.Fatalf("probe granted before its %s interval", gap)
		}
		at = at.Add(gap)
		if ok, _ := a.probe("claude", at); !ok {
			t.Fatalf("probe after %s not granted", gap)
		}
	}
}

// byWorkerProbe answers each Worker with its own observation.
type byWorkerProbe map[string]ProviderActivityObservation

func (p byWorkerProbe) ObserveProviderActivity(worker classifier.Worker, _ time.Time) ProviderActivityObservation {
	return p[worker.ID]
}
func (byWorkerProbe) ForgetProviderActivity(string) {}

// The reviewer's P5/P6: a Worker reports done one second before its request
// finishes, so its Turn is settled when the provider's answer lands. While the
// family is in an outage every Session of the family is read, and that answer
// ends it.
func TestAnAnswerAfterItsTurnSettledEndsTheOutage(t *testing.T) {
	ledger := newFakeTurnLedger()
	w := New(0)
	w.SetTurnLedger(ledger)
	start := time.Date(2026, 11, 22, 6, 17, 0, 0, time.UTC)
	w.availability().observe("claude", refused(start.Add(-time.Minute)), start)
	w.availability().observe("claude", refused(start), start)
	ledger.turns["p5"] = TurnSnapshot{SessionID: "p5", TurnID: "turn:p5", Status: TurnDone, SignalProtocol: true}
	ledger.turns["c1"] = TurnSnapshot{SessionID: "c1", TurnID: "turn:c1", Status: TurnDone, SignalProtocol: true}
	finished := start.Add(5 * time.Minute)
	probe := byWorkerProbe{"p5": answered(finished), "c1": answered(finished)}
	item := func(id, command string) preparedPollWorker {
		return preparedPollWorker{id: id, workerSnap: classifier.Worker{ID: id, Command: command, Delegated: true}, alive: true, now: finished.Add(time.Second)}
	}
	// Another family's answer says nothing of Claude's provider.
	w.collectPollEvidence([]preparedPollWorker{item("c1", "codex resume c1")}, nil, nil, probe)
	if _, down := w.ProviderOutage("claude"); !down {
		t.Fatal("a Codex answer ended Claude's outage")
	}
	w.collectPollEvidence([]preparedPollWorker{item("p5", "claude --session-id p5")}, nil, nil, probe)
	if _, down := w.ProviderOutage("claude"); down {
		t.Fatal("an answer that finished after its Turn settled did not end the outage")
	}
	// With no outage a settled Turn is not read at all.
	probe["p5"] = refused(finished.Add(time.Minute))
	probe["p5b"] = refused(finished.Add(2 * time.Minute))
	ledger.turns["p5b"] = TurnSnapshot{SessionID: "p5b", TurnID: "turn:p5b", Status: TurnDone, SignalProtocol: true}
	w.collectPollEvidence([]preparedPollWorker{item("p5", "claude --session-id p5"), item("p5b", "claude --session-id p5b")}, nil, nil, probe)
	if _, down := w.ProviderOutage("claude"); down {
		t.Fatal("settled Turns were read outside an outage")
	}
}
