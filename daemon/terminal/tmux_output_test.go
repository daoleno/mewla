package terminal

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"
)

func TestTmuxOutputIdleEchoAndBoundedTrailingFlush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		results := make(chan tmuxReadResult)
		s := &tmuxSession{events: make(chan Event, 128)}
		go s.forwardOutput(context.Background(), results)
		results <- tmuxReadResult{data: "a"}
		synctest.Wait()
		select {
		case event := <-s.events:
			if event.Data != "a" {
				t.Fatalf("first echo = %q", event.Data)
			}
		default:
			t.Fatal("idle echo waited for a timer")
		}
		results <- tmuxReadResult{data: "b"}
		results <- tmuxReadResult{data: "终端"}
		synctest.Wait()
		select {
		case event := <-s.events:
			t.Fatalf("burst was not coalesced: %q", event.Data)
		default:
		}
		time.Sleep(16 * time.Millisecond)
		synctest.Wait()
		if event := <-s.events; event.Data != "b终端" {
			t.Fatalf("trailing output = %q", event.Data)
		}
		time.Sleep(100 * time.Millisecond)
		results <- tmuxReadResult{data: "c"}
		synctest.Wait()
		select {
		case event := <-s.events:
			if event.Data != "c" {
				t.Fatalf("next idle echo = %q", event.Data)
			}
		default:
			t.Fatal("next idle echo waited")
		}
		results <- tmuxReadResult{data: "tail"}
		close(results)
		synctest.Wait()
		if event := <-s.events; event.Data != "tail" {
			t.Fatalf("EOF lost trailing output: %q", event.Data)
		}
	})
}

func TestTmuxOutputFloodRetainsByteBoundAndOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		results := make(chan tmuxReadResult)
		s := &tmuxSession{events: make(chan Event, 128)}
		go s.forwardOutput(context.Background(), results)
		want := strings.Repeat("终端", 3000)
		results <- tmuxReadResult{data: want}
		close(results)
		synctest.Wait()
		var got strings.Builder
		for len(s.events) > 0 {
			event := <-s.events
			if len(event.Data) > 8192 {
				t.Fatalf("unbounded frame: %d", len(event.Data))
			}
			got.WriteString(event.Data)
		}
		if got.String() != want {
			t.Fatal("flood dropped or reordered bytes")
		}
	})
}

func TestTmuxOutputPreservesUTF8AcrossPtyReads(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	results := make(chan tmuxReadResult, 8)
	session := &tmuxSession{}
	go func() { defer close(results); session.readLoop(context.Background(), reader, results) }()
	want := strings.Repeat("a", 8191) + "终端"
	go func() { defer writer.Close(); _, _ = writer.Write([]byte(want)) }()
	var got strings.Builder
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !utf8.ValidString(result.data) {
			t.Fatal("PTY read published an incomplete UTF-8 rune")
		}
		got.WriteString(result.data)
	}
	if got.String() != want {
		t.Fatal("PTY read lost Unicode bytes")
	}
}
