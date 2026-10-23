package connections

import (
	"errors"
	"fmt"
)

// toolGroup is the class of a tool: "read", "write", or "" for a tool Mewla
// has not reviewed. Remote readOnlyHint annotations never grant anything.
// Grants still fingerprint the discovered full definition, and invocation
// rechecks it. Unknown/new tools require individual review. An official MCP
// list applies only on its exact endpoint.
func toolGroup(r *record, t Tool) string {
	e := lookup(r.Account.Integration)
	if e == nil || e.Custom {
		return ""
	}
	if e.official(r) {
		if e.MCP == nil || r.Account.Endpoint != e.MCP.Endpoint {
			return ""
		}
		return e.MCP.group(t.Name)
	}
	for _, candidate := range e.reviewed() {
		if fingerprint(candidate) == fingerprint(t) {
			if candidate.Read {
				return "read"
			}
			return "write"
		}
	}
	return ""
}
func (m *Manager) setGroup(r *record, group string, allowed bool) bool {
	if e := lookup(r.Account.Integration); (group != "read" && group != "write") || e != nil && e.Custom {
		return false
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	for _, t := range r.Account.Tools {
		if toolGroup(r, t) == group {
			if allowed {
				r.Grants[t.Name] = fingerprint(t)
			} else {
				delete(r.Grants, t.Name)
			}
			clearUpdates(r, t.Name)
		}
	}
	return true
}

// setPolicy allows or blocks one tool, or every tool in a batch. A batch names
// the tools the user saw, so a tool discovered later is never granted by it,
// and it applies all of them or none.
func setPolicy(r *record, q Request) error {
	names := q.Tools
	if q.Tool != "" {
		if len(names) != 0 {
			return errors.New("name one tool or a list of tools, not both")
		}
		names = []string{q.Tool}
	}
	if len(names) == 0 {
		return errors.New("tool not found")
	}
	tools := map[string]Tool{}
	for _, t := range r.Account.Tools {
		tools[t.Name] = t
	}
	for _, name := range names {
		if _, ok := tools[name]; !ok {
			return fmt.Errorf("tool not found: %s", name)
		}
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	for _, name := range names {
		if q.Allowed {
			r.Grants[name] = fingerprint(tools[name])
		} else {
			delete(r.Grants, name)
		}
		clearUpdates(r, name)
	}
	return nil
}

func groupState(tools []Tool, group string) string {
	total, allowed := 0, 0
	for _, t := range tools {
		if t.Group == group {
			total++
			if t.Allowed {
				allowed++
			}
		}
	}
	switch {
	case total == 0:
		return "none"
	case allowed == total:
		return "allowed"
	case allowed == 0:
		return "off"
	}
	return "partial"
}

// writeConsent reports a sign-in whose grant lacks the service's write scope,
// so allowing changes means connecting again with writes. Tokens and imported
// logins carry whatever the person granted them.
func writeConsent(r *record) bool {
	if r.OAuth == nil {
		return false
	}
	for _, scope := range lookup(r.Account.Integration).changeScopes() {
		if !contains(r.Account.Scopes, scope) {
			return true
		}
	}
	return false
}
