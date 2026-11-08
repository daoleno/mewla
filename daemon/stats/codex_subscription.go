package stats

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/providerpaths"
)

// codexStaleLimit is how long a failed refresh may keep showing the last
// good quota (marked stale) before the card says unavailable.
const codexStaleLimit = time.Hour

// codexAccountReader reads the signed-in Codex account's quota. The default
// asks Codex itself, through its app-server; tests substitute it.
type codexAccountReader func(context.Context) (codexctl.AccountUsage, error)

func readCodexAccountWithAppServer(ctx context.Context) (codexctl.AccountUsage, error) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return codexctl.AccountUsage{}, err
	}
	return codexctl.ReadAccountUsage(ctx, binary, nil)
}

// codexSubscriptionEvery is how often a refresh asks Codex for its quota while
// Codex is being used, and codexSubscriptionIdleEvery how often while it is
// not. Codex answers by starting its app server, which reads some 100 MB to do
// so (the daemon is charged for what its children read), and a quota moves with
// use: when Codex's state database has not changed since the last answer there
// is little new to learn.
const (
	codexSubscriptionEvery     = 15 * time.Minute
	codexSubscriptionIdleEvery = time.Hour
)

// codexSubscriptionFresh reports whether the last answer still stands.
func (c *Collector) codexSubscriptionFresh(now time.Time) bool {
	if c.codexSubscriptionAt.IsZero() {
		return false
	}
	age := now.Sub(c.codexSubscriptionAt)
	if age < 0 || age >= codexSubscriptionIdleEvery {
		return false
	}
	if age < codexSubscriptionEvery {
		return true
	}
	state := c.codexStateNow()
	return state != "" && state == c.codexSubscriptionState
}

func (c *Collector) codexStateNow() string {
	if c.codexState != nil {
		return c.codexState()
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	return codexStateStamp(providerpaths.CodexStateDB(home))
}

// codexSubscriptionForRefresh is collectCodexSubscription, asked only when the
// last answer no longer stands: a refresh in between shows what it was.
func (c *Collector) codexSubscriptionForRefresh() *CodexSubscriptionUsage {
	now := c.now()
	if c.codexSubscriptionAt.IsZero() {
		// A restart or hot reload carries on from the last answer.
		c.loadCodexSubscription()
	}
	if c.codexSubscriptionFresh(now) {
		if last := c.codexSubscriptionLast; last != nil {
			clone := *last
			clone.Windows = append([]CodexUsageWindow(nil), last.Windows...)
			return &clone
		}
		return nil
	}
	c.codexSubscriptionState = c.codexStateNow()
	c.codexSubscriptionLast = c.collectCodexSubscription()
	c.codexSubscriptionAt = now
	c.saveCodexSubscription()
	if c.codexSubscriptionLast == nil {
		return nil
	}
	clone := *c.codexSubscriptionLast
	clone.Windows = append([]CodexUsageWindow(nil), c.codexSubscriptionLast.Windows...)
	return &clone
}

// collectCodexSubscription returns the official ChatGPT-backed Codex quota, or
// nil when Codex is not installed or has no subscription quota to report (not
// signed in, or signed in with an API key). Mewla never reads Codex's credential file or calls
// OpenAI endpoints with Codex's token: Codex answers for itself.
func (c *Collector) collectCodexSubscription() *CodexSubscriptionUsage {
	ctx, cancel := context.WithTimeout(context.Background(), c.codexUsageTimeout)
	defer cancel()
	account, err := c.readCodexAccount(ctx)
	if err != nil {
		// Codex is missing, not signed in with a subscription, or briefly
		// unreachable. Only a recent good reading is worth showing.
		return c.staleCodexSubscription()
	}
	if account.Primary == nil && account.Secondary == nil {
		c.lastCodexSubscription = nil
		return nil
	}
	usage := &CodexSubscriptionUsage{
		AuthKind:  "official",
		State:     "available",
		Plan:      strings.TrimSpace(account.Plan),
		FetchedAt: c.now().UTC().Format(time.RFC3339),
	}
	for _, candidate := range []struct {
		name   string
		window *codexctl.UsageWindow
	}{{"primary", account.Primary}, {"secondary", account.Secondary}} {
		if candidate.window == nil {
			continue
		}
		window := CodexUsageWindow{
			Name:          candidate.name,
			UsedPercent:   candidate.window.UsedPercent,
			WindowMinutes: candidate.window.WindowMinutes,
		}
		if !candidate.window.ResetsAt.IsZero() {
			window.ResetsAt = candidate.window.ResetsAt.UTC().Format(time.RFC3339)
		}
		usage.Windows = append(usage.Windows, window)
	}
	c.lastCodexSubscription = usage
	return usage
}

// staleCodexSubscription is the last good quota marked stale, while a failed
// refresh is still recent enough to be useful; nil when there is none.
func (c *Collector) staleCodexSubscription() *CodexSubscriptionUsage {
	last := c.lastCodexSubscription
	if last == nil {
		return nil
	}
	fetched, err := time.Parse(time.RFC3339, last.FetchedAt)
	if err != nil || c.now().Sub(fetched) > codexStaleLimit {
		return nil
	}
	stale := *last
	stale.Windows = append([]CodexUsageWindow(nil), last.Windows...)
	stale.Stale = true
	return &stale
}

const codexSubscriptionFile = "codex-subscription.json"

type codexSubscriptionRecord struct {
	At    time.Time
	State string
	Usage *CodexSubscriptionUsage
}

// loadCodexSubscription takes the stored answer when it can still stand; an
// older, missing or damaged one is as if Codex had never been asked.
func (c *Collector) loadCodexSubscription() {
	if c.usageCacheDir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(c.usageCacheDir, codexSubscriptionFile))
	if err != nil {
		return
	}
	var record codexSubscriptionRecord
	if json.Unmarshal(raw, &record) != nil || record.At.IsZero() {
		return
	}
	if age := c.now().Sub(record.At); age < 0 || age >= codexSubscriptionIdleEvery {
		return
	}
	c.codexSubscriptionAt, c.codexSubscriptionState, c.codexSubscriptionLast = record.At, record.State, record.Usage
}

func (c *Collector) saveCodexSubscription() {
	if c.usageCacheDir == "" {
		return
	}
	raw, err := json.Marshal(codexSubscriptionRecord{At: c.codexSubscriptionAt, State: c.codexSubscriptionState, Usage: c.codexSubscriptionLast})
	if err == nil {
		_ = writeUsageCache(filepath.Join(c.usageCacheDir, codexSubscriptionFile), raw)
	}
}
