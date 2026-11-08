package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/codexctl"
)

func collectorWithCodexAccount(read codexAccountReader) *Collector {
	return &Collector{readCodexAccount: read, codexUsageTimeout: time.Second, now: time.Now}
}

func TestCollectCodexSubscriptionNormalizesWhatCodexReports(t *testing.T) {
	reset := time.Unix(1795266347, 0).UTC()
	c := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) {
		return codexctl.AccountUsage{
			Plan:      " plus ",
			Primary:   &codexctl.UsageWindow{UsedPercent: 42, WindowMinutes: 300, ResetsAt: reset},
			Secondary: &codexctl.UsageWindow{UsedPercent: 7, WindowMinutes: 10080},
		}, nil
	})
	got := c.collectCodexSubscription()
	if got == nil || got.AuthKind != "official" || got.State != "available" || got.Plan != "plus" || got.Stale {
		t.Fatalf("subscription = %#v", got)
	}
	if len(got.Windows) != 2 || got.Windows[0].Name != "primary" || got.Windows[0].UsedPercent != 42 ||
		got.Windows[0].WindowMinutes != 300 || got.Windows[0].ResetsAt != reset.Format(time.RFC3339) ||
		got.Windows[1].Name != "secondary" || got.Windows[1].ResetsAt != "" {
		t.Fatalf("windows = %#v", got.Windows)
	}
}

func TestCollectCodexSubscriptionOmitsAnAccountWithoutWindows(t *testing.T) {
	c := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) { return codexctl.AccountUsage{}, nil })
	c.lastCodexSubscription = &CodexSubscriptionUsage{AuthKind: "official", State: "available", FetchedAt: time.Now().Format(time.RFC3339)}
	if got := c.collectCodexSubscription(); got != nil || c.lastCodexSubscription != nil {
		t.Fatalf("subscription = %#v, cached = %#v", got, c.lastCodexSubscription)
	}
}

func TestCollectCodexSubscriptionIsNilWhenCodexCannotAnswerAndNothingIsCached(t *testing.T) {
	c := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) {
		return codexctl.AccountUsage{}, errors.New("executable file not found")
	})
	if got := c.collectCodexSubscription(); got != nil {
		t.Fatalf("subscription = %#v, want nil", got)
	}
}

func TestCollectCodexSubscriptionKeepsRecentGoodUsageOnFailure(t *testing.T) {
	now := time.Now()
	c := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) {
		return codexctl.AccountUsage{}, errors.New("app-server did not start")
	})
	c.now = func() time.Time { return now }
	c.lastCodexSubscription = &CodexSubscriptionUsage{
		AuthKind: "official", State: "available", Plan: "pro", FetchedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		Windows: []CodexUsageWindow{{Name: "primary", UsedPercent: 20}},
	}
	got := c.collectCodexSubscription()
	if got == nil || !got.Stale || got.State != "available" || len(got.Windows) != 1 {
		t.Fatalf("stale usage = %#v", got)
	}
	got.Windows[0].UsedPercent = 99
	if c.lastCodexSubscription.Windows[0].UsedPercent != 20 {
		t.Fatal("stale copy aliases the cache")
	}

	c.now = func() time.Time { return now.Add(2 * time.Hour) }
	if got := c.collectCodexSubscription(); got != nil {
		t.Fatalf("usage older than %v still shown: %#v", codexStaleLimit, got)
	}
}

func TestRefreshAsksCodexForItsQuotaOnlyEveryFewMinutes(t *testing.T) {
	now := time.Now()
	asked := 0
	c := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) {
		asked++
		return codexctl.AccountUsage{Plan: "pro", Primary: &codexctl.UsageWindow{UsedPercent: float64(asked), WindowMinutes: 300}}, nil
	})
	c.now = func() time.Time { return now }

	first := c.codexSubscriptionForRefresh()
	if first == nil || asked != 1 {
		t.Fatalf("first refresh: %#v after %d questions", first, asked)
	}
	now = now.Add(codexSubscriptionEvery - time.Minute)
	again := c.codexSubscriptionForRefresh()
	if asked != 1 || again == nil || again.Windows[0].UsedPercent != 1 {
		t.Fatalf("a refresh within %v asked again: %#v after %d questions", codexSubscriptionEvery, again, asked)
	}
	again.Windows[0].UsedPercent = 99
	if c.codexSubscriptionForRefresh().Windows[0].UsedPercent != 1 {
		t.Fatal("what a refresh returns aliases the remembered answer")
	}
	now = now.Add(2 * time.Minute)
	if got := c.codexSubscriptionForRefresh(); asked != 2 || got.Windows[0].UsedPercent != 2 {
		t.Fatalf("a refresh after %v did not ask: %#v after %d questions", codexSubscriptionEvery, got, asked)
	}

	// An answer of "nothing to show" is remembered too.
	nobody := collectorWithCodexAccount(func(context.Context) (codexctl.AccountUsage, error) {
		asked++
		return codexctl.AccountUsage{}, errors.New("codex is not installed")
	})
	asked = 0
	nobody.now = func() time.Time { return now }
	nobody.codexSubscriptionForRefresh()
	nobody.codexSubscriptionForRefresh()
	if asked != 1 {
		t.Fatalf("an agent that cannot answer was asked %d times", asked)
	}
}

func TestCodexQuotaAnswerSurvivesARestart(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	asked := 0
	read := func(context.Context) (codexctl.AccountUsage, error) {
		asked++
		return codexctl.AccountUsage{Plan: "pro", Primary: &codexctl.UsageWindow{UsedPercent: 12, WindowMinutes: 300}}, nil
	}
	c := collectorWithCodexAccount(read)
	c.usageCacheDir, c.now = dir, func() time.Time { return now }
	c.codexSubscriptionForRefresh()

	restarted := collectorWithCodexAccount(read)
	restarted.usageCacheDir, restarted.now = dir, func() time.Time { return now.Add(10 * time.Minute) }
	if got := restarted.codexSubscriptionForRefresh(); asked != 1 || got == nil || got.Windows[0].UsedPercent != 12 {
		t.Fatalf("a restart asked Codex again: %#v after %d questions", got, asked)
	}

	late := collectorWithCodexAccount(read)
	late.usageCacheDir, late.now = dir, func() time.Time { return now.Add(codexSubscriptionEvery + time.Minute) }
	late.codexSubscriptionForRefresh()
	if asked != 2 {
		t.Fatalf("an answer older than %v was reused: %d questions", codexSubscriptionEvery, asked)
	}
}
