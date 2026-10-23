package stats

import (
	"context"
	"os/exec"
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
