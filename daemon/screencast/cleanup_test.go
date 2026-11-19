package screencast

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

type nopEncoder struct{}

func (nopEncoder) Encode(Picture, bool) ([]byte, error) { return []byte{0, 0, 0, 1, 0x65}, nil }
func (nopEncoder) SetBitrate(int)                       {}
func (nopEncoder) Name() string                         { return "nop" }
func (nopEncoder) Close()                               {}

// settles waits until the goroutines are back to about base.
func settles(t *testing.T, base int, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > base+2 {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%s: %d goroutines, %d before\n%s", what, runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A session that fails to start, or ends before the viewer answered, leaves
// nothing running: no peer, no ICE agent, no reader.
func TestSessionLeavesNothingRunning(t *testing.T) {
	cfg := Config{
		Source:       &fakeSource{w: 64, h: 64, ch: make(chan struct{}, 1)},
		NewEncoder:   func(int) (Encoder, error) { return nopEncoder{}, nil },
		Signal:       func(any) {},
		LoopbackOnly: true,
	}
	base := runtime.NumGoroutine()
	failing := cfg
	failing.SignOffer = func([]string) (any, error) { return nil, errors.New("no key") }
	if _, err := Start(context.Background(), failing); err == nil {
		t.Fatal("started without a signed offer")
	}
	settles(t, base, "after a failed start")

	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	settles(t, base, "after closing before an answer")
}

// A kind that fails (VA-API at a size it does not take) gives way to the next
// for the rest of the view.
func TestEncoderFallsBackToTheNextKind(t *testing.T) {
	bin, kinds, err := findEncoders()
	if err != nil {
		t.Skip(err)
	}
	broken := &ffmpegKind{name: "broken", args: func(int) []string { return []string{"-c:v", "no_such_encoder"} }}
	enc := &ffmpegEncoder{bin: bin, kind: broken, next: kinds, kbps: 2000}
	defer enc.Close()
	au, err := enc.Encode(Picture{Pix: testPicture(320, 240, 1), Width: 320, Height: 240, Stride: 320 * 4}, false)
	if err != nil || len(au) == 0 {
		t.Fatalf("after the fallback: %d bytes, %v", len(au), err)
	}
	if enc.Name() != kinds[0].name {
		t.Fatalf("encoder %q, want %q", enc.Name(), kinds[0].name)
	}
	if types := nalTypes(au); len(types) == 0 || types[0] != 7 {
		t.Fatalf("the first picture after the fallback is no key frame: %v", types)
	}
}

// A computer without ffmpeg is not believed for long: one installed later is
// found without a restart.
func TestAMissingFFmpegIsLookedForAgain(t *testing.T) {
	if _, _, err := findEncoders(); err != nil {
		t.Skip(err)
	}
	probe.mu.Lock()
	bin, kinds := probe.bin, probe.kinds
	probe.bin, probe.kinds, probe.err = "", nil, nil
	probe.mu.Unlock()
	defer func() {
		probe.mu.Lock()
		probe.bin, probe.kinds, probe.err = bin, kinds, nil
		probe.mu.Unlock()
	}()
	path := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if _, _, err := findEncoders(); !errors.Is(err, ErrNoEncoder) {
		t.Fatalf("without ffmpeg: %v", err)
	}
	os.Setenv("PATH", path)
	if _, _, err := findEncoders(); err == nil {
		t.Fatal("looked again at once")
	}
	probe.mu.Lock()
	probe.failed = time.Now().Add(-probeAgain)
	probe.mu.Unlock()
	if _, _, err := findEncoders(); err != nil {
		t.Fatalf("ffmpeg installed meanwhile was not found: %v", err)
	}
}

type brokenEncoder struct{ nopEncoder }

func (brokenEncoder) Encode(Picture, bool) ([]byte, error) { return nil, errors.New("frame too large") }

// An encoder that fails ends the session with ErrEncoder, which the viewer is
// told, not a lost connection.
func TestAFailedEncoderEndsTheSessionSayingSo(t *testing.T) {
	s, err := Start(context.Background(), Config{
		Source:       &fakeSource{w: 64, h: 64, ch: make(chan struct{}, 1)},
		NewEncoder:   func(int) (Encoder, error) { return brokenEncoder{}, nil },
		Signal:       func(any) {},
		LoopbackOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session went on")
	}
	if !errors.Is(s.Err(), ErrEncoder) {
		t.Fatalf("ended with %v", s.Err())
	}
}
