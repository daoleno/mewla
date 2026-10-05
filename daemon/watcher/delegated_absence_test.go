package watcher

import (
	"errors"
	"testing"
)

func TestResolveDelegatedAbsenceUsesAuthoritativeInventory(t *testing.T) {
	for _, tc := range []struct {
		name, script  string
		gone, unknown bool
	}{
		{"unreachable", `echo 'failed to connect to server' >&2; exit 1`, false, true},
		{"absent", `if [ "$1" = list-panes ]; then echo '%7'; exit 0; fi; exit 1`, true, false},
		{"owned", `if [ "$1" = list-panes ]; then echo '%1'; else echo 1; fi`, false, false},
		{"foreign", `if [ "$1" = list-panes ]; then echo '%1'; else echo 0; fi`, true, false},
		{"ownership unavailable", `if [ "$1" = list-panes ]; then echo '%1'; exit 0; fi; echo 'no server running on fixture' >&2; exit 1`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", writeFakeTmux(t, tc.script))
			w := New(0)
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
