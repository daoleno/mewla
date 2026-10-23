package screen

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func displayHeld(f *FakeBackend) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.DisplayHeld
}

func waitHeld(t *testing.T, f *FakeBackend, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for displayHeld(f) != want {
		if time.Now().After(deadline) {
			t.Fatalf("display held = %v, want %v", displayHeld(f), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func viewerService(t *testing.T) (*Service, *FakeBackend) {
	t.Helper()
	fake := NewFakeBackend(64, 48)
	service := NewServiceWith(t.TempDir(), fake)
	service.lockPoll = 10 * time.Millisecond
	t.Cleanup(func() {
		service.StopAgent()
		waitHeld(t, fake, false)
	})
	if err := service.Enable(context.Background(), "phone", "Pixel"); err != nil {
		t.Fatal(err)
	}
	return service, fake
}

func openHeld(t *testing.T, service *Service, ctx context.Context, obs *Observer) net.Conn {
	t.Helper()
	conn, err := service.Open(ctx, "phone", obs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// A connected viewer holds the display on, and disconnecting, the request
// ending (a timed-out socket cancels its context) or both let it go.
func TestAViewerHoldsTheDisplayUntilItEnds(t *testing.T) {
	service, fake := viewerService(t)
	if displayHeld(fake) {
		t.Fatal("the display was held with no viewer")
	}

	conn := openHeld(t, service, context.Background(), nil)
	if !displayHeld(fake) {
		t.Fatal("an open viewer didn't hold the display before its first frame")
	}
	conn.Close()
	waitHeld(t, fake, false)

	ctx, cancel := context.WithCancel(context.Background())
	openHeld(t, service, ctx, nil)
	waitHeld(t, fake, true)
	cancel()
	waitHeld(t, fake, false)
}

func TestTheDisplayStaysHeldUntilTheLastViewerEnds(t *testing.T) {
	service, fake := viewerService(t)
	first := openHeld(t, service, context.Background(), nil)
	second := openHeld(t, service, context.Background(), nil)
	first.Close()
	time.Sleep(60 * time.Millisecond)
	if !displayHeld(fake) {
		t.Fatal("one viewer ending let go of the other's hold")
	}
	second.Close()
	waitHeld(t, fake, false)
}

// An agent and a viewer each hold the display; neither ending drops the
// other's, in either order. Opening a viewer cancels the agent, which lets go
// a moment later.
func TestAnAgentAndAViewerHoldTheDisplayTogether(t *testing.T) {
	service, fake := viewerService(t)
	if _, err := service.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	conn := openHeld(t, service, context.Background(), nil)
	service.StopAgent()
	waitFor(t, func() bool { _, ok := service.Agent(); return !ok })
	time.Sleep(60 * time.Millisecond)
	if !displayHeld(fake) {
		t.Fatal("the agent ending let go of the viewer's hold")
	}
	if _, err := service.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	time.Sleep(60 * time.Millisecond)
	if !displayHeld(fake) {
		t.Fatal("the viewer ending let go of the agent's hold")
	}
	service.StopAgent()
	waitHeld(t, fake, false)
}

// Watching the agent adds no hold of its own.
func TestWatchingTheAgentDoesNotHoldTheDisplay(t *testing.T) {
	service, fake := viewerService(t)
	if _, err := service.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	conn, err := service.Watch(context.Background(), "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	time.Sleep(60 * time.Millisecond)
	service.StopAgent()
	waitHeld(t, fake, false)
}

type lockLog struct {
	mu     sync.Mutex
	events []bool
}

func (l *lockLog) observe(locked bool) {
	l.mu.Lock()
	l.events = append(l.events, locked)
	l.mu.Unlock()
}

func (l *lockLog) get() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bool(nil), l.events...)
}

// The display is never held to show a login window, and a lock someone makes
// while viewing ends the hold at once; unlocking takes it again.
func TestALockEndsTheHoldAndAnUnlockResumesIt(t *testing.T) {
	service, fake := viewerService(t)
	fake.SetLocked(true)
	var log lockLog
	openHeld(t, service, context.Background(), &Observer{Locked: log.observe})
	if displayHeld(fake) {
		t.Fatal("a viewer on a locked computer held the display")
	}
	fake.SetLocked(false)
	waitHeld(t, fake, true)
	fake.SetLocked(true)
	waitHeld(t, fake, false)
	fake.SetLocked(false)
	waitHeld(t, fake, true)
	waitFor(t, func() bool { return len(log.get()) == 4 })
	want := []bool{true, false, true, false}
	for i, locked := range log.get() {
		if locked != want[i] {
			t.Fatalf("lock events %v, want %v", log.get(), want)
		}
	}
}

func TestStatusReportsALockedComputer(t *testing.T) {
	service, fake := viewerService(t)
	if service.Status(context.Background(), "phone").Locked {
		t.Fatal("an unlocked computer reported as locked")
	}
	fake.SetLocked(true)
	if !service.Status(context.Background(), "phone").Locked {
		t.Fatal("a locked computer not reported")
	}
}

func netPipe() (net.Conn, net.Conn) { return net.Pipe() }
