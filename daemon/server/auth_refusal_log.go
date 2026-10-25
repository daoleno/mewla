package server

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/clientaddr"
)

// refusalLogEvery is how often one kind of refusal from one device is logged.
// A client that retries a refused request every second leaves one line per
// interval with the count it stands for, not a line per retry.
const refusalLogEvery = 30 * time.Second

// refusalLogKeys bounds the memory a flood of made-up device IDs can use.
const refusalLogKeys = 256

type refusalLog struct {
	mu   sync.Mutex
	last map[string]refusalLogState
	now  func() time.Time
}

type refusalLogState struct {
	at         time.Time
	suppressed int
}

// allow reports whether a refusal under key should be logged now, and how many
// like it were held back since the last line.
func (l *refusalLog) allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	t := now()
	if l.last == nil {
		l.last = map[string]refusalLogState{}
	}
	state, seen := l.last[key]
	if seen && t.Sub(state.at) < refusalLogEvery {
		state.suppressed++
		l.last[key] = state
		return false, 0
	}
	if !seen && len(l.last) >= refusalLogKeys {
		for k, v := range l.last {
			if t.Sub(v.at) >= refusalLogEvery {
				delete(l.last, k)
			}
		}
		if len(l.last) >= refusalLogKeys {
			return false, 0
		}
	}
	l.last[key] = refusalLogState{at: t}
	return true, state.suppressed
}

// logRefusedRequest says in the log why a signed request was refused. The
// response a client gets stays as terse as before; this is for the owner of
// the computer, who otherwise sees a phone that cannot connect and no reason.
// It names the device the request claims to be from and what was refused; it
// never writes the Authorization header, nonce or signature.
func (s *Server) logRefusedRequest(r *http.Request, purpose string, err error) {
	reason, detail := auth.RefusalReason(err)
	if reason == "" {
		reason = "unauthorized"
	}
	device := auth.ClaimedDeviceID(authorizationFromRequest(r))
	if device == "" {
		device = "-"
	}
	allowed, held := s.refusals.allow(reason + "|" + device + "|" + purpose)
	if !allowed {
		return
	}
	line := fmt.Sprintf("auth: refused %s %s purpose=%s reason=%s device=%s remote=%s via=%s", r.Method, r.URL.Path, purpose, reason, device, r.RemoteAddr, clientaddr.Path(r))
	if detail != "" {
		line += " (" + detail + ")"
	}
	if held > 0 {
		line += fmt.Sprintf(" [%d more since the last line]", held)
	}
	log.Print(line)
}
