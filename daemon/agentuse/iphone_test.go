package agentuse

import (
	"strings"
	"testing"
)

func TestIPhoneUseIsOffUntilTurnedOnAndStopsWhenTurnedOff(t *testing.T) {
	dir := t.TempDir()
	service, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if service.Status().IPhone != nil || service.IPhoneReady() {
		t.Fatal("an iPhone row without iPhone use here")
	}
	state := IPhoneStatus{State: IPhoneReady, Summary: "ready"}
	stops, dones := 0, 0
	service.SetIPhone(IPhoneHooks{
		Status:      func() IPhoneStatus { return state },
		Stop:        func() { stops++ },
		HandoffDone: func() bool { dones++; return true },
	})
	if row := service.Status().IPhone; row == nil || row.Enabled || row.State != IPhoneOff || !strings.Contains(row.Summary, "Labs") {
		t.Fatalf("off row %+v", row)
	}
	if service.IPhoneReady() {
		t.Fatal("ready while off")
	}
	// The mewla command and the daemon share the switch through the file.
	other, _ := New(dir, false)
	if _, err := other.SetIPhoneEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !service.IPhoneReady() || !service.Status().IPhone.Enabled || service.Status().IPhone.State != IPhoneReady {
		t.Fatalf("on: %+v", service.Status().IPhone)
	}
	// A computer that can't drive an iPhone gives no tools.
	state = IPhoneStatus{State: IPhoneUnsupported, Summary: "not a Mac"}
	if service.IPhoneReady() || service.Status().IPhone.State != IPhoneUnsupported {
		t.Fatalf("unsupported: %+v", service.Status().IPhone)
	}
	if !service.IPhoneHandoffDone() || dones != 1 {
		t.Fatal("handoff done not passed on")
	}
	if _, err := service.SetIPhoneEnabled(false); err != nil {
		t.Fatal(err)
	}
	if stops != 1 {
		t.Fatalf("turning it off stopped the agent %d times", stops)
	}
	// Phone use and iPhone use are separate switches.
	if service.PhoneEnabled() {
		t.Fatal("the iPhone switch turned Phone use on")
	}
}
