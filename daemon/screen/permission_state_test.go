package screen

import "testing"

// Each state the Mac app shows comes from what macOS answers this process (own), a fresh process (live), whether
// Mewla has a row in the list at all, and whether capture was refused to this process.
func TestPermissionStateReadsGrantedOffMissingAndRestart(t *testing.T) {
	for _, c := range []struct {
		name                     string
		own, live, listed, stale bool
		want                     string
	}{
		{"never asked, or the row removed with −", false, false, false, false, PermissionMissing},
		{"a row of another signature (ignored by macOS)", false, false, false, false, PermissionMissing},
		{"in the list, switched off", false, false, true, false, PermissionOff},
		{"denied when macOS asked", false, false, true, false, PermissionOff},
		{"allowed and counting", true, true, true, false, PermissionGranted},
		{"allowed, this process still answered from its start", false, true, true, false, PermissionRestart},
		{"allowed, capture refused to this process", true, true, true, true, PermissionRestart},
	} {
		if got := permissionState(c.own, c.live, c.listed, c.stale); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}
