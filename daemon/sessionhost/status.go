package sessionhost

import (
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// maxStatusRecords bounds the OSC 7501 records one Session keeps; the
// specification asks for at least 64 and at most 256.
const maxStatusRecords = 256

// statusRecords keeps a program's OSC 7501 records by the specification's
// rules: a report replaces its record, a clear removes the record and every
// record beneath it (an empty id removes all), and the record updated longest
// ago goes when there are too many.
type statusRecords struct {
	byID map[string]ProgramStatus
}

func newStatusRecords() *statusRecords {
	return &statusRecords{byID: make(map[string]ProgramStatus)}
}

// apply records one report and returns the record as stored, or nil for a
// clear.
func (r *statusRecords) apply(report ghosttyvt.Status, at time.Time) *ProgramStatus {
	if report.State == ghosttyvt.StatusClear {
		if report.ID == "" {
			clear(r.byID)
			return nil
		}
		for id := range r.byID {
			if id == report.ID || strings.HasPrefix(id, report.ID+"/") {
				delete(r.byID, id)
			}
		}
		return nil
	}
	record := ProgramStatus{
		ID:        report.ID,
		State:     report.State.String(),
		Kind:      report.Kind.String(),
		Progress:  report.Progress,
		App:       report.App,
		Title:     report.Title,
		Message:   report.Message,
		UpdatedAt: at,
	}
	r.byID[report.ID] = record
	for len(r.byID) > maxStatusRecords {
		oldest := ""
		var oldestAt time.Time
		for id, existing := range r.byID {
			if oldest == "" || existing.UpdatedAt.Before(oldestAt) {
				oldest, oldestAt = id, existing.UpdatedAt
			}
		}
		delete(r.byID, oldest)
	}
	return &record
}

// programExited drops the records that only mean something while the program
// runs.
func (r *statusRecords) programExited() {
	for id, record := range r.byID {
		if record.State == ghosttyvt.StatusWorking.String() || record.State == ghosttyvt.StatusBlocked.String() {
			delete(r.byID, id)
		}
	}
}

// list returns the records with an app inherited from the nearest ancestor
// that has one, ordered by id.
func (r *statusRecords) list() []ProgramStatus {
	if len(r.byID) == 0 {
		return nil
	}
	out := make([]ProgramStatus, 0, len(r.byID))
	for _, record := range r.byID {
		if record.App == "" {
			record.App = r.inheritedApp(record.ID)
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *statusRecords) inheritedApp(id string) string {
	for id != "" {
		cut := strings.LastIndex(id, "/")
		if cut < 0 {
			id = ""
		} else {
			id = id[:cut]
		}
		if parent, ok := r.byID[id]; ok && parent.App != "" {
			return parent.App
		}
	}
	return ""
}
