package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type connectionSnapshots struct {
	value   atomic.Pointer[Response]
	mu      sync.Mutex
	active  map[string]bool
	checked map[string]time.Time
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	slots   chan struct{}
	changed func(Response)
}

func newConnectionSnapshots() *connectionSnapshots {
	ctx, cancel := context.WithCancel(context.Background())
	return &connectionSnapshots{active: map[string]bool{}, checked: map[string]time.Time{}, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 4)}
}

// SetChanged binds server-local background refreshes to the existing websocket.
// It must be called during server setup, before handling client requests.
func (m *Manager) SetChanged(changed func(Response)) {
	m.snapshots.mu.Lock()
	m.snapshots.changed = changed
	m.snapshots.mu.Unlock()
}

// listLocked does no process or network work. The stored value is immutable.
func (m *Manager) listLocked() Response {
	accounts := make([]Account, 0, len(m.records))
	for _, r := range m.records {
		if m.isPending(r.Account.ID) {
			continue
		}
		a := m.projection(r)
		a.Tools = nil
		accounts = append(accounts, a)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	return Response{Catalog: m.catalog(), Accounts: accounts, OAuthConfigured: m.oauthConfigured()}
}
func (m *Manager) cacheListLocked() {
	if m.snapshots == nil {
		return
	}
	response := m.listLocked()
	m.snapshots.value.Store(&response)
}
func (m *Manager) cachedList() Response {
	cached := m.snapshots.value.Load()
	if cached == nil {
		return Response{Catalog: []Integration{}, Accounts: []Account{}}
	}
	response := *cached
	response.Accounts = append([]Account{}, cached.Accounts...)
	m.snapshots.mu.Lock()
	for i := range response.Accounts {
		a := &response.Accounts[i]
		a.Checking = m.snapshots.active[a.Integration]
	}
	m.snapshots.mu.Unlock()
	return response
}
func (m *Manager) pushConnections() {
	m.snapshots.mu.Lock()
	notify := m.snapshots.changed
	m.snapshots.mu.Unlock()
	if notify != nil {
		notify(m.cachedList())
	}
}
func cloneRecord(r *record) *record {
	data, _ := json.Marshal(r)
	var out record
	_ = json.Unmarshal(data, &out)
	return &out
}

func sameRecord(a, b *record) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

// Discovery and identity checks run on a detached snapshot. They never hold the
// manager's account lock while starting a CLI or contacting a cloud service.
func (m *Manager) refreshCloudProfiles() {
	if m.cloud == nil {
		return
	}
	s := m.snapshots
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	for id, provider := range m.cloud.providers {
		if s.active[id] || time.Since(s.checked[id]) < time.Minute {
			continue
		}
		s.active[id] = true
		s.wg.Add(1)
		go func(id string, provider cloudProvider) {
			defer s.wg.Done()
			ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
			defer cancel()
			m.mu.Lock()
			baseline := map[string]*record{}
			records := map[string]*record{}
			for key, r := range m.records {
				if r.Account.Integration == id {
					baseline[key] = cloneRecord(r)
					records[key] = cloneRecord(r)
				}
			}
			checked := map[string]time.Time{}
			for key, at := range m.cloud.checked {
				checked[key] = at
			}
			originalRun := m.cloud.run
			cloud := newCloudRuntime(m.cloud.dir)
			cloud.profileFiles = m.cloud.profileFiles
			cloud.providers = m.cloud.providers
			cloud.updates = m.cloud.updates
			cloud.checked = checked
			cloud.run = func(ctx context.Context, binary string, args, env []string) (cloudOutput, error) {
				select {
				case s.slots <- struct{}{}:
				case <-ctx.Done():
					return cloudOutput{}, ctx.Err()
				}
				defer func() { <-s.slots }()
				return originalRun(ctx, binary, args, env)
			}
			snapshot := &Manager{records: records, cloud: cloud, transient: true}
			m.mu.Unlock()
			merge := func() {
				m.mu.Lock()
				defer m.mu.Unlock()
				for key, before := range baseline {
					current := m.records[key]
					if !sameRecord(current, before) {
						continue
					} // A delete, rename or permission change wins.
					if next := snapshot.records[key]; next != nil {
						m.records[key] = cloneRecord(next)
					} else {
						delete(m.records, key)
					}
				}
				for key, next := range snapshot.records {
					if baseline[key] == nil && m.records[key] == nil {
						m.records[key] = cloneRecord(next)
					}
				}
				// Subsequent discovery/status merges compare with what this refresh last published.
				for key := range baseline {
					delete(baseline, key)
				}
				for key, r := range snapshot.records {
					baseline[key] = cloneRecord(r)
				}
				m.cacheListLocked()
			}
			snapshot.cloudDiscovered = func() { merge(); m.pushConnections() }
			_ = provider.syncProfiles(snapshot, ctx, false)
			info := cloud.info(id)
			merge()
			m.mu.Lock()
			for key, at := range checked {
				if at.After(m.cloud.checked[key]) {
					m.cloud.checked[key] = at
				}
			}
			if info != nil {
				m.cloudInfo[id] = info
			}
			_ = m.save()
			m.cacheListLocked()
			m.mu.Unlock()
			s.mu.Lock()
			s.active[id] = false
			s.checked[id] = time.Now()
			s.mu.Unlock()
			m.pushConnections()
		}(id, provider)
	}
}
