package opencodeapi

import (
	"context"
	"errors"
	"testing"
)

func TestParseVersionAndMajor(t *testing.T) {
	for _, tc := range []struct {
		out, version string
		major        int
	}{
		{"opencode v2.0.32\n", "2.0.32", 2},
		{"1.18.4\n", "1.18.4", 1},
		{"", "", 0},
	} {
		version := ParseVersion([]byte(tc.out))
		if version != tc.version || MajorVersion(version) != tc.major {
			t.Fatalf("%q: version=%q major=%d", tc.out, version, MajorVersion(version))
		}
	}
}

func TestParseDebugPaths(t *testing.T) {
	paths := ParseDebugPaths([]byte("data    /home/u/.local/share/opencode\nstate   /home/u/.local/state/opencode\ndb      /home/u/.local/share/opencode/opencode v2.db\n"))
	if paths.Data != "/home/u/.local/share/opencode" || paths.DB != "/home/u/.local/share/opencode/opencode v2.db" {
		t.Fatalf("paths = %+v", paths)
	}
}

func TestDefaultRunnerNeverReachesTheRealServiceInTests(t *testing.T) {
	if _, err := Runner(context.Background(), "--version"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestShortMessagePageIsLastDespiteNextCursor(t *testing.T) {
	previous := Runner
	t.Cleanup(func() { Runner = previous })
	calls := 0
	Runner = func(context.Context, ...string) ([]byte, error) {
		calls++
		return []byte(`{"data":[{"id":"msg_1","type":"user"}],"cursor":{"next":"abc"}}`), nil
	}
	messages, err := ListMessages(context.Background(), "ses_1", "")
	if err != nil || len(messages) != 1 || calls != 1 {
		t.Fatalf("messages=%d calls=%d err=%v", len(messages), calls, err)
	}
}

func TestSessionNotFoundIsTyped(t *testing.T) {
	previous := Runner
	t.Cleanup(func() { Runner = previous })
	Runner = func(context.Context, ...string) ([]byte, error) {
		return nil, TypedError([]byte(`{"_tag":"SessionNotFoundError","message":"gone"}`), errors.New("404"))
	}
	if _, err := GetSession(context.Background(), "ses_x"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v", err)
	}
}
