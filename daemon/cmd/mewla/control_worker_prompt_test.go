package main

import (
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// `mewla worker status` carries the id of the prompt open in the Worker's
// terminal, or why a dialog that is open has none.
func TestControlWorkerCarriesThePromptIDOrWhyThereIsNone(t *testing.T) {
	identified := controlWorker(&classifier.Worker{ID: "w", Prompt: &classifier.Prompt{ID: "abc123"}})
	if identified.PromptID != "abc123" || identified.PromptUnidentified != "" {
		t.Fatalf("%+v", identified)
	}
	unidentified := controlWorker(&classifier.Worker{ID: "w", PromptUnidentified: "the screen does not show where the dialog starts"})
	if unidentified.PromptID != "" || unidentified.PromptUnidentified == "" {
		t.Fatalf("%+v", unidentified)
	}
}
