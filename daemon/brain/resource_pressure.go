package brain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

// ResourceWorkContext enriches read-only telemetry from canonical Work ownership.
// Reading this context never admits a turn or replaces a Brain host.
func (s *Service) ResourceWorkContext(consumers []watcher.ProcessConsumer) ([]watcher.ProcessConsumer, int, int, error) {
	if s == nil || s.store == nil {
		return consumers, 0, 0, nil
	}
	s.store.mu.Lock()
	database, err := s.store.loadPresentationLocked()
	s.store.mu.Unlock()
	if err != nil {
		return consumers, 0, 0, err
	}
	byWorker := map[string]Work{}
	byID := map[string]Work{}
	queued, inflight := 0, 0
	for _, item := range database.BrainWork {
		byID[item.ID] = item
		if item.AttemptSessionID != "" {
			old, ok := byWorker[item.AttemptSessionID]
			if !ok || item.UpdatedAt.After(old.UpdatedAt) {
				byWorker[item.AttemptSessionID] = item
			}
		}
		if item.Status == WorkOpen {
			queued++
		}
		if item.Status == WorkRunning {
			inflight++
		}
	}
	// Finished Work can have no current Attempt; accepted Turn lineage retains
	// its exact Session owner for orphan attribution without per-consumer reads.
	latest := map[string]TurnRecord{}
	for _, turn := range database.BrainTurns {
		old, ok := latest[turn.SessionID]
		if !ok || turn.AcceptedAt.After(old.AcceptedAt) {
			latest[turn.SessionID] = turn
		}
	}
	for id, turn := range latest {
		if _, ok := byWorker[id]; !ok {
			if item, ok := byID[turn.WorkID]; ok {
				byWorker[id] = item
			}
		}
	}

	out := append([]watcher.ProcessConsumer{}, consumers...)
	for i := range out {
		item, ok := byWorker[out[i].WorkerID]

		if ok && out[i].Owner != "brain" {
			out[i].WorkID = item.ID
			out[i].Title = item.Title
			if item.Status == WorkDone || item.Status == WorkCancelled {
				out[i].Status = "done"
				out[i].Owner = "orphaned_worker"
			}
		}
	}
	return out, queued, inflight, nil
}

// RouteResourcePressure records a producer event and uses the durable Host lane
// used by zen_work_event. It never enters the user-input admission channel.
func (s *Service) RouteResourcePressure(event watcher.ResourcePressureEvent) error {
	if s == nil || s.store == nil {
		return nil
	}
	consumers, queued, inflight, err := s.ResourceWorkContext(event.Consumers)
	if err != nil {
		return err
	}
	event.Consumers = consumers
	event.Queued = queued
	event.InFlight = inflight
	event.Orphaned, _, _, err = s.ResourceWorkContext(event.Orphaned)
	if err != nil {
		return err
	}
	// Per-core values live in the full snapshot; the Brain envelope stays small.
	if event.CPU != nil {
		cpu := *event.CPU
		cpu.PerCorePercent = nil
		event.CPU = &cpu
	}
	for i := range event.Consumers {
		event.Consumers[i].Title = compactDirectWorkEventField(event.Consumers[i].Title, 100)
		event.Consumers[i].Cwd = compactDirectWorkEventField(event.Consumers[i].Cwd, 160)
	}
	if len(event.Consumers) > 5 {
		event.Consumers = event.Consumers[:5]
	}
	if len(event.Orphaned) > 2 {
		event.Orphaned = event.Orphaned[:2]
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	for len(payload) > 7000 && len(event.Consumers) > 1 {
		event.Consumers = event.Consumers[:len(event.Consumers)-1]
		payload, _ = json.Marshal(event)
	}
	id := "resource-pressure:" + event.SampledAt.UTC().Format("20060102T150405.000000000Z")
	_, _, err = s.store.EnsureWork(Work{ID: id, Title: "Machine resource pressure: " + event.State, Objective: "Assess the machine resource event and decide whether any Worker action is needed.", CompletionPolicy: CompletionBounded, ContextRef: "get_resource_telemetry", NextAction: "Decide whether to defer dispatch, ask a Worker to release resources, or close it. No daemon resource intervention occurred."})
	if err != nil {
		return err
	}
	recorded, _, err := s.store.AppendWorkEvent(WorkEvent{WorkID: id, Kind: "resource_pressure", DedupeKey: id, SourceName: "machine resource telemetry", Summary: fmt.Sprintf("Machine pressure changed from %s to %s. Brain decides any resource action.", event.PreviousState, event.State), DetailsJSON: string(payload), PayloadRef: "get_resource_telemetry", Actionable: true})
	if err != nil {
		return err
	}
	if recorded.HandledAt != nil || recorded.DiscardedAt != nil {
		return nil
	}
	if _, err = s.store.FSM().OpenReviewEvent(lifecycle.WorkID(id), "resource_pressure", recorded.ID, recorded.ID); err != nil {
		return err
	}
	if err = s.store.SyncWorkProjection(id); err != nil {
		return err
	}
	_, err = s.ReconcileHostLane()
	return err
}

func resourcePressureDetails(kind, details string) json.RawMessage {
	if kind != "resource_pressure" || !json.Valid([]byte(details)) {
		return nil
	}
	return json.RawMessage(strings.TrimSpace(details))
}
