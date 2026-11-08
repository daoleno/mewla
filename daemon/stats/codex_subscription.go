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

// codexSubscriptionEvery is how often a refresh asks Codex for its quota. Codex
// answers by starting its app server, which reads some 100 MB to do so (the
// daemon is charged for what its children read), and a quota moves slowly
// enough for a reading a few minutes old.
const codexSubscriptionEvery = 15 * time.Minute

// codexSubscriptionForRefresh is collectCodexSubscription, asked at most every
// codexSubscriptionEvery: a refresh in between shows what the last answer was.
func (c *Collector) codexSubscriptionForRefresh() *CodexSubscriptionUsage {
	now := c.now()
	if c.codexSubscriptionAt.IsZero() {
		// A restart or hot reload carries on from the last answer.
		c.loadCodexSubscription()
	}
	if !c.codexSubscriptionAt.IsZero() && now.Sub(c.codexSubscriptionAt) >= 0 && now.Sub(c.codexSubscriptionAt) < codexSubscriptionEvery {
		if last := c.codexSubscriptionLast; last != nil {
			clone := *last
			clone.Windows = append([]CodexUsageWindow(nil), last.Windows...)
			return &clone
		}
		return nil
	}
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
	Usage *CodexSubscriptionUsage
}

// loadCodexSubscription takes the stored answer when it is still within
// codexSubscriptionEvery; an older, missing or damaged one is as if Codex had
// never been asked.
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
	if age := c.now().Sub(record.At); age < 0 || age >= codexSubscriptionEvery {
		return
	}
	c.codexSubscriptionAt, c.codexSubscriptionLast = record.At, record.Usage
}

func (c *Collector) saveCodexSubscription() {
	if c.usageCacheDir == "" {
		return
	}
	raw, err := json.Marshal(codexSubscriptionRecord{At: c.codexSubscriptionAt, Usage: c.codexSubscriptionLast})
	if err == nil {
		_ = writeUsageCache(filepath.Join(c.usageCacheDir, codexSubscriptionFile), raw)
	}
}
