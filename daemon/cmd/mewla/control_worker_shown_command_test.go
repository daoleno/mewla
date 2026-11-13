package main

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// mewla worker status/list/spawn show the launch without Mewla's hooks.
func TestControlWorkerShowsTheLaunchWithoutMewlaHooks(t *testing.T) {
	full := `claude --settings '{"env":{"A":"b"},"hooks":{"Stop":[{"hooks":[{"command":"/bin/mewla agent-event -state-dir /s claude","timeout":10,"type":"command"}]}]}}' --resume 68197d1b-d079-4cd2-9c44-c93a18189b6e`
	got := controlWorker(&classifier.Worker{ID: "mh-1", Command: full}).Command
	if strings.Contains(got, "agent-event") || !strings.Contains(got, "--resume 68197d1b-d079-4cd2-9c44-c93a18189b6e") {
		t.Fatalf("shown command: %s", got)
	}
}
