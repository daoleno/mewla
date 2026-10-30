package agentuse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/phone"
)

func TestPhoneUseIsOffUntilTheyTurnItOnAndOfflineIsStillOn(t *testing.T) {
	dir := t.TempDir()
	service, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if service.PhoneEnabled() || service.PhoneReady() {
		t.Fatal("Phone use is on without anyone turning it on")
	}
	if status := service.Status(); status.Phone != nil {
		t.Fatalf("a phone row without a phone manager: %+v", status.Phone)
	}

	state := phone.Status{}
	service.SetPhone(func() phone.Status { return state })
	if row := service.Status().Phone; row == nil || row.Enabled || row.State != phone.StateOffline || row.Connected {
		t.Fatalf("phone row %+v", row)
	}

	// The mewla command and the daemon share the switch through the file.
	other, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.SetPhoneEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !service.PhoneReady() {
		t.Fatal("the daemon does not see the switch")
	}
	// A phone that is not connected does not make Phone use not ready: Workers get the tools
	// and the tool says the phone is offline.
	if !service.PhoneReady() || service.Status().Phone.Connected {
		t.Fatalf("%+v", service.Status().Phone)
	}

	state = phone.Status{Connected: true, State: phone.StateReady, Model: "Pixel 8", SDK: 35, W: 1080, H: 2400, Active: &phone.Active{Worker: "%7", Name: "Writer"}}
	status := service.Status()
	raw, _ := json.Marshal(status.Phone)
	for _, want := range []string{`"enabled":true`, `"connected":true`, `"state":"ready"`, `"model":"Pixel 8"`, `"sdk":35`, `"w":1080`, `"h":2400`, `"worker":"%7"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("phone row %s lacks %s", raw, want)
		}
	}

	// It is independent of the switch for this computer, and survives it.
	if _, err := service.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if !service.PhoneReady() {
		t.Fatal("turning the computer off turned Phone use off")
	}
	if _, err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	stored, _ := os.ReadFile(filepath.Join(dir, "agent-use", "settings.json"))
	if !strings.Contains(string(stored), `"phone": true`) {
		t.Fatalf("settings %s", stored)
	}

	if _, err := service.SetPhoneEnabled(false); err != nil {
		t.Fatal(err)
	}
	if service.PhoneReady() || service.Status().Phone.Enabled {
		t.Fatal("Phone use stayed on")
	}
}
