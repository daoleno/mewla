package brain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestResourcePressureUsesDurableEventLaneAndKeepsPayload(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "resource-test-thread"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	now := time.Now().UTC()
	event := watcher.ResourcePressureEvent{State: "elevated", PreviousState: "normal", SampledAt: now, SnapshotEndpoint: "get_resource_telemetry", Memory: &watcher.MemoryResource{TotalBytes: 1000, AvailableBytes: 100}, Consumers: []watcher.ProcessConsumer{{WorkerID: "worker1", Owner: "worker", Commands: []string{"qemu-system-x86"}, RSSBytes: 700}}}
	// A missing host leaves the event pending; no user-input path is involved.
	_ = service.RouteResourcePressure(event)
	items, err := store.ListWork()
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	events, err := store.ListWorkEvents(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var found WorkEvent
	for _, e := range events {
		if e.Kind == "resource_pressure" {
			found = e
		}
	}
	if found.ID == "" || !found.Actionable || !json.Valid([]byte(found.DetailsJSON)) {
		t.Fatalf("missing durable resource event %+v", events)
	}
	_ = service.RouteResourcePressure(event)
	again, _ := store.ListWorkEvents(items[0].ID)
	if len(again) != len(events) {
		t.Fatal("duplicate transition event")
	}
	action := WorkReviewAction{EventID: found.ID, WorkID: items[0].ID, Kind: found.Kind, DetailsJSON: found.DetailsJSON, DeliveryWorkRevision: 1, HandlingID: "handling", ProviderTurnID: "turn"}
	text, err := marshalDirectWorkEventInput(action, items[0])
	if err != nil {
		t.Fatal(err)
	}
	input, ok := work.ParseCanonicalDirectWorkEventInput(text)
	if !ok || !work.IsDirectWorkEventPresentationInput(text) {
		t.Fatal("not a reserved event envelope")
	}
	var payload watcher.ResourcePressureEvent
	if err = json.Unmarshal(input.ResourcePressure, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.State != "elevated" || payload.Memory.AvailableBytes != 100 || len(payload.Consumers) != 1 || payload.Consumers[0].WorkerID != "worker1" {
		t.Fatal(payload)
	}
	event.State = "normal"
	event.PreviousState = "elevated"
	event.SampledAt = now.Add(time.Minute)
	_ = service.RouteResourcePressure(event)
	items, _ = store.ListWork()
	if len(items) != 2 {
		t.Fatal("recovery not independently queued", items)
	}
}

func TestResourcePressureSupersedesOnlyUnengagedEarlierWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "resource-test-thread"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	now := time.Now().UTC()
	route := func(state string, at time.Time) string {
		_ = service.RouteResourcePressure(watcher.ResourcePressureEvent{State: state, PreviousState: "normal", SampledAt: at, SnapshotEndpoint: "get_resource_telemetry"})
		return resourcePressureWorkPrefix + at.UTC().Format("20060102T150405.000000000Z")
	}
	status := func(id string) WorkStatus {
		item, err := store.Work(id)
		if err != nil {
			t.Fatal(err)
		}
		return item.Status
	}

	first := route("elevated", now)
	second := route("critical", now.Add(time.Minute))
	if status(first) != WorkCancelled {
		t.Fatalf("unreviewed earlier pressure Work stays %s", status(first))
	}
	if s := status(second); s == WorkDone || s == WorkCancelled {
		t.Fatalf("latest pressure Work must stay open, got %s", status(second))
	}

	// A Host delivery lease means Brain is handling it; a newer event must not
	// cancel it underneath that turn.
	if _, claimed, err := store.ClaimNextReviewAction("host:@1"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	third := route("normal", now.Add(2*time.Minute))
	if status(second) == WorkCancelled || status(third) == WorkCancelled {
		t.Fatalf("engaged=%s latest=%s", status(second), status(third))
	}
}
