package codexctl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// AccountUsage is what Codex itself reports about the signed-in account's
// subscription quota. It is read through the app-server's own method
// `account/rateLimits/read`, so Mewla never opens Codex's credential file and
// never sends its token anywhere. Primary and Secondary are both nil when the
// account has no subscription windows.
type AccountUsage struct {
	Plan      string
	Primary   *UsageWindow
	Secondary *UsageWindow
}

// UsageWindow is one rolling quota window. UsedPercent is 0 for unused and
// 100 for exhausted.
type UsageWindow struct {
	UsedPercent   float64
	WindowMinutes int64
	ResetsAt      time.Time
}

// ReadAccountUsage starts a short-lived `codex app-server` on stdio, asks it
// for the account's rate limits, and stops it. A sign-in that has none (an API
// key, no sign-in) is an error from Codex, not an empty answer.
// binary is the codex executable; env is its environment (nil inherits).
func ReadAccountUsage(ctx context.Context, binary string, env []string) (AccountUsage, error) {
	cmd := procspawn.CommandContext(ctx, "codex app-server account", binary, "app-server", "--listen", "stdio://")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return AccountUsage{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return AccountUsage{}, err
	}
	if err := cmd.Start(); err != nil {
		return AccountUsage{}, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	rpc := newStdioRPC(stdin, stdout)

	if _, err := rpc.call(ctx, "initialize", map[string]any{
		"capabilities": map[string]any{"experimentalApi": true},
		"clientInfo":   map[string]any{"name": "mewla-stats", "version": "1"},
	}); err != nil {
		return AccountUsage{}, err
	}
	if err := rpc.notify("initialized"); err != nil {
		return AccountUsage{}, err
	}
	raw, err := rpc.call(ctx, "account/rateLimits/read", nil)
	if err != nil {
		return AccountUsage{}, err
	}
	var limits struct {
		RateLimits struct {
			PlanType  string      `json:"planType"`
			Primary   *wireWindow `json:"primary"`
			Secondary *wireWindow `json:"secondary"`
		} `json:"rateLimits"`
	}
	if err := json.Unmarshal(raw, &limits); err != nil {
		return AccountUsage{}, fmt.Errorf("%w: account/rateLimits/read: %v", ErrProtocol, err)
	}
	var usage AccountUsage
	usage.Plan = strings.TrimSpace(limits.RateLimits.PlanType)
	usage.Primary = limits.RateLimits.Primary.window()
	usage.Secondary = limits.RateLimits.Secondary.window()
	return usage, nil
}

type wireWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

func (w *wireWindow) window() *UsageWindow {
	if w == nil {
		return nil
	}
	out := &UsageWindow{UsedPercent: w.UsedPercent}
	if w.WindowDurationMins != nil {
		out.WindowMinutes = *w.WindowDurationMins
	}
	if w.ResetsAt != nil && *w.ResetsAt > 0 {
		out.ResetsAt = time.Unix(*w.ResetsAt, 0).UTC()
	}
	return out
}

// stdioRPC is the app-server's newline-delimited JSON-RPC over a child's
// stdin/stdout. Notifications that arrive while a call waits are dropped.
type stdioRPC struct {
	in     io.Writer
	out    *bufio.Reader
	nextID int
}

func newStdioRPC(in io.Writer, out io.Reader) *stdioRPC {
	return &stdioRPC{in: in, out: bufio.NewReaderSize(out, 1<<20)}
}

func (r *stdioRPC) notify(method string) error {
	line, _ := json.Marshal(map[string]any{"method": method})
	_, err := r.in.Write(append(line, '\n'))
	return err
}

func (r *stdioRPC) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r.nextID++
	id := r.nextID
	line, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := r.in.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	type frame struct {
		ID     *int            `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	done := make(chan struct {
		raw json.RawMessage
		err error
	}, 1)
	go func() {
		for {
			data, err := r.out.ReadBytes('\n')
			if err != nil {
				done <- struct {
					raw json.RawMessage
					err error
				}{nil, fmt.Errorf("%w: %v", ErrConnect, err)}
				return
			}
			var f frame
			if json.Unmarshal(data, &f) != nil || f.ID == nil || *f.ID != id {
				continue
			}
			if f.Error != nil {
				done <- struct {
					raw json.RawMessage
					err error
				}{nil, errors.New(method + ": " + f.Error.Message)}
				return
			}
			done <- struct {
				raw json.RawMessage
				err error
			}{f.Result, nil}
			return
		}
	}()
	select {
	case res := <-done:
		return res.raw, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
