package brain

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

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
	payload, err := json.Marshal(compactResourcePressureEvent(event))
	if err != nil {
		return err
	}
	id := resourcePressureWorkPrefix + event.SampledAt.UTC().Format("20060102T150405.000000000Z")
	if err = s.supersedeResourcePressureWork(id); err != nil {
		return err
	}
	// Recovery is informational: it closes unreviewed pressure Work above and
	// needs no Brain turn. Engaged Work stays with Brain.
	if event.State == "normal" {
		return nil
	}
	_, _, err = s.store.EnsureWork(Work{ID: id, Title: "Machine resource pressure: " + event.State, Objective: "Assess the machine resource event and decide whether any Worker action is needed.", CompletionPolicy: CompletionBounded, NextAction: "Decide whether to defer dispatch, ask a Worker to release resources, or close it."})
	if err != nil {
		return err
	}
	recorded, _, err := s.store.AppendWorkEvent(WorkEvent{WorkID: id, Kind: "resource_pressure", DedupeKey: id, SourceName: "machine resource telemetry", Summary: fmt.Sprintf("Machine pressure changed from %s to %s.", event.PreviousState, event.State), DetailsJSON: string(payload), PayloadRef: resourceSnapshotCommand, Actionable: true})
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

const (
	resourcePressureWorkPrefix    = "resource-pressure:"
	resourcePressureConsumerLimit = 3
	resourcePressureOrphanLimit   = 2
	resourceSnapshotCommand       = "zen resources --json"
)

// resourcePressureBrief is the Brain-facing event payload: the transition,
// crossed signals, machine headroom and the largest attributed consumers.
// Process rows, trend samples and PSI detail stay in zen resources --json,
// which Brain reads only when it acts.
type resourcePressureBrief struct {
	State         string                   `json:"state"`
	PreviousState string                   `json:"previous_state"`
	SampledAt     time.Time                `json:"sampled_at"`
	Crossed       []watcher.PressureSignal `json:"crossed,omitempty"`
	MemoryFreeGiB float64                  `json:"memory_available_gib,omitempty"`
	MemoryGiB     float64                  `json:"memory_total_gib,omitempty"`
	SwapUsedGiB   float64                  `json:"swap_used_gib,omitempty"`
	CPUPercent    float64                  `json:"cpu_percent,omitempty"`
	Load15        float64                  `json:"load15,omitempty"`
	Consumers     []resourceConsumerBrief  `json:"consumers,omitempty"`
	Orphaned      []resourceConsumerBrief  `json:"orphaned,omitempty"`
	Queued        int                      `json:"queued"`
	InFlight      int                      `json:"in_flight"`
	Snapshot      string                   `json:"snapshot"`
}

type resourceConsumerBrief struct {
	ID         string  `json:"id,omitempty"`
	WorkerID   string  `json:"worker_id,omitempty"`
	WorkID     string  `json:"work_id,omitempty"`
	Title      string  `json:"title,omitempty"`
	Owner      string  `json:"owner"`
	Status     string  `json:"status,omitempty"`
	RSSMiB     float64 `json:"rss_mib"`
	CPUPercent float64 `json:"cpu_percent,omitempty"`
	Processes  int     `json:"processes,omitempty"`
}

func compactResourcePressureEvent(event watcher.ResourcePressureEvent) resourcePressureBrief {
	brief := resourcePressureBrief{State: event.State, PreviousState: event.PreviousState, SampledAt: event.SampledAt, Queued: event.Queued, InFlight: event.InFlight, Snapshot: resourceSnapshotCommand}
	for _, signal := range event.Crossed {
		signal.Value = roundTo(signal.Value, 2)
		brief.Crossed = append(brief.Crossed, signal)
	}
	if memory := event.Memory; memory != nil {
		brief.MemoryFreeGiB = roundTo(float64(memory.AvailableBytes)/(1<<30), 1)
		brief.MemoryGiB = roundTo(float64(memory.TotalBytes)/(1<<30), 1)
		brief.SwapUsedGiB = roundTo(float64(memory.SwapUsedBytes)/(1<<30), 1)
	}
	if cpu := event.CPU; cpu != nil {
		if cpu.UtilizationPercent != nil {
			brief.CPUPercent = roundTo(*cpu.UtilizationPercent, 0)
		}
		brief.Load15 = roundTo(cpu.Load15, 1)
	}
	brief.Consumers = compactResourceConsumers(event.Consumers, resourcePressureConsumerLimit)
	brief.Orphaned = compactResourceConsumers(event.Orphaned, resourcePressureOrphanLimit)
	return brief
}

func compactResourceConsumers(consumers []watcher.ProcessConsumer, limit int) []resourceConsumerBrief {
	if len(consumers) > limit {
		consumers = consumers[:limit]
	}
	out := make([]resourceConsumerBrief, 0, len(consumers))
	for _, consumer := range consumers {
		item := resourceConsumerBrief{WorkerID: consumer.WorkerID, WorkID: consumer.WorkID, Owner: consumer.Owner, RSSMiB: roundTo(float64(consumer.RSSBytes)/(1<<20), 0), Processes: consumer.ProcessCount}
		if item.WorkerID == "" {
			item.ID = consumer.ID
		}
		if consumer.Status != "unknown" {
			item.Status = consumer.Status
		}
		// Worker titles repeat the Session id; the worker_id field carries it.
		item.Title = compactDirectWorkEventField(strings.TrimSpace(strings.TrimSuffix(consumer.Title, " ("+consumer.WorkerID+")")), 100)
		if consumer.CPUPercent != nil {
			item.CPUPercent = roundTo(*consumer.CPUPercent, 0)
		}
		out = append(out, item)
	}
	return out
}

func roundTo(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}

// supersedeResourcePressureWork cancels earlier pressure Work that Brain has
// not engaged: each event carries the full current state, so an unreviewed
// older transition is stale and would otherwise accumulate as open Work.
// Work with a delivery lease, an Attempt or a Brain decision is left alone.
func (s *Service) supersedeResourcePressureWork(currentID string) error {
	items, err := s.store.ListWork()
	if err != nil {
		return err
	}
	cancelled := WorkCancelled
	next := "Superseded by " + currentID + "."
	for _, item := range items {
		if item.ID == currentID || !strings.HasPrefix(item.ID, resourcePressureWorkPrefix) ||
			item.Status == WorkDone || item.Status == WorkCancelled ||
			item.AttemptSessionID != "" || item.Review == nil || item.Review.Lease != nil {
			continue
		}
		if _, err := s.UpdateWork(item.ID, WorkUpdate{Status: &cancelled, NextAction: &next}); err != nil {
			return err
		}
	}
	return nil
}

func resourcePressureDetails(kind, details string) json.RawMessage {
	if kind != "resource_pressure" || !json.Valid([]byte(details)) {
		return nil
	}
	return json.RawMessage(strings.TrimSpace(details))
}
