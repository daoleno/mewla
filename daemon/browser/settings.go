package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/google/uuid"
)

// Settings are the computer-wide Browser choices, kept beside the profiles.
type Settings struct {
	// VirtualScreen: new agent Workers get a browser on the virtual screen
	// without being told which one (Settings → Labs → Virtual screen).
	VirtualScreen bool `json:"virtual_screen,omitempty"`
}

func loadSettings(root string) Settings {
	var s Settings
	if raw, err := os.ReadFile(filepath.Join(root, "settings.json")); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// VirtualScreen reports whether new Workers get a virtual-screen browser.
func (m *Manager) VirtualScreen() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.VirtualScreen
}

func (m *Manager) setVirtualScreen(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrUnavailable
	}
	next := m.settings
	next.VirtualScreen = on
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(m.root, "settings.json"), raw, 0o600); err != nil {
		return err
	}
	m.settings = next
	return nil
}

// reservationLifetime keeps a browser handed to a launching Worker from being
// handed to another before the first agent has asked for it.
const reservationLifetime = 2 * time.Minute

// maxBrowsers is the most browser profiles one server keeps.
const maxBrowsers = 8

// AutoAttachment picks the browser a new Worker gets when the virtual screen
// is on: one no agent holds, else a new one (up to eight; after that the
// first is shared and agents take turns). It starts nothing; the browser comes
// up when the agent first asks for control. "" means none (off, or this
// computer cannot run one).
func (m *Manager) AutoAttachment(ctx context.Context) string {
	if m == nil || !m.VirtualScreen() || !m.backend.Capability(ctx).Available {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ""
	}
	entries := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].resource.CreatedAt.Before(entries[j].resource.CreatedAt) })
	for _, e := range entries {
		e.mu.Lock()
		free := e.resource.AllowAgents && e.owner.Kind != "agent" && time.Since(e.reservedAt) > reservationLifetime
		if free {
			e.reservedAt = time.Now()
		}
		e.mu.Unlock()
		if free {
			return e.resource.ID
		}
	}
	if len(entries) >= maxBrowsers {
		return entries[0].resource.ID
	}
	name := "Browser"
	if len(entries) > 0 {
		name = fmt.Sprintf("Browser %d", len(entries)+1)
	}
	e := newEntry(Resource{ID: uuid.NewString(), Name: name, CreatedAt: time.Now().UTC(), State: "stopped", Control: "idle", AllowAgents: true})
	e.reservedAt = time.Now()
	if err := m.save(e); err != nil {
		return ""
	}
	m.entries[e.resource.ID] = e
	return e.resource.ID
}
