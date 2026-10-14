package screen

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRestartOnlyForAllowedDevicesWhenSupervised(t *testing.T) {
	ctx := context.Background()
	service := NewServiceWith(t.TempDir(), NewFakeBackend(64, 48))
	if err := service.Enable(ctx, "phone", "Pixel"); err != nil {
		t.Fatal(err)
	}
	if service.Status(ctx, "phone").CanRestart {
		t.Fatal("an unsupervised daemon offered restart")
	}
	if err := service.Restart("phone"); !errors.Is(err, ErrNoRestart) {
		t.Fatalf("unsupervised restart = %v", err)
	}
	restarted := make(chan struct{})
	service.SetRestart(func() { close(restarted) })
	if !service.Status(ctx, "phone").CanRestart {
		t.Fatal("a supervised daemon did not offer restart")
	}
	if err := service.Restart("stranger"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("an unapproved device restarted Mewla: %v", err)
	}
	if err := service.Restart("phone"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("restart never ran")
	}
}

func TestApprovalIsPerDeviceAndAskedAtTheComputer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fake := NewFakeBackend(64, 48)
	service := NewServiceWith(dir, fake)

	status := service.Status(ctx, "phone")
	if !status.Supported || status.Approved || status.Ready || status.KeepAwake {
		t.Fatalf("fresh status = %+v", status)
	}
	if _, err := service.Open(ctx, "phone"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("open before approval = %v", err)
	}
	if err := service.SetKeepAwake("phone", false); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("an unapproved device changed keep-awake: %v", err)
	}

	fake.Answer = false
	if err := service.Enable(ctx, "phone", "Pixel"); !errors.Is(err, ErrDenied) {
		t.Fatalf("denied enable = %v", err)
	}
	fake.Answer = true
	if err := service.Enable(ctx, "phone", "Pixel"); err != nil {
		t.Fatal(err)
	}
	if err := service.Enable(ctx, "phone", "Pixel"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Confirmed) != 2 {
		t.Fatalf("an approved device was asked again: %v", fake.Confirmed)
	}
	if fake.Requested != 2 {
		t.Fatalf("permissions requested %d times, want once per enable", fake.Requested)
	}
	status = service.Status(ctx, "phone")
	if !status.Approved || !status.Ready || !status.KeepAwake {
		t.Fatalf("approved status = %+v", status)
	}
	if service.Status(ctx, "laptop").Approved {
		t.Fatal("approval leaked to another device")
	}

	if err := service.SetKeepAwake("phone", false); err != nil {
		t.Fatal(err)
	}
	if service.Status(ctx, "phone").KeepAwake {
		t.Fatal("keep-awake stayed on")
	}
	if err := service.OpenSettings(ctx, "phone", "accessibility"); err != nil || fake.Opened[0] != "accessibility" {
		t.Fatalf("open settings: %v %v", err, fake.Opened)
	}

	// The local kill switch forgets every approval.
	if err := service.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := service.Enable(ctx, "phone", "Pixel"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("enable while disabled = %v", err)
	}
	if err := service.EnableLocally(false); err != nil {
		t.Fatal(err)
	}
	if service.Status(ctx, "phone").Approved {
		t.Fatal("approval survived disable")
	}
}

func TestUnsupportedAndMissingPermissions(t *testing.T) {
	ctx := context.Background()
	fake := NewFakeBackend(64, 48)
	fake.Unsupported = true
	service := NewServiceWith(t.TempDir(), fake)
	if status := service.Status(ctx, "phone"); status.Supported || status.Reason == "" || status.Power != nil {
		t.Fatalf("unsupported status = %+v", status)
	}
	if err := service.Enable(ctx, "phone", "Pixel"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("enable unsupported = %v", err)
	}

	fake = NewFakeBackend(64, 48)
	fake.Missing = []string{"accessibility"}
	service = NewServiceWith(t.TempDir(), fake)
	if err := service.Enable(ctx, "phone", "Pixel"); err != nil {
		t.Fatal(err)
	}
	status := service.Status(ctx, "phone")
	if !status.Approved || status.Ready {
		t.Fatalf("missing-permission status = %+v", status)
	}
	if _, err := service.Open(ctx, "phone"); !errors.Is(err, ErrPermissions) {
		t.Fatalf("open without permission = %v", err)
	}
}

func TestUnsupportedBackendIsTheDefaultElsewhere(t *testing.T) {
	b := unsupportedBackend{reason: ErrUnsupported.Error()}
	if c := b.Capability(context.Background()); c.Supported || c.Ready() {
		t.Fatalf("capability = %+v", c)
	}
}
