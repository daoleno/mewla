package watcher

import (
	"errors"
	"testing"
)

func TestResolveDelegatedAbsenceUsesAuthoritativeInventory(t *testing.T) {
	for _, tc := range []struct {
		name          string
		state         fakeSessionState
		gone, unknown bool
	}{
		{"unreachable", fakeUnreachable, false, true},
		{"absent", fakeAbsent, true, false},
		{"owned", fakeOwned, false, false},
		{"foreign", fakeUnowned, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := New(0)
			useFakeSessionBackend(w, newFakeSessionBackend()).set("%1", tc.state)
			gone, err := w.ResolveDelegatedAbsence("%1")
			if tc.unknown {
				if !errors.Is(err, ErrOwnershipProbeUnavailable) {
					t.Fatalf("gone=%v err=%v", gone, err)
				}
				return
			}
			if err != nil || gone != tc.gone {
				t.Fatalf("gone=%v err=%v want=%v", gone, err, tc.gone)
			}
		})
	}
}
