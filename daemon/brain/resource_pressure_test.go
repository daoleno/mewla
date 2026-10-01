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
