package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

type recordingProbe struct{ calls int }

func (p *recordingProbe) ObserveProviderActivity(classifier.Worker, time.Time) watcher.ProviderActivityObservation {
	p.calls++
	return watcher.ProviderActivityObservation{ID: "from-transcript"}
}
func (p *recordingProbe) ForgetProviderActivity(string) {}

func TestEventProbeAnswersFromEventsOnlyForATurnItSawStart(t *testing.T) {
	hub := agentevents.NewHub()
	fallback := &recordingProbe{}
	probe := newEventActivityProbe(hub, fallback)
	now := time.Now().UTC()
	worker := classifier.Worker{ID: "%3"}

	// Nothing heard: the transcript answers.
	if got := probe.ObserveProviderActivity(worker, now); got.ID != "from-transcript" || fallback.calls != 1 {
		t.Fatalf("unheard worker: %+v calls=%d", got, fallback.calls)
	}
	// A turn it did not see begin (the daemon met the Worker mid-turn): still the transcript.
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnFinished, TurnID: "old", Status: agentevents.TurnCompleted, At: now})
	if got := probe.ObserveProviderActivity(worker, now); got.ID != "from-transcript" {
		t.Fatalf("unseen turn start: %+v", got)
	}
	// A turn it watched begin: events answer, and the transcript is not read.
	before := fallback.calls
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindUserMessage, ID: "p1", InputSHA256: "abc", At: now})
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnStarted, TurnID: "p1", At: now})
	got := probe.ObserveProviderActivity(worker, now)
	if got.ID != "p1" || got.Status != "running" || got.AdmissionID != "p1" || got.InputSHA256 != "abc" || got.AdmissionCursor == 0 || fallback.calls != before {
		t.Fatalf("running: %+v calls=%d", got, fallback.calls)
	}
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnFinished, TurnID: "p1", Status: agentevents.TurnCompleted, At: now.Add(time.Second)})
	got = probe.ObserveProviderActivity(worker, now)
	if got.Status != "completed" || got.SettledAt.IsZero() || len(got.TerminalActivities) != 2 || got.TerminalActivities[1].ID != "p1" {
		t.Fatalf("completed: %+v", got)
	}
}

func TestEventProbeReportsStructuredErrorsWithoutText(t *testing.T) {
	hub := agentevents.NewHub()
	probe := newEventActivityProbe(hub, &recordingProbe{})
	now := time.Now().UTC()
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnStarted, TurnID: "p1", At: now})
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnFinished, TurnID: "p1", Status: agentevents.TurnFailed, At: now,
		Error: &agentevents.TurnError{Class: agentevents.ErrorBilling, Code: "billing_error", Message: "your card was declined"}})
	got := probe.ObserveProviderActivity(classifier.Worker{ID: "%3"}, now)
	if got.Error == nil || got.Error.Code != "billing_error" || got.Error.Message != "" {
		t.Fatalf("error = %+v", got.Error)
	}
	if watcher.ClassifyProviderError(*got.Error) != watcher.ProviderErrorPersistent {
		t.Fatal("a billing error is not persistent")
	}
	hub.Publish(agentevents.Event{WorkerID: "%4", Kind: agentevents.KindTurnStarted, TurnID: "p2", At: now})
	hub.Publish(agentevents.Event{WorkerID: "%4", Kind: agentevents.KindTurnFinished, TurnID: "p2", Status: agentevents.TurnFailed, At: now,
		Error: &agentevents.TurnError{Class: agentevents.ErrorOverloaded}})
	got = probe.ObserveProviderActivity(classifier.Worker{ID: "%4"}, now)
	if watcher.ClassifyProviderError(*got.Error) != watcher.ProviderErrorTransient {
		t.Fatalf("an overloaded error is not transient: %+v", got.Error)
	}
}

func TestEventProbeForgetKeepsTheBinding(t *testing.T) {
	hub := agentevents.NewHub()
	probe := newEventActivityProbe(hub, &recordingProbe{})
	hub.Publish(agentevents.Event{WorkerID: "%3", Kind: agentevents.KindTurnStarted, TurnID: "p1", At: time.Now()})
	probe.ForgetProviderActivity("%3")
	if _, ok := hub.State("%3"); !ok {
		t.Fatal("a settled turn must not drop the Worker's events")
	}
}
