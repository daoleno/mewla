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

// Brain's host has no slice and no lease: the session host's inventory of
// panes is what names its processes.
func TestAProcessAPaneStartedIsItsSessionsAndBrainsHostIsBrain(t *testing.T) {
	records := map[int]workerproc.Process{300: {PPID: 200}, 200: {PPID: 100}, 100: {PPID: 1}, 400: {PPID: 100}}
	l := WhyLookup{Root: t.TempDir(),
		Pane: func(pid int) (WhySession, bool) {
			switch pid {
			case 200:
				return WhySession{ID: "mh-brain", Name: "Brain", Brain: true}, true
			case 400:
				return WhySession{ID: "mh-worker", Name: "fix the build"}, true
			}
			return WhySession{}, false
		},
		Work: func(id string) (WhyLink, bool) { return WhyLink{ID: "w-1", Name: "Build"}, id == "mh-worker" },
	}
	why := whyProcess(l, whyProc{pid: 300, name: "python3"}, records)
	if !why.Brain || !why.Mewla || len(why.Chain) != 2 || why.Chain[1].ID != "mh-brain" || why.Chain[1].By != "the Session's pane 200 started it" {
		t.Fatalf("Brain's child = %+v", why)
	}
	why = whyProcess(l, whyProc{pid: 400, name: "node"}, records)
	if why.Brain || len(why.Chain) != 3 || why.Chain[1].By != "it is the Session's pane" || why.Chain[2].ID != "w-1" {
		t.Fatalf("a Worker's pane = %+v", why)
	}
	if why = whyProcess(l, whyProc{pid: 100, name: "tmux"}, records); why.Mewla || why.Brain {
		t.Fatalf("the host itself = %+v", why)
	}
}
