package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const lifecycleStoreSchema = 2

// lifecycleDatabase is the single durable transaction image. Current rows and
// append-only Events are replaced together, so recovery never replays a second
// log to repair a separate projection.
type lifecycleDatabase struct {
	Schema  int               `json:"schema"`
	NextSeq uint64            `json:"next_seq"`
	Works   map[WorkID]*State `json:"works"`
	Events  []Event           `json:"events"`
}

func readLifecycleDatabase(path string) (lifecycleDatabase, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return lifecycleDatabase{Schema: lifecycleStoreSchema, NextSeq: 1, Works: map[WorkID]*State{}}, nil
	}
	if err != nil {
		return lifecycleDatabase{}, err
	}
	database := lifecycleDatabase{}
	if err := json.Unmarshal(raw, &database); err != nil {
		return lifecycleDatabase{}, fmt.Errorf("decode lifecycle store: %w", err)
	}
	return database, validateLifecycleDatabase(database)
}

func validateLifecycleDatabase(database lifecycleDatabase) error {
	if database.Schema != lifecycleStoreSchema {
		return fmt.Errorf("lifecycle: unsupported store schema %d", database.Schema)
	}
	if database.Works == nil || database.Events == nil {
		return fmt.Errorf("lifecycle: works and events are required")
	}
	var maxSeq uint64
	for _, event := range database.Events {
		if event.Seq <= maxSeq {
			return fmt.Errorf("lifecycle: event sequence is not strictly increasing")
		}
		maxSeq = event.Seq
	}
	if database.NextSeq <= maxSeq {
		return fmt.Errorf("lifecycle: next event sequence is stale")
	}
	for id, work := range database.Works {
		if work == nil || work.ID != id {
			return fmt.Errorf("lifecycle: Work row %q has mismatched identity", id)
		}
		if work.Status.Terminal() && work.Wake != nil {
			return fmt.Errorf("lifecycle: terminal Work %q retains a wake", id)
		}
		if work.Attempt != nil && (work.Attempt.SessionID == "" || work.Attempt.TurnToken == "" || work.Attempt.Generation == 0) {
			return fmt.Errorf("lifecycle: Work %q has incomplete active Attempt identity", id)
		}
		prepared := 0
		acceptedSequences := make(map[uint64]bool)
		for token, admission := range work.Admissions {
			if admission == nil || token == "" || token != admission.TurnToken || admission.SessionID == "" || admission.AttemptedAt.IsZero() {
				return fmt.Errorf("lifecycle: Work %q has incomplete Attempt admission", id)
			}
			// Ambiguous outcomes remain evidence across model-directed retries.
			// Only a prepared transaction still owns submission serialization.
			if admission.Status == AdmissionPrepared {
				prepared++
			}
			if admission.Status == AdmissionAccepted {
				if admission.AcceptedSeq == 0 || admission.AcceptedSeq >= database.NextSeq || acceptedSequences[admission.AcceptedSeq] {
					return fmt.Errorf("lifecycle: Work %q has invalid admission acceptance sequence", id)
				}
				acceptedSequences[admission.AcceptedSeq] = true
			}
		}
		if prepared > 1 {
			return fmt.Errorf("lifecycle: Work %q has multiple prepared admissions", id)
		}
	}
	return nil
}

func writeLifecycleDatabase(path string, database lifecycleDatabase) error {
	database.Schema = lifecycleStoreSchema
	if database.NextSeq == 0 {
		database.NextSeq = 1
	}
	raw, err := json.Marshal(database)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0o600)
}

// Protocol timing constants. Fixed so lifecycle decisions are deterministic.
const (
	LeaseGrace    = 10 * time.Minute
	LostGrace     = 30 * time.Minute
	EventClaimTTL = 2 * time.Minute
)
