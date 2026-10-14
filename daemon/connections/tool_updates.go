package connections

import (
	"errors"
	"fmt"
	"sort"
)

// A remote service can change its tools at any time. Mewla adopts the new
// list right away, so unchanged tools keep working, and holds back only what
// the person has not seen: a new tool, or a new definition of a tool they had
// allowed. Grants stay bound to the definition the person allowed, so an
// updated tool cannot run until they confirm it. A changed tool they had
// blocked stays blocked and asks for nothing.

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
