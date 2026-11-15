package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

// A computer whose clock is hours off (a VM or dual-boot PC whose hardware
// clock holds local time) has every signed request refused. That is the
// clock, not a sign-out: `mewla login` said "no longer signed in" on a
// computer that had never signed in.
func TestRefusedRequestWithAFarClockSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().Add(-8*time.Hour).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(server.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.StartLogin(context.Background(), "", "route")
	var skew *ClockSkewError
	if !errors.As(err, &skew) || errors.Is(err, ErrSignedOut) {
		t.Fatalf("StartLogin = %v, want a clock error and not a sign-out", err)
	}
	if !strings.Contains(err.Error(), "8h0m0s ahead of") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestClockSkewIgnoresANearClock(t *testing.T) {
	now := time.Now()
	if got := clockSkew(now.Add(-time.Minute).UTC().Format(http.TimeFormat), now); got != 0 {
		t.Fatalf("one minute = %v, want 0", got)
	}
	if got := clockSkew("", now); got != 0 {
		t.Fatalf("no Date = %v, want 0", got)
	}
}
