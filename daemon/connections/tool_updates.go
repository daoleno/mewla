package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"
)

// A remote service can change its tools at any time. Mewla adopts the new
// list right away, so unchanged tools keep working, and holds back only what
// the person has not seen: a new tool, or a new definition of a tool they had
// allowed. Grants stay bound to the definition the person allowed (name,
// description and schema: the description is what the model follows), so an
// updated tool cannot run until they confirm it. A changed tool they had
// blocked stays blocked and asks for nothing, and a tool that changes back to
// the definition they allowed needs nothing either.
//
// The list is read again on every call, before an MCP account is shown or
// described (at most once a minute), and whenever updates are asked for.

// toolsFresh is how long a listed MCP tool list stands in for the service's
// own when an account is shown or described.
const toolsFresh = time.Minute

// syncTools adopts a service's current tool list and returns the names whose
// definitions are new or changed.
func syncTools(r *record, current []Tool) []string {
	before := map[string]Tool{}
	for _, t := range r.Account.Tools {
		before[t.Name] = t
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	if r.Updates == nil {
		r.Updates = map[string]string{}
	}
	present := map[string]bool{}
	var updated []string
	for _, t := range current {
		present[t.Name] = true
		want := fingerprint(t)
		old, existed := before[t.Name]
		if existed && fingerprint(old) == want {
			continue
		}
		if r.Grants[t.Name] == want {
			// Back to the definition the person allowed.
			delete(r.Updates, t.Name)
			continue
		}
		updated = append(updated, t.Name)
		_, waiting := r.Updates[t.Name]
		if !existed || waiting || r.Grants[t.Name] == fingerprint(old) {
			r.Updates[t.Name] = want
		}
	}
	for name := range r.Grants {
		if !present[name] {
			delete(r.Grants, name)
		}
	}
	for name := range r.Updates {
		if !present[name] {
			delete(r.Updates, name)
		}
	}
	if len(r.Updates) == 0 {
		r.Updates = nil
	}
	r.Account.Tools = current
	return updated
}

// adoptTools takes a tool list the service just sent.
func (m *Manager) adoptTools(r *record, current []Tool) {
	if updated := syncTools(r, current); len(updated) > 0 {
		log.Printf("plugins: %s updated %d tools; waiting for confirmation: %d", r.Account.Integration, len(updated), len(r.Updates))
	}
	m.toolsSeen[r.Account.ID] = time.Now()
}

// syncRemoteTools reads an MCP account's tool list and adopts it. Like a
// call, it lists the tools outside the manager lock.
//
// With force (updates, or a call to a tool the list lacks), an expired OAuth
// token is refreshed first under the lock, exactly as a call's is, so two
// refreshes never race. Without force (showing or describing an account), a
// list read in the last toolsFresh is kept, and an account whose token needs
// refreshing is left for its next call: nothing waits on the network while
// holding the lock. Listing never changes the account's status.
func (m *Manager) syncRemoteTools(parent context.Context, id string, force bool, wait time.Duration) error {
	m.mu.Lock()
	r, ok := m.records[id]
	if !ok || m.isPending(id) || !usesMCP(r) || !r.Account.Enabled || r.Account.Status == "disconnected" || (!force && time.Since(m.toolsSeen[id]) < toolsFresh) {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	var secret string
	var err error
	if force {
		secret, err = m.accountToken(ctx, r)
	} else if secret, ok = m.storedToken(r); !ok {
		m.mu.Unlock()
		return nil
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	tools, err := m.discoverMCP(ctx, &snapshot, secret)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r = m.records[id]; r == nil || r.Account.Status == "disconnected" {
		return nil
	}
	m.adoptTools(r, tools)
	return m.save()
}

// storedToken is the account's credential when using it needs no network: an
// API key, or an OAuth access token that has not expired.
func (m *Manager) storedToken(r *record) (string, bool) {
	raw, ok, err := m.vault.Get("integration:" + r.Account.ID)
	if err != nil {
		return "", false
	}
	if r.OAuth == nil {
		return raw, true
	}
	var secret oauthSecret
	if !ok || json.Unmarshal([]byte(raw), &secret) != nil || secret.Token == nil || !secret.Token.Valid() {
		return "", false
	}
	return secret.Token.AccessToken, true
}

// reviewUpdates confirms or dismisses updated tools the person saw, all of
// them or none. Confirming allows the definition Mewla recorded when the tool
// changed; dismissing leaves the tool blocked.
func reviewUpdates(r *record, names []string, confirm bool) error {
	if len(names) == 0 {
		return errors.New("name the updated tools to review")
	}
	current := map[string]Tool{}
	for _, t := range r.Account.Tools {
		current[t.Name] = t
	}
	for _, name := range names {
		want, ok := r.Updates[name]
		if !ok {
			return fmt.Errorf("no update waits for confirmation: %s", name)
		}
		if fingerprint(current[name]) != want {
			return fmt.Errorf("%s changed again; review the account again", name)
		}
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	for _, name := range names {
		if confirm {
			r.Grants[name] = r.Updates[name]
		}
		delete(r.Updates, name)
	}
	if len(r.Updates) == 0 {
		r.Updates = nil
	}
	return nil
}

// clearUpdates drops tools the person just decided about another way.
func clearUpdates(r *record, names ...string) {
	for _, name := range names {
		delete(r.Updates, name)
	}
	if len(r.Updates) == 0 {
		r.Updates = nil
	}
}

func updatedNames(r *record) []string {
	if len(r.Updates) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.Updates))
	for name := range r.Updates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
