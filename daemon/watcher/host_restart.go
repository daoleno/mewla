package watcher

import (
	"errors"
	"log"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// SessionHostState is the running session host: its version and the Sessions
// whose program still runs.
type SessionHostState struct {
	Version string
	PID     int
	Alive   []string
}

// SessionHostState reads the running host. ErrSessionHostGone means none
// runs; it never starts one.
func (w *Watcher) SessionHostState() (SessionHostState, error) {
	host := w.host()
	if host == nil {
		return SessionHostState{}, errNoSessionHost
	}
	return host.state()
}

// RestartSessionHost ends the session host, and with it every Session in it,
// then starts a new host from this daemon's executable. allowed sees the live
// Sessions right before the host is asked to stop; when it says no, nothing
// happens and restarted is false.
func (w *Watcher) RestartSessionHost(allowed func(SessionHostState) bool) (restarted bool, err error) {
	host := w.host()
	if host == nil {
		return false, errNoSessionHost
	}
	return host.restart(allowed)
}

func (b *hostBackend) state() (SessionHostState, error) {
	client, err := b.conn(false)
	if err != nil {
		return SessionHostState{}, err
	}
	infos, err := b.list()
	if err != nil {
		return SessionHostState{}, err
	}
	welcome := client.Welcome()
	state := SessionHostState{Version: welcome.HostVersion, PID: welcome.PID}
	for _, info := range infos {
		// A probe Session (ready) lives for a moment and holds nothing.
		if info.Alive && b.validID(info.ID) {
			state.Alive = append(state.Alive, info.ID)
		}
	}
	return state, nil
}

func (b *hostBackend) restart(allowed func(SessionHostState) bool) (bool, error) {
	before, err := b.state()
	if err != nil {
		return false, err
	}
	if allowed != nil && !allowed(before) {
		return false, nil
	}
	client, err := b.conn(false)
	if err != nil {
		return false, err
	}
	ctx, cancel := hostCtx()
	err = client.Shutdown(ctx, true)
	cancel()
	if err != nil {
		return false, err
	}
	b.mu.Lock()
	if b.client == client {
		b.client.Close()
		b.client = nil
	}
	b.mu.Unlock()
	// The host answers before it stops, and goes on ending its Sessions for a
	// moment after it stops listening: wait until its process has exited, so
	// the new host is neither the old one answering nor refused by its lock.
	deadline := time.Now().Add(30 * time.Second)
	for before.PID > 0 && platform.ProcessAlive(before.PID) {
		if time.Now().After(deadline) {
			return true, errors.New("the old session host did not stop")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := b.conn(true); err != nil {
		return true, err
	}
	after, _ := b.state()
	log.Printf("watcher: restarted the session host: %s (pid %d) -> %s (pid %d)", before.Version, before.PID, after.Version, after.PID)
	return true, nil
}
