package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"
)

// A Quick Tunnel's public hostname and readiness come from cloudflared's own
// metrics endpoint, not from its log lines: GET /quicktunnel answers
// {"hostname":"<name>.trycloudflare.com"} once the tunnel exists, and
// GET /ready answers 200 {"readyConnections":N,...} once N > 0 connections to
// the edge are up (503 before). Checked against cloudflared 2026.7.3.

type cloudflaredMetrics struct {
	address string
	client  *http.Client
}

func newCloudflaredMetrics(address string) cloudflaredMetrics {
	return cloudflaredMetrics{address: address, client: &http.Client{Timeout: 2 * time.Second}}
}

// reserveLoopbackAddress picks a free loopback address for cloudflared's
// --metrics flag. The port is released before cloudflared binds it; if another
// process takes it in between, cloudflared exits and the tunnel reports failure.
func reserveLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

var quickTunnelHostname = regexp.MustCompile(`^[a-z0-9-]+\.trycloudflare\.com$`)

// publicURL is the tunnel's https URL, or "" while cloudflared has not
// published one. A hostname outside trycloudflare.com is an error.
func (m cloudflaredMetrics) publicURL(ctx context.Context) (string, error) {
	status, body, err := m.get(ctx, "/quicktunnel")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("cloudflared /quicktunnel answered HTTP %d", status)
	}
	var answer struct {
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", fmt.Errorf("cloudflared /quicktunnel: %w", err)
	}
	if answer.Hostname == "" {
		return "", nil
	}
	if !quickTunnelHostname.MatchString(answer.Hostname) {
		return "", fmt.Errorf("cloudflared /quicktunnel named %q, not a trycloudflare.com host", answer.Hostname)
	}
	return "https://" + answer.Hostname, nil
}

// ready reports whether cloudflared holds at least one connection to the edge.
func (m cloudflaredMetrics) ready(ctx context.Context) (bool, error) {
	status, body, err := m.get(ctx, "/ready")
	if err != nil {
		return false, err
	}
	if status == http.StatusServiceUnavailable {
		return false, nil
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("cloudflared /ready answered HTTP %d", status)
	}
	var answer struct {
		ReadyConnections int `json:"readyConnections"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return false, fmt.Errorf("cloudflared /ready: %w", err)
	}
	return answer.ReadyConnections > 0, nil
}

func (m cloudflaredMetrics) get(ctx context.Context, path string) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+m.address+path, nil)
	if err != nil {
		return 0, nil, err
	}
	response, err := m.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	return response.StatusCode, body, err
}

// pollCloudflared asks cloudflared's metrics endpoint twice a second for the
// tunnel's public URL and for its first edge connection, and returns once it
// has delivered both or ctx ends. Errors before the endpoint is up are
// expected; a tunnel that never gets there is reported by the caller's startup
// timeout.
func pollCloudflared(ctx context.Context, metrics cloudflaredMetrics, announced chan<- string, connected chan<- struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	haveURL, haveConnection := false, false
	for !haveURL || !haveConnection {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !haveURL {
			if publicURL, err := metrics.publicURL(ctx); err == nil && publicURL != "" {
				haveURL = true
				select {
				case announced <- publicURL:
				default:
				}
			}
		}
		if !haveConnection {
			if ready, err := metrics.ready(ctx); err == nil && ready {
				haveConnection = true
				select {
				case connected <- struct{}{}:
				default:
				}
			}
		}
	}
}
