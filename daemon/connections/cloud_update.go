package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const cloudUpdateInterval = 24 * time.Hour
const cloudLongGap = 7 * 24 * time.Hour

type cloudUpdateState struct {
	Attempted time.Time `json:"attempted"`
	Succeeded time.Time `json:"succeeded"`
}
type cloudUpdates struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	active map[string]chan struct{}
	used   map[string]bool
	// Memory also throttles attempts when the state directory cannot be written.
	attempted map[string]time.Time
}

func (c *cloudRuntime) enableUpdates() {
	ctx, cancel := context.WithCancel(context.Background())
	u := &cloudUpdates{ctx: ctx, cancel: cancel, active: map[string]chan struct{}{}, used: map[string]bool{}, attempted: map[string]time.Time{}}
	c.updates = u
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		timer := time.NewTicker(time.Hour)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				u.mu.Lock()
				ids := []string{}
				for id := range u.used {
					ids = append(ids, id)
				}
				u.mu.Unlock()
				for _, id := range ids {
					c.refresh(id, false)
				}
			}
		}
	}()
}
func (c *cloudRuntime) updateState(id string) cloudUpdateState {
	var state cloudUpdateState
	data, _ := os.ReadFile(filepath.Join(c.dir, id, "update.json"))
	_ = json.Unmarshal(data, &state)
	return state
}

// refresh coalesces readers, persists a daily attempt budget across restarts,
// and owns cancellation independently of any one UI request.
func (c *cloudRuntime) refresh(id string, force bool) <-chan struct{} {
	u := c.updates
	if u == nil || c.providers[id] == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ctx.Err() != nil {
		return nil
	}
	u.used[id] = true
	if done := u.active[id]; done != nil {
		return done
	}
	state := c.updateState(id)
	last := state.Attempted
	if u.attempted[id].After(last) {
		last = u.attempted[id]
	}
	if !force && !last.IsZero() && time.Since(last) < cloudUpdateInterval {
		return nil
	}
	u.attempted[id] = time.Now()
	done := make(chan struct{})
	u.active[id] = done
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		defer func() { u.mu.Lock(); delete(u.active, id); close(done); u.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(u.ctx, 3*time.Minute)
		defer cancel()
		dir := filepath.Join(c.dir, id)
		unlock, err := lockCloudUpdate(dir)
		if err != nil {
			return
		}
		defer unlock()
		state = c.updateState(id)
		if !force && !state.Attempted.IsZero() && time.Since(state.Attempted) < cloudUpdateInterval {
			return
		}
		state.Attempted = time.Now()
		save := func() {
			data, _ := json.Marshal(state)
			_ = atomicfile.Write(filepath.Join(dir, "update.json"), data, 0600)
		}
		save() // A crash or failed download must not create a request/restart loop.
		if c.providers[id].install(ctx, dir) == nil {
			state.Succeeded = time.Now()
			save()
		}
	}()
	return done
}

func (c *cloudRuntime) prepare(ctx context.Context, id string) error {
	if c.providers[id] == nil {
		return errors.New("Cloud CLI is unavailable")
	}
	state := c.updateState(id)
	_, managedErr := os.Stat(filepath.Join(c.dir, id, "bin", c.providers[id].binary()))
	wait := managedErr != nil || state.Succeeded.IsZero() || time.Since(state.Succeeded) >= cloudLongGap
	done := c.refresh(id, false)
	if wait && done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// A failed update is never a blocking error while a usable CLI exists.
	_, err := c.resolveExecutable(id)
	return err
}

// CLIInfo is projected from the executable actually selected on this server.
type CLIInfo struct {
	Version string `json:"version"`
	Managed bool   `json:"managed"`
}

func (c *cloudRuntime) info(id string) *CLIInfo {
	if c.providers[id] == nil {
		return nil
	}
	binary, err := c.executable(id)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := cloudExecutableVersion(ctx, id, binary, runCloudProcess, c.providers[id].environment())
	if err != nil {
		return nil
	}
	root, _ := filepath.Abs(filepath.Join(c.dir, id))
	resolved, _ := filepath.Abs(binary)
	return &CLIInfo{Version: version, Managed: strings.HasPrefix(resolved, root+string(filepath.Separator))}
}
