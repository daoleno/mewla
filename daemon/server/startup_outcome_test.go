package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/daoleno/mewla/daemon/watcher"
)

func TestSignalSystemStartupStopsRetryingAfterOnlyRefusals(t *testing.T) {
	if done, refused := signalSystemStartupOutcome(true, nil); !done || refused {
		t.Fatalf("clean pass: done=%v refused=%v", done, refused)
	}
	if done, _ := signalSystemStartupOutcome(false, nil); done {
		t.Fatal("an incomplete pass must run again")
	}
	transient := errors.New("sqlite busy")
	if done, refused := signalSystemStartupOutcome(true, transient); done || refused {
		t.Fatalf("a transient failure must be retried: done=%v refused=%v", done, refused)
	}
	// A refusal that was not marked by the cleanup (a bare sentinel) is not final.
	bare := fmt.Errorf("Session %%6 is %w", watcher.ErrNotOwnedDelegatedWorker)
	if done, _ := signalSystemStartupOutcome(true, bare); done {
		t.Fatal("only refusals the cleanup recorded may end the retry")
	}
}
