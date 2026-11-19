package modelprofiles

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/daoleno/mewla/daemon/listenhandoff"
)

// A daemon restart must not cut a model reply. Every Worker reaches its model
// through the Owner's loopback listeners (the Session routes and the machine
// gateway), so before the daemon stops it drains them: each listener stops
// accepting while its socket stays open for the next daemon (listenhandoff),
// idle keep-alive connections close, and every reply in progress (an HTTP
// stream to its last byte, a WebSocket response to response.completed) runs
// to its end. A connection made meanwhile waits in the socket's queue for the
// next daemon instead of being refused.

// DrainResult is what a drain found and left.
type DrainResult struct {
	// Waited is how many replies were in progress when the drain began.
	Waited int
	// Cut is how many were still in progress when the drain's time ran out.
	Cut int
	// Held is the listening addresses kept open for the next daemon.
	Held []string
}

func (r DrainResult) plus(other DrainResult) DrainResult {
	return DrainResult{
		Waited: r.Waited + other.Waited,
		Cut:    r.Cut + other.Cut,
		Held:   append(append([]string(nil), r.Held...), other.Held...),
	}
}

// Drain stops the Owner's listeners taking new connections and waits until
// every model reply they pass has finished, or ctx ends. It is the last thing
// before Close: the listeners do not serve again.
func (o *Owner) Drain(ctx context.Context) DrainResult {
	if o == nil {
		return DrainResult{}
	}
	o.mu.Lock()
	ln, srv, router, gateway := o.ln, o.srv, o.router, o.gateway
	o.mu.Unlock()
	routes := make(chan DrainResult, 1)
	go func() {
		if ln == nil || router == nil {
			routes <- DrainResult{}
			return
		}
		routes <- drainListener(ctx, ln, srv, router.ws, func() int { return int(router.active.Load()) })
	}()
	result := gateway.Drain(ctx)
	return result.plus(<-routes)
}

// drainPoll is how often a drain looks at what is still in progress.
const drainPoll = 50 * time.Millisecond

func drainListener(ctx context.Context, ln net.Listener, server *http.Server, ws *wsConnRegistry, httpActive func() int) DrainResult {
	result := DrainResult{Waited: httpActive() + ws.activeTurns()}
	if listenhandoff.Hold(ln) {
		result.Held = []string{ln.Addr().String()}
	}
	ws.drain()
	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		if server != nil {
			// Closes the listener (the held copy keeps the socket), then the
			// idle connections, then waits for the active ones.
			_ = server.Shutdown(ctx)
		} else {
			_ = ln.Close()
		}
	}()
	tick := time.NewTicker(drainPoll)
	defer tick.Stop()
	for {
		select {
		case <-shutdown:
			if ws.open() == 0 {
				return result
			}
			shutdown = nil
		case <-tick.C:
			if shutdown == nil && ws.open() == 0 {
				return result
			}
		case <-ctx.Done():
			if shutdown != nil {
				<-shutdown
			}
			result.Cut = httpActive() + ws.activeTurns()
			return result
		}
	}
}
