package gc

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/userunit"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// Why is what holds a process or a path: the chain of owners the runtime
// recorded, the thing itself first and its outermost owner last. Mewla is
// false when no Mewla Session or state holds it.
type Why struct {
	Chain []WhyLink `json:"chain"`
	Mewla bool      `json:"mewla"`
	// Brain is true for a process of Brain's host: its own pane or what that
	// started.
	Brain bool `json:"brain,omitempty"`
}

// WhyLink is one step of the chain.
type WhyLink struct {
	// Kind is process, path, scratch, session, work, kept, trash, cache,
	// worktree, state, outside or unheld.
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
	// By is the evidence that ties this step to the one before it.
	By string `json:"by,omitempty"`
}

// WhySession is what the daemon knows of a live Session. Brain marks
// Brain's own host.
type WhySession struct {
	ID, Name, State string
	Brain           bool
}

// WhyLookup is where the chain is read: the state root, and the daemon's
// view of its live Sessions and their Work. Session and Work may be nil (no
// daemon to ask): the chain then stops at the resource unit.
type WhyLookup struct {
	Root    string
	Session func(unit string) (WhySession, bool)
	Work    func(sessionID string) (WhyLink, bool)
	// Pane is the live Session whose pane process is pid (the daemon's
	// inventory of its session host).
	Pane func(pid int) (WhySession, bool)
}

// ErrNoProcess is returned for a pid no process has.
var ErrNoProcess = errors.New("no such process")

// WhyPath tells what holds path.
func WhyPath(l WhyLookup, path string) (Why, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Why{}, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	root := l.Root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	subject := WhyLink{Kind: "path", ID: abs}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Why{Chain: []WhyLink{subject, {Kind: "outside", Name: "outside Mewla's state directory"}}}, nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	why := Why{Mewla: true}
	state := func(name string) Why {
		why.Chain = []WhyLink{subject, {Kind: "state", ID: root, Name: name}}
		return why
	}
	switch {
	case rel == ".":
		return state("Mewla's state directory"), nil
	case parts[0] == "t" && len(parts) == 1:
		return state("the scratch root"), nil
	case parts[0] == "t":
		scratch := filepath.Join(root, "t", parts[1])
		unit, marked := markerUnit(scratch)
		if !marked {
			if subject.State = keptState(l.Root, abs); subject.State == "" {
				subject.State = unmarkedState(l.Root, parts[1])
			}
			why.Chain = []WhyLink{subject}
			return why, nil
		}
		subject.State = keptState(l.Root, abs)
		why.Chain = []WhyLink{subject}
		if abs != scratch {
			why.Chain = append(why.Chain, WhyLink{Kind: "scratch", ID: scratch, By: "it is inside"})
		}
		why.Chain = append(why.Chain, l.sessionChain(unit, "its marker names the Session")...)
		return why, nil
	case parts[0] == "gc" && len(parts) >= 3 && parts[1] == "kept":
		keeps, err := ListKeeps(l.Root)
		if err != nil {
			return Why{}, err
		}
		for _, k := range keeps {
			if k.ID == parts[2] {
				why.Chain = append([]WhyLink{subject, {Kind: "kept", ID: k.ID, Name: k.Why, State: "until " + k.Until.Local().Format("2006-01-02")}},
					l.sessionChain(k.Owner, "it kept this")...)
				return why, nil
			}
		}
		return state("a keep that has ended"), nil
	case parts[0] == "gc" && len(parts) >= 3 && parts[1] == "trash":
		items, err := List(l.Root)
		if err != nil {
			return Why{}, err
		}
		for _, item := range items {
			if at := resolved(item.Path); abs == at || abs == at+".json" || within(abs, at) {
				why.Chain = []WhyLink{subject, {Kind: "trash", ID: item.ID, Name: item.Original,
					State: "deleted after " + item.DeleteAfter().Local().Format("2006-01-02 15:04"), By: item.Reason}}
				if item.Owner != "" {
					why.Chain = append(why.Chain, l.sessionChain(item.Owner, "it was this Session's")...)
				}
				return why, nil
			}
		}
		return state("the trash"), nil
	case parts[0] == "gc" && len(parts) >= 2 && parts[1] == "cache":
		why.Chain = []WhyLink{subject, {Kind: "cache", ID: CacheDir(root), Name: "shared cache; no Session owns it"}}
		return why, nil
	case parts[0] == "worktrees" && len(parts) >= 2:
		why.Chain = []WhyLink{subject, {Kind: "worktree", ID: filepath.Join(root, "worktrees", parts[1]), Name: "no owner recorded"}}
		return why, nil
	default:
		return state("Mewla state"), nil
	}
}

// whyProcess builds a process's chain from what was read of it.
func whyProcess(l WhyLookup, p whyProc, records map[int]workerproc.Process) Why {
	subject := WhyLink{Kind: "process", ID: itoa(p.pid), Name: p.name}
	leases := readLeases(l.Root)
	why := Why{Mewla: true}
	held := func(by string, unit string, between ...WhyLink) Why {
		why.Chain = append(append([]WhyLink{subject}, between...), l.sessionChain(unit, by)...)
		return why
	}
	// The runtime's own records first: the Session's slice, then a process
	// its lease recorded (hosts that place no slice).
	for i := len(p.sessionTokens) - 1; i >= 0; i-- {
		if unit := leases.unitOfToken(p.sessionTokens[i]); unit != "" {
			by := "it runs in the Session's slice"
			if i < len(p.sessionTokens)-1 {
				by = "it runs in a nested Mewla's Session inside this Session's slice"
			}
			return held(by, unit)
		}
	}
	for pid, seen := p.pid, 0; pid > 1 && seen < 64; seen++ {
		record, ok := records[pid]
		if !ok {
			break
		}
		if unit := leases.observedBy(workerproc.ProcessIdentity{PID: pid, Start: record.Start}); unit != "" {
			by := "the Session's lease recorded it"
			if pid != p.pid {
				by = "the Session's lease recorded pid " + itoa(pid) + ", which started it"
			}
			return held(by, unit)
		}
		pid = record.PPID
	}
	// Then the session host's own inventory: a process a live Session's pane
	// started is that Session's. Brain's host has no slice and no lease, so
	// this is where Brain's processes are known.
	if l.Pane != nil {
		for pid, seen := p.pid, 0; pid > 1 && seen < 64; seen++ {
			if s, ok := l.Pane(pid); ok {
				by := "it is the Session's pane"
				if pid != p.pid {
					by = "the Session's pane " + itoa(pid) + " started it"
				}
				why.Chain = []WhyLink{subject, {Kind: "session", ID: s.ID, Name: s.Name, State: s.State, By: by}}
				if l.Work != nil && !s.Brain {
					if w, ok := l.Work(s.ID); ok {
						w.Kind = "work"
						why.Chain = append(why.Chain, w)
					}
				}
				why.Brain = s.Brain
				return why
			}
			record, ok := records[pid]
			if !ok {
				break
			}
			pid = record.PPID
		}
	}
	// Then where it runs from: an owned scratch holds what runs out of it,
	// whatever its environment says (setsid env -i keeps none).
	for _, place := range p.places {
		scratch, ok := scratchOf(l.Root, place.path)
		if !ok {
			continue
		}
		if unit, ok := markerUnit(scratch); ok {
			return held("its marker names the Session", unit, WhyLink{Kind: "scratch", ID: scratch, By: place.by})
		}
	}
	if p.envUnit != "" {
		if leases.has(p.envUnit) {
			return held("only its environment names the Session", p.envUnit)
		}
		why.Chain = []WhyLink{subject, {Kind: "unheld", ID: p.envUnit, Name: "another Mewla's Session"}}
		why.Mewla = false
		return why
	}
	return Why{Chain: []WhyLink{subject, {Kind: "unheld", Name: "no Mewla Session holds it"}}}
}

// whyProc is what WhyProcess reads of a process.
type whyProc struct {
	pid           int
	name          string
	sessionTokens []string // from its cgroup, outermost first
	places        []whyPlace
	envUnit       string
}

type whyPlace struct{ path, by string }

func (l WhyLookup) sessionChain(unit, by string) []WhyLink {
	link := WhyLink{Kind: "session", ID: unit, By: by}
	if l.Session != nil {
		if s, ok := l.Session(unit); ok {
			link.ID, link.Name, link.State = s.ID, s.Name, s.State
			chain := []WhyLink{link}
			if l.Work != nil {
				if w, ok := l.Work(s.ID); ok {
					w.Kind = "work"
					chain = append(chain, w)
				}
			}
			return chain
		}
	}
	held := readLeases(l.Root).has(unit)
	recorded, known, _ := SessionOfUnit(l.Root, unit)
	ended := l.Session != nil || (known && !recorded.EndedAt.IsZero())
	switch {
	case !ended && held:
		link.State = "lease held" // no daemon asked: alive or not is not known
	case !ended:
		link.State = "released"
	case held:
		link.State = "ended; lease not released yet"
	default:
		link.State = "ended"
	}
	if !known {
		return []WhyLink{link}
	}
	// The ledger names the Session the unit was, after it ended too.
	link.ID = recorded.SessionID
	if !recorded.EndedAt.IsZero() {
		link.State = "ended " + recorded.EndedAt.Local().Format("2006-01-02 15:04")
	}
	if recorded.HeldBecause != "" {
		link.State += "; held: " + recorded.HeldBecause
	}
	chain := []WhyLink{link}
	if l.Work != nil {
		if w, ok := l.Work(recorded.SessionID); ok {
			w.Kind = "work"
			chain = append(chain, w)
		}
	}
	return chain
}

func unmarkedState(root, name string) string {
	if r, ok := ReviewOf(root, filepath.Join(root, "t", name)); ok && (r.Answer == "" || r.Answer == AnswerKeep) {
		return "unmarked; " + ReviewWhy(r)
	}
	if raw, err := os.ReadFile(snapshotPath(root)); err == nil && strings.Contains(string(raw), `"`+name+`"`) {
		return "unmarked; there before the t/ rule, left alone"
	}
	return "unmarked; collected once unused and untouched for 48 h"
}

func keptState(root, path string) string {
	keeps, err := ListKeeps(root)
	if err != nil {
		return ""
	}
	for _, k := range keeps {
		if k.Moved == "" && (path == k.Path || within(path, k.Path)) {
			return "kept until " + k.Until.Local().Format("2006-01-02") + ": " + k.Why
		}
	}
	return ""
}

// scratchOf is the t/<name> scratch path is in, if any.
func scratchOf(root, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	path = strings.TrimSuffix(path, " (deleted)")
	for _, base := range []string{root, resolved(root)} {
		rel, err := filepath.Rel(filepath.Join(base, "t"), filepath.Clean(path))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.Join(base, "t", strings.Split(rel, string(filepath.Separator))[0]), true
	}
	return "", false
}

func resolved(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

// markerUnit is the resource unit a scratch's marker names.
func markerUnit(scratch string) (string, bool) {
	marker := filepath.Join(scratch, scratchMarker)
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	raw, err := os.ReadFile(marker)
	unit := strings.TrimSpace(string(raw))
	return unit, err == nil && userunit.Token(unit) != ""
}

// leaseSet is every lease under a state root, by unit.
type leaseSet map[string]workerproc.Lease

func readLeases(root string) leaseSet {
	set := leaseSet{}
	owners, _ := os.ReadDir(filepath.Join(root, "run", "worker-resources"))
	for _, owner := range owners {
		dir := filepath.Join(root, "run", "worker-resources", owner.Name())
		files, _ := os.ReadDir(dir)
		for _, file := range files {
			unit, ok := strings.CutSuffix(file.Name(), ".json")
			if !ok || userunit.Token(unit) == "" {
				continue
			}
			if lease, err := workerproc.ReadLease(filepath.Join(dir, file.Name())); err == nil {
				set[unit] = lease
			}
		}
	}
	return set
}

func (s leaseSet) has(unit string) bool { _, ok := s[unit]; return ok }

func (s leaseSet) unitOfToken(token string) string {
	for unit := range s {
		if userunit.Token(unit) == token {
			return unit
		}
	}
	return ""
}

func (s leaseSet) observedBy(id workerproc.ProcessIdentity) string {
	units := make([]string, 0, len(s))
	for unit := range s {
		units = append(units, unit)
	}
	sort.Strings(units)
	for _, unit := range units {
		for _, seen := range s[unit].Observed {
			if seen == id {
				return unit
			}
		}
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
