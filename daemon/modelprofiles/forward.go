package modelprofiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Upstream retries, shared by the per-Session router and the machine
// gateway. A request is sent again only before anything reached the client:
// the body is held in memory, and a reply is handed on only once it is the
// one the client gets. A busy upstream (5xx, 529, a dropped connection) is
// asked twice more, after 1 s and 2 s; a rate limit three times, after 1, 2
// and 4 s. Either waits what Retry-After says instead when that is at most
// 8 s. A longer wait, a used-up quota or balance, and every other status go
// to the client at once: the agent's own retry and Mewla's turn recovery
// handle what takes longer.
const (
	upstreamBusyRetries = 2
	upstreamRateRetries = 3
	upstreamRetryPause  = time.Second
	upstreamLongestWait = 8 * time.Second
	// upstreamPeekLimit bounds how much of a 429 body is read to tell a
	// used-up quota from a passing rate limit.
	upstreamPeekLimit = 64 << 10
)

// retrySleep waits d or until ctx ends; tests replace it.
var retrySleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sendUpstream sends req, whose body is body, and asks again while the answer
// is one that may pass. retry false sends it once (billed tool endpoints).
// The returned response is the client's answer; every earlier one is drained
// and closed.
func sendUpstream(client *http.Client, req *http.Request, body []byte, retry bool) (*http.Response, error) {
	busy, rate := 0, 0
	for {
		if busy+rate > 0 {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := client.Do(req)
		if !retry {
			return resp, err
		}
		pause, kind := retryDecision(req.Context(), resp, err)
		switch kind {
		case retryBusy:
			if busy >= upstreamBusyRetries {
				return resp, err
			}
			busy++
			if pause <= 0 {
				pause = upstreamRetryPause << (busy - 1)
			}
		case retryRate:
			if rate >= upstreamRateRetries {
				return resp, err
			}
			rate++
			if pause <= 0 {
				pause = upstreamRetryPause << (rate - 1)
			}
		default:
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, upstreamPeekLimit))
			resp.Body.Close()
		}
		if sleepErr := retrySleep(req.Context(), pause); sleepErr != nil {
			return nil, sleepErr
		}
	}
}

type retryKind int

const (
	retryNone retryKind = iota
	retryBusy
	retryRate
)

// retryDecision classifies one answer. A 429's body is read (bounded) and put
// back so a used-up quota is told apart; the pause it returns is the
// upstream's own Retry-After, or 0 for the default backoff.
func retryDecision(ctx context.Context, resp *http.Response, err error) (time.Duration, retryKind) {
	if ctx.Err() != nil {
		return 0, retryNone
	}
	if err != nil {
		if errors.Is(err, ErrUpstreamSSRF) || errors.Is(err, ErrUpstreamRedirect) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, retryNone
		}
		// An upstream that took the whole header wait (minutes) to say
		// nothing is not asked again: the client would wait that long twice
		// more.
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return 0, retryNone
		}
		return 0, retryBusy
	}
	switch status := resp.StatusCode; {
	case status == http.StatusTooManyRequests:
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, upstreamPeekLimit))
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(peek), resp.Body), resp.Body}
		if quotaUsedUp(peek) {
			return 0, retryNone
		}
		wait, known := upstreamRetryAfter(resp.Header, time.Now())
		if known && wait > upstreamLongestWait {
			return 0, retryNone
		}
		return wait, retryRate
	case status == http.StatusRequestTimeout, status == http.StatusInternalServerError,
		status == http.StatusBadGateway, status == http.StatusServiceUnavailable,
		status == http.StatusGatewayTimeout, status == 529, status >= 520 && status <= 524:
		wait, known := upstreamRetryAfter(resp.Header, time.Now())
		if known && wait > upstreamLongestWait {
			return 0, retryNone
		}
		return wait, retryBusy
	}
	return 0, retryNone
}

// quotaUsedUp tells a 429 that will not pass by itself (no credit, a plan's
// allowance spent) from a moment's rate limit.
func quotaUsedUp(body []byte) bool {
	lower := strings.ToLower(string(body))
	// Relays often answer in Chinese: 余额 (balance), 欠费 (in arrears),
	// 充值 (top up), 额度 (allowance).
	for _, marker := range []string{"insufficient_quota", "quota_exceeded", "exceeded your current quota", "insufficient balance", "insufficient_balance", "billing", "credit balance", "payment required", "usage limit", "usage_limit", "余额", "欠费", "充值", "额度已用"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// upstreamRetryAfter reads how long the upstream asks to wait: Retry-After
// (seconds or an HTTP date), retry-after-ms, or the reset of an
// x-ratelimit-reset-* header given in seconds ("1s", "6m0s" or "12").
func upstreamRetryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if v := strings.TrimSpace(h.Get("Retry-After-Ms")); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
			return time.Duration(s * float64(time.Second)), true
		}
		if at, err := http.ParseTime(v); err == nil {
			if d := at.Sub(now); d > 0 {
				return d, true
			}
			return 0, true
		}
	}
	longest, known := time.Duration(0), false
	for _, name := range []string{"X-Ratelimit-Reset-Requests", "X-Ratelimit-Reset-Tokens"} {
		v := strings.TrimSpace(h.Get(name))
		if v == "" {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			s, numErr := strconv.ParseFloat(v, 64)
			if numErr != nil {
				continue
			}
			d = time.Duration(s * float64(time.Second))
		}
		if d > longest {
			longest = d
		}
		known = true
	}
	return longest, known
}
