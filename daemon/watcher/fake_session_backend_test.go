package watcher

import (
	"errors"
	"fmt"
	"sync"

	"github.com/daoleno/mewla/daemon/classifier"
)

// fakeSessionState is what a fakeSessionBackend says about one Session.
type fakeSessionState string

const (
	fakeAbsent      fakeSessionState = ""
	fakeOwned       fakeSessionState = "owned"
	fakeUnowned     fakeSessionState = "unowned"     // present, not Mewla's
	fakeUnreachable fakeSessionState = "unreachable" // the backend cannot be read
)

// fakeSessionBackend is a sessionBackend for tests of what the watcher does
// with what a backend reports: present or absent, owned or not, readable or
// not. It runs nothing; kills and keys are recorded.
type fakeSessionBackend struct {
	mu       sync.Mutex
	states   map[string]fakeSessionState
	panes    []sessionPane
	listErr  error
	killed   []string
	keys     []string
	literals []string
	// created records every create; nextID is the id the next one gets
	// ("%1" when empty) and createErr makes it fail.
	created   []backendCreateSpec
	nextID    string
	createErr error
	// replaced records every replace, by id.
	replaced []string
	agentEnd map[string]string
}

var _ sessionBackend = (*fakeSessionBackend)(nil)

func newFakeSessionBackend() *fakeSessionBackend {
	return &fakeSessionBackend{states: map[string]fakeSessionState{}}
}

// useFakeSessionBackend makes b the watcher's backend.
func useFakeSessionBackend(w *Watcher, b *fakeSessionBackend) *fakeSessionBackend {
	w.mu.Lock()
	w.backend = b
	w.listPanes = b.listOwned
	w.mu.Unlock()
	return b
}

func (b *fakeSessionBackend) set(id string, state fakeSessionState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[id] = state
}

func (b *fakeSessionBackend) state(id string) fakeSessionState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.states[id]
}

func (b *fakeSessionBackend) kills() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.killed...)
}

var errFakeUnreachable = errors.New("fake backend cannot be read")

func (b *fakeSessionBackend) validID(id string) bool           { return id != "" }
func (b *fakeSessionBackend) observedGeneration(string) string { return "" }
func (b *fakeSessionBackend) terminal(string) TerminalTarget   { return TerminalTarget{} }

func (b *fakeSessionBackend) listOwned() ([]sessionPane, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listErr != nil {
		return nil, b.listErr
	}
	return append([]sessionPane(nil), b.panes...), nil
}

func (b *fakeSessionBackend) listServices() ([]servicePane, error) { return nil, nil }

func (b *fakeSessionBackend) probeOwnership(id string) (bool, bool, error) {
	switch b.state(id) {
	case fakeOwned:
		return true, true, nil
	case fakeUnowned:
		return true, false, nil
	case fakeUnreachable:
		return false, false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, errFakeUnreachable)
	}
	return false, false, nil
}

func (b *fakeSessionBackend) resolveAbsence(id string) (bool, error) {
	switch b.state(id) {
	case fakeOwned:
		return false, nil
	case fakeUnowned:
		return true, nil
	case fakeUnreachable:
		return false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, errFakeUnreachable)
	}
	return true, nil
}

func (b *fakeSessionBackend) processRoot(id string) (int, error) {
	if b.state(id) != fakeOwned {
		return 0, fmt.Errorf("no Session %s", id)
	}
	return 1, nil
}

func (b *fakeSessionBackend) currentPath(id string) (string, error) {
	if b.state(id) != fakeOwned {
		return "", fmt.Errorf("no Session %s", id)
	}
	return "/", nil
}

func (b *fakeSessionBackend) delegatedResource(string) (bool, string) { return false, "" }

func (b *fakeSessionBackend) captureState(id string) (string, bool, int) {
	return "", b.state(id) == fakeOwned, -1
}

func (b *fakeSessionBackend) captureRecent(string) (string, error) { return "", nil }

func (b *fakeSessionBackend) captureVisible(string, bool) (string, error) { return "", nil }

func (b *fakeSessionBackend) captureDialog(string) (string, classifier.CaptureInfo, error) {
	return "", classifier.CaptureInfo{}, nil
}

func (b *fakeSessionBackend) sendKeys(id string, keys ...string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keys = append(b.keys, keys...)
	return nil
}

func (b *fakeSessionBackend) repeatKey(id, key string, count int) error {
	for range count {
		_ = b.sendKeys(id, key)
	}
	return nil
}

func (b *fakeSessionBackend) sendLiteral(id, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.literals = append(b.literals, text)
	return nil
}

func (b *fakeSessionBackend) sendBytes(id string, data []byte) error {
	return b.sendLiteral(id, string(data))
}

func (b *fakeSessionBackend) checkNear(string) error { return nil }

func (b *fakeSessionBackend) create(spec backendCreateSpec) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.created = append(b.created, spec)
	if b.createErr != nil {
		return "", b.createErr
	}
	id := b.nextID
	if id == "" {
		id = "%1"
	}
	b.states[id] = fakeOwned
	return id, nil
}

func (b *fakeSessionBackend) kill(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.killed = append(b.killed, id)
	b.states[id] = fakeAbsent
	return nil
}

func (b *fakeSessionBackend) inputIO() sessionInputIO { return (&hostBackend{}).inputIO() }

func (b *fakeSessionBackend) replace(id string, spec backendCreateSpec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.states[id] == fakeAbsent {
		return errSessionGone
	}
	b.created = append(b.created, spec)
	b.replaced = append(b.replaced, id)
	b.states[id] = fakeOwned
	return nil
}

func (b *fakeSessionBackend) agentEndData(id string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agentEnd[id], nil
}

func (b *fakeSessionBackend) setAgentEndData(id, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agentEnd == nil {
		b.agentEnd = map[string]string{}
	}
	b.agentEnd[id] = value
	return nil
}
