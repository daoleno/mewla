package screen

import (
	"context"
	"testing"
	"time"
)

func TestWaitQuietReturnsFalseWithoutEvents(t *testing.T) {
	events := make(chan struct{})
	start := time.Now()
	saw, err := waitQuiet(context.Background(), events, 20*time.Millisecond, 30*time.Millisecond, time.Second)
	if err != nil || saw || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("saw %v err %v after %v", saw, err, time.Since(start))
	}
}

func TestWaitQuietWaitsForTheBurstToEnd(t *testing.T) {
	events := make(chan struct{}, 16)
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(15 * time.Millisecond)
			events <- struct{}{}
		}
	}()
	start := time.Now()
	saw, err := waitQuiet(context.Background(), events, 40*time.Millisecond, 50*time.Millisecond, time.Second)
	// Five events 15 ms apart end at about 75 ms; quiet then needs 50 more.
	if err != nil || !saw || time.Since(start) < 110*time.Millisecond || time.Since(start) > 400*time.Millisecond {
		t.Fatalf("saw %v err %v after %v", saw, err, time.Since(start))
	}
}

func TestWaitQuietGivesUpAtMaxAndOnCancel(t *testing.T) {
	events := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case events <- struct{}{}:
				time.Sleep(5 * time.Millisecond)
			case <-stop:
				return
			}
		}
	}()
	start := time.Now()
	saw, _ := waitQuiet(context.Background(), events, 20*time.Millisecond, 30*time.Millisecond, 120*time.Millisecond)
	if !saw || time.Since(start) > 400*time.Millisecond {
		t.Fatalf("never-quiet app: saw %v after %v", saw, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitQuiet(ctx, make(chan struct{}), time.Second, time.Second, time.Second); err == nil {
		t.Fatal("cancel must end the wait")
	}
}
