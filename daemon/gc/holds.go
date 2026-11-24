package gc

import (
	"sort"
	"time"
)

// Holds is what the collector holds and did, for the Resources page: per
// kind its size, oldest item and growth over 7 days; the Sessions holding the
// most; what is kept; and what was collected today, restorable while it is
// in the trash.
type Holds struct {
	Day    string      `json:"day,omitempty"` // the size sample's day; "" before the first
	Kinds  []HoldKind  `json:"kinds"`
	Owners []HoldOwner `json:"owners"`
	Kept   []HeldKeep  `json:"kept"`
	Today  HoldsToday  `json:"collected_today"`
}

// HoldKind is one kind on the sample day.
type HoldKind struct {
	Kind   string    `json:"kind"`
	Count  int       `json:"count"`
	Bytes  int64     `json:"bytes"`
	Oldest time.Time `json:"oldest,omitempty"`
	// Growth7d is bytes now minus bytes 7 days before, when that day was
	// sampled.
	Growth7d *int64 `json:"growth_7d,omitempty"`
}

// HoldOwner is a Session among those holding the most bytes.
type HoldOwner struct {
	Unit      string `json:"unit"`
	SessionID string `json:"session_id,omitempty"`
	Name      string `json:"name,omitempty"`
	WorkID    string `json:"work_id,omitempty"`
	WorkTitle string `json:"work_title,omitempty"`
	Bytes     int64  `json:"bytes"`
}

// HeldKeep is one keep: what, whose, until when and why.
type HeldKeep struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	SessionID string    `json:"session_id,omitempty"`
	Until     time.Time `json:"until"`
	Why       string    `json:"why"`
}

// HoldsToday is what was collected since local midnight.
type HoldsToday struct {
	Items int   `json:"items"`
	Bytes int64 `json:"bytes"`
	// Restorable is what of it is still in the trash.
	Restorable []HoldsCollected `json:"restorable"`
}

// HoldsCollected is one item collected today and still in the trash.
type HoldsCollected struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Original string    `json:"original"`
	Bytes    int64     `json:"bytes"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

// HoldsOwners is how many Sessions Holds lists.
const HoldsOwners = 5

// SummarizeHolds reads Holds from the ledger and the trash and keep records.
// lookup names a unit's Session and Work (its Session and Work may be nil).
func SummarizeHolds(l WhyLookup, now time.Time) (Holds, error) {
	root := l.Root
	h := Holds{Kinds: []HoldKind{}, Owners: []HoldOwner{}, Kept: []HeldKeep{}, Today: HoldsToday{Restorable: []HoldsCollected{}}}
	week := now.Local().AddDate(0, 0, -7).Format("2006-01-02")
	samples, err := Sizes(root, week)
	if err != nil {
		return h, err
	}
	if len(samples) > 0 {
		h.Day = samples[len(samples)-1].Day
	}
	before := map[string]int64{}
	for _, s := range samples {
		switch s.Day {
		case week:
			before[s.Kind] = s.Bytes
		case h.Day:
			h.Kinds = append(h.Kinds, HoldKind{Kind: s.Kind, Count: s.Count, Bytes: s.Bytes, Oldest: s.Oldest})
		}
	}
	for i := range h.Kinds {
		if old, ok := before[h.Kinds[i].Kind]; ok && h.Day != week {
			growth := h.Kinds[i].Bytes - old
			h.Kinds[i].Growth7d = &growth
		}
	}
	sort.SliceStable(h.Kinds, func(a, b int) bool { return h.Kinds[a].Bytes > h.Kinds[b].Bytes })
	if h.Day != "" {
		owners, err := OwnerSizes(root, h.Day, HoldsOwners)
		if err != nil {
			return h, err
		}
		for _, o := range owners {
			owner := HoldOwner{Unit: o.Unit, Bytes: o.Bytes}
			owner.SessionID, owner.Name, owner.WorkID, owner.WorkTitle = l.nameUnit(o.Unit)
			h.Owners = append(h.Owners, owner)
		}
	}
	keeps, err := ListKeeps(root)
	if err != nil {
		return h, err
	}
	for _, k := range keeps {
		session, _, _, _ := l.nameUnit(k.Owner)
		h.Kept = append(h.Kept, HeldKeep{ID: k.ID, Path: k.Path, SessionID: session, Until: k.Until, Why: k.Why})
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	events, err := Events(root, midnight)
	if err != nil {
		return h, err
	}
	trashed := map[string]Event{}
	for _, e := range events {
		if e.Action == ActionTrashed || e.Action == ActionEvicted {
			h.Today.Items++
			h.Today.Bytes += e.Bytes
		}
		if e.Action == ActionTrashed {
			trashed[e.Subject] = e
		}
	}
	items, err := List(root)
	if err != nil {
		return h, err
	}
	for _, item := range items {
		e, ok := trashed[item.Original]
		if !ok || !item.Moved || item.TrashedAt.Before(midnight.UTC()) {
			continue
		}
		h.Today.Restorable = append(h.Today.Restorable, HoldsCollected{ID: item.ID, Kind: item.Kind, Original: item.Original,
			Bytes: e.Bytes, Reason: item.Reason, At: item.TrashedAt})
	}
	sort.SliceStable(h.Today.Restorable, func(a, b int) bool { return h.Today.Restorable[a].At.After(h.Today.Restorable[b].At) })
	return h, nil
}

// nameUnit is the Session (live, else from the ledger) and Work of a unit.
func (l WhyLookup) nameUnit(unit string) (sessionID, name, workID, workTitle string) {
	if l.Session != nil {
		if s, ok := l.Session(unit); ok {
			sessionID, name = s.ID, s.Name
		}
	}
	if sessionID == "" {
		if s, ok, _ := SessionOfUnit(l.Root, unit); ok {
			sessionID = s.SessionID
		}
	}
	if sessionID != "" && l.Work != nil {
		if w, ok := l.Work(sessionID); ok {
			workID, workTitle = w.ID, w.Name
		}
	}
	return sessionID, name, workID, workTitle
}
