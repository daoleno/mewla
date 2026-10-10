package watcher

import (
	"testing"
	"time"
)

// Brain and watcher build provider facts through these helpers, so both sides
// anchor a missing admission timestamp on the activity start and derive the
// same FactID source for the same observation.
func TestAdmissionFromObservationAnchorsOnActivityStart(t *testing.T) {
	started := time.Date(2026, 10, 10, 8, 0, 0, 0, time.FixedZone("x", 3600))
	observation := ProviderActivityObservation{
		ID: "activity-1", StartedAt: started,
		AdmissionStream: " provider ", AdmissionID: "admission-1", AdmissionCursor: 7,
	}
	admission := AdmissionFromObservation(observation)
	if !admission.At.Equal(started) || admission.At.Location() != time.UTC || admission.Stream != "provider" {
		t.Fatalf("admission = %+v", admission)
	}
	explicit := started.Add(-time.Second)
	observation.AdmissionAt = explicit
	if got := AdmissionFromObservation(observation).At; !got.Equal(explicit) {
		t.Fatalf("explicit admission time replaced: %v", got)
	}
	if ProviderFactSourceID(" %3 ", observation) != ProviderFactSourceID("%3", observation) {
		t.Fatal("source identity depends on session ID padding")
	}
}
