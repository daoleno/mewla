package gc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

const whyOwner = "0123456789abcdef0123456789abcdef"

// whyFixture is a state root with one Session: its lease and its marked
// scratch.
func whyFixture(t *testing.T) (root, unit, scratch string) {
	t.Helper()
	root = t.TempDir()
	unit = "mewla-worker-" + whyOwner + "-" + strings.Repeat("ab", 16) + ".scope"
	scratch = filepath.Join(root, "t", ScratchName(unit))
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, scratchMarker), []byte(unit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	leases := filepath.Join(root, "run", "worker-resources", whyOwner)
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(workerproc.Lease{Version: 1, ResourceID: unit, StartedAt: time.Now()})
	if err := os.WriteFile(filepath.Join(leases, unit+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, unit, scratch
}

func TestWhyNamesAnEndedSessionFromTheLedger(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	start := time.Now().Add(-time.Hour)
	BindSession(root, unit, "mh-gone", start)
	EndSession(root, unit, start.Add(30*time.Minute))
	lookup := WhyLookup{Root: root,
		Session: func(string) (WhySession, bool) { return WhySession{}, false }, // no live Session has it
		Work: func(session string) (WhyLink, bool) {
			return WhyLink{ID: "w-9", Name: "Ship it", State: "done"}, session == "mh-gone"
		},
	}
	why, err := WhyPath(lookup, filepath.Join(scratch, "left.txt"))
	if err != nil {
		t.Fatal(err)
	}
	last := len(why.Chain) - 1
	if why.Chain[last-1].ID != "mh-gone" || !strings.HasPrefix(why.Chain[last-1].State, "ended ") || why.Chain[last].ID != "w-9" {
		t.Fatalf("chain %+v", why.Chain)
	}
}
