package connections

import (
	"sort"
	"strings"
)

// An account keeps its ID for as long as it is the same account at the
// service: Triggers, notes and per-account policy name it. Signing in again
// renews the card in place, and a card left behind by an earlier version (a
// disconnected twin of the same identity) is folded into the live one, its ID
// kept as an alias so whatever stored it still reaches the account.

// identityKnown: the service named who signed in, so two cards with the same
// identity are the same account.
func identityKnown(identity string) bool {
	return identity != "" && !strings.Contains(identity, "identity not provided") && identity != "Account identity unavailable"
}

// twinsOf reports whether a and b are one account at one service.
func twinsOf(a, b *record) bool {
	if a == b || a.Account.Integration != b.Account.Integration || a.CLI != nil || b.CLI != nil {
		return false
	}
	if e := lookup(a.Account.Integration); e == nil || e.Custom {
		return false
	}
	return identityKnown(a.Account.Identity) && a.Account.Identity == b.Account.Identity
}

// foldTwins folds every finished-disconnected twin of live into it: the twin
// is removed and its IDs answer for live from now on. A twin whose saved
// sign-in is still being removed waits, so that removal can finish. Reports
// whether anything was folded.
func (m *Manager) foldTwins(live *record) bool {
	if live == nil || live.Account.Status == "disconnected" {
		return false
	}
	ids := make([]string, 0, len(m.records))
	for id := range m.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	folded := false
	for _, id := range ids {
		twin := m.records[id]
		if twin.Account.Status != "disconnected" || twin.Account.CredentialRemovalPending || !twinsOf(live, twin) || m.isPending(id) {
			continue
		}
		live.Aliases = appendNew(live.Aliases, append([]string{id}, twin.Aliases...)...)
		delete(m.records, id)
		folded = true
	}
	return folded
}

// foldAllTwins runs foldTwins for every live account, once at start.
func (m *Manager) foldAllTwins() bool {
	ids := make([]string, 0, len(m.records))
	for id := range m.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	folded := false
	for _, id := range ids {
		if r := m.records[id]; r != nil && m.foldTwins(r) {
			folded = true
		}
	}
	return folded
}

// canonicalID is the account an ID names: itself, or the account it was
// folded into.
func (m *Manager) canonicalID(id string) string {
	if id == "" {
		return id
	}
	if _, ok := m.records[id]; ok {
		return id
	}
	for live, r := range m.records {
		for _, alias := range r.Aliases {
			if alias == id {
				return live
			}
		}
	}
	return id
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if v != "" && !contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}
