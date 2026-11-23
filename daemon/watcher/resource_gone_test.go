//go:build linux || darwin

package watcher

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/workerproc"
)

var goneTestHost = sessionHostIdentity{PID: 4_194_000, StartedAt: time.Unix(1_790_000_000, 0)}

type goneTestClock struct{ at time.Time }

func (c *goneTestClock) now() time.Time          { return c.at }
func (c *goneTestClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// newGoneTestManager returns a manager whose clock starts well after the
// leases it makes, so they are past the young-lease guard.
func newGoneTestManager(t *testing.T) (*portableDelegatedResourceManager, *goneTestClock) {
	t.Helper()
	m := newTestPortableResourceManager(t, "abc123")
	clock := &goneTestClock{at: time.Now().Add(10 * time.Minute)}
	m.now = clock.now
	return m, clock
}

func addGoneTestUnit(t *testing.T, m *portableDelegatedResourceManager, token string) (string, string) {
	t.Helper()
	unit := delegatedResourceUnit(m.owner, token)
	tempDir, err := m.createOwnedTempDir(unit)
	if err != nil {
		t.Fatal(err)
	}
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		t.Fatal(err)
	}
	if err := workerproc.WriteOwnershipLease(path, unit); err != nil {
		t.Fatal(err)
	}
	return unit, tempDir
}

func leaseExists(t *testing.T, m *portableDelegatedResourceManager, unit string) bool {
	t.Helper()
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(path)
	return err == nil
}

// startUnitProcess starts a process carrying unit, as a Session's tool would.
func startUnitProcess(t *testing.T, unit string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, delegatedResourceUnitEnv+"=") && !strings.HasPrefix(entry, "MEWLA_WORKER_ID=") {
			env = append(env, entry)
		}
	}
	cmd.Env = append(env, delegatedResourceUnitEnv+"="+unit)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	// The ownership reader must see the process's environment.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		records, err := workerproc.Processes(true)
		if err == nil && records[cmd.Process.Pid].ResourceID == unit {
			return cmd, exited
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d never read as owned by %s", cmd.Process.Pid, unit)
	return nil, nil
}

func waitGoneIdle(t *testing.T, m *portableDelegatedResourceManager) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		busy := m.gone.releasing
		m.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("release of gone Sessions never finished")
}

// A Session whose host went away (a reboot, a host that stopped or was
// replaced) leaves its lease, its processes and its scratch. Before this, only
// KillSession released them, so they stayed forever.
func TestReleaseGoneReleasesTheLeaseOfASessionItsHostNoLongerHas(t *testing.T) {
	m, clock := newGoneTestManager(t)
	unit, tempDir := addGoneTestUnit(t, m, "0123456789abcdef0123456789abcdef")
	_, exited := startUnitProcess(t, unit)

	observe := func() {
		m.releaseGone(nil, goneTestHost)
		waitGoneIdle(t, m)
	}
	observe()
	if !leaseExists(t, m, unit) {
		t.Fatal("released on the first inventory that missed the Session")
	}
	clock.advance(goneScanEvery)
	observe()
	if !leaseExists(t, m, unit) {
		t.Fatal("released before the Session was missing for goneConfirm")
	}
	clock.advance(goneConfirm)
	observe()

	if leaseExists(t, m, unit) {
		t.Fatal("lease of a gone Session is still there")
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the gone Session's process is still running")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("scratch still at %s: %v", tempDir, err)
	}
	items, err := gc.List(m.stateRoot())
	if err != nil || len(items) != 1 {
		t.Fatalf("trash = %+v, %v", items, err)
	}
	if items[0].Owner != unit || items[0].Reason != releaseReasonGone || items[0].Original != tempDir {
		t.Fatalf("trash item = %+v", items[0])
	}
	if _, err := gc.Restore(m.stateRoot(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if marked, ok := readDelegatedTempMarker(tempDir); !ok || marked != unit {
		t.Fatalf("restored scratch marker = %q, %v", marked, ok)
	}
}

func TestReleaseGoneNeedsTwoInventoriesFromOneHost(t *testing.T) {
	type step struct {
		advance time.Duration
		windows []sessionPane
		host    sessionHostIdentity
	}
	unitToken := "fedcba9876543210fedcba9876543210"
	unit := delegatedResourceUnit("abc123", unitToken)
	other := sessionHostIdentity{PID: goneTestHost.PID + 1, StartedAt: goneTestHost.StartedAt.Add(time.Hour)}
	cases := []struct {
		name    string
		reserve bool
		steps   []step
		due     bool
	}{
		{
			name:  "missing twice, goneConfirm apart, one host",
			steps: []step{{0, nil, goneTestHost}, {goneConfirm, nil, goneTestHost}},
			due:   true,
		},
		{
			name:  "missing for less than goneConfirm",
			steps: []step{{0, nil, goneTestHost}, {goneConfirm - time.Second, nil, goneTestHost}},
		},
		{
			name: "carried by a live Session",
			steps: []step{
				{0, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit}}, goneTestHost},
				{goneConfirm, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit}}, goneTestHost},
				{goneConfirm, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit}}, goneTestHost},
			},
		},
		{
			name: "carried by a Session whose program exited (it can be relaunched)",
			steps: []step{
				{0, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit, dead: true}}, goneTestHost},
				{goneConfirm, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit, dead: true}}, goneTestHost},
			},
		},
		{
			name: "back on the inventory between two misses",
			steps: []step{
				{0, nil, goneTestHost},
				{goneScanEvery, []sessionPane{{target: "mh-1", delegated: true, resourceUnit: unit}}, goneTestHost},
				{goneConfirm, nil, goneTestHost},
			},
		},
		{
			name:  "the host changed between the two misses",
			steps: []step{{0, nil, goneTestHost}, {goneConfirm, nil, other}},
		},
		{
			name:  "no known host",
			steps: []step{{0, nil, sessionHostIdentity{}}, {goneConfirm, nil, sessionHostIdentity{}}},
		},
		{
			name:    "a reservation not yet bound",
			reserve: true,
			steps:   []step{{0, nil, goneTestHost}, {goneConfirm, nil, goneTestHost}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, clock := newGoneTestManager(t)
			addGoneTestUnit(t, m, unitToken)
			if tc.reserve {
				m.reserved[unit] = clock.now().Add(time.Hour)
			}
			var due []workerproc.Lease
			for _, s := range tc.steps {
				clock.advance(s.advance)
				due = m.goneCandidates(s.windows, s.host, clock.now())
				m.gone.releasing = false
			}
			if got := len(due) == 1 && due[0].ResourceID == unit; got != tc.due {
				t.Fatalf("due = %+v, want due %v", due, tc.due)
			}
		})
	}
}

func TestReleaseGoneSkipsAYoungLease(t *testing.T) {
	m := newTestPortableResourceManager(t, "abc123")
	unit, _ := addGoneTestUnit(t, m, "00112233445566778899aabbccddeeff")
	now := time.Now()
	m.goneCandidates(nil, goneTestHost, now)
	if due := m.goneCandidates(nil, goneTestHost, now.Add(goneConfirm-time.Second)); len(due) != 0 {
		t.Fatalf("young lease %s was due: %+v", unit, due)
	}
}

// The inventory can be wrong (a host answering mid-change); a lease whose
// process is a pane of the host is a live Session whatever the list said.
func TestReleaseGoneKeepsALeaseWithALivePane(t *testing.T) {
	m, clock := newGoneTestManager(t)
	unit, tempDir := addGoneTestUnit(t, m, "0123456789abcdef0123456789abcdef")
	cmd, exited := startUnitProcess(t, unit)
	// This test process plays the session host: the unit's process is its child.
	host := sessionHostIdentity{PID: os.Getpid(), StartedAt: time.Unix(1_790_000_000, 0)}

	m.releaseGone(nil, host)
	waitGoneIdle(t, m)
	clock.advance(goneConfirm)
	m.releaseGone(nil, host)
	waitGoneIdle(t, m)

	if !leaseExists(t, m, unit) {
		t.Fatal("released the lease of a Session with a live pane")
	}
	select {
	case <-exited:
		t.Fatalf("pane process %d was stopped", cmd.Process.Pid)
	default:
	}
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("scratch of a live Session moved: %v", err)
	}
}

// A process still running from a gone Session's scratch (here its cwd; an
// escaped daemon's exe is the same) keeps the scratch and the lease: moving
// the directory would take the evidence of whose that process is. Once the
// process is gone the next scan releases.
func TestReleaseKeepsScratchAProcessStillUses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scratch users are read from /proc")
	}
	m, clock := newGoneTestManager(t)
	unit, tempDir := addGoneTestUnit(t, m, "0123456789abcdef0123456789abcdef")
	// No unit in its environment: the release cannot stop it (an escaped
	// process, as PR 2 will contain).
	user := exec.Command("sleep", "60")
	user.Dir = tempDir
	user.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := user.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = user.Wait(); close(exited) }()
	t.Cleanup(func() { _ = user.Process.Kill(); <-exited })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := scratchUser(tempDir); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the process never read as a user of the scratch")
		}
		time.Sleep(10 * time.Millisecond)
	}

	scan := func() {
		m.releaseGone(nil, goneTestHost)
		waitGoneIdle(t, m)
		clock.advance(goneConfirm)
	}
	scan()
	scan()
	if !leaseExists(t, m, unit) {
		t.Fatal("lease released while a process still uses the scratch")
	}
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("scratch moved while a process still uses it: %v", err)
	}
	if !strings.Contains(m.keptLogged[unit], "is still using it") {
		t.Fatalf("kept line = %q", m.keptLogged[unit])
	}
	// The closed path keeps them too, and says the release is done.
	if err := m.Release("", unit); err != nil || !leaseExists(t, m, unit) {
		t.Fatalf("closed release: err=%v lease=%v", err, leaseExists(t, m, unit))
	}

	_ = user.Process.Kill()
	<-exited
	scan()
	if leaseExists(t, m, unit) {
		t.Fatal("lease kept after the process using the scratch exited")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("scratch not moved after the process exited: %v", err)
	}
	if items, err := gc.List(m.stateRoot()); err != nil || len(items) != 1 {
		t.Fatalf("trash = %+v, %v", items, err)
	}
}

// The Session's slice is stopped first: what it held (here a process with no
// unit in its environment, which the owned-process match cannot find) is gone
// before the scratch is checked, so the scratch goes to the trash at once.
func TestReleaseStopsTheSessionSliceFirst(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scratch users are read from /proc")
	}
	m, _ := newGoneTestManager(t)
	unit, tempDir := addGoneTestUnit(t, m, "0123456789abcdef0123456789abcdef")
	escaped := exec.Command("sleep", "60")
	escaped.Dir = tempDir
	escaped.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := escaped.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = escaped.Wait(); close(exited) }()
	t.Cleanup(func() { _ = escaped.Process.Kill(); <-exited })

	var stopped []string
	previous := stopSessionSlice
	stopSessionSlice = func(u string) ([]string, error) {
		stopped = append(stopped, u)
		_ = escaped.Process.Kill() // what stopping the slice does to it
		<-exited
		return []string{"slice-of-" + u}, nil
	}
	t.Cleanup(func() { stopSessionSlice = previous })

	if err := m.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != unit {
		t.Fatalf("slices stopped for %v, want [%s]", stopped, unit)
	}
	if leaseExists(t, m, unit) {
		t.Fatal("lease kept although the slice stop ended the scratch's user")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("scratch not moved: %v", err)
	}
}
