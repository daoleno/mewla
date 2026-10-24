package brain

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// hostIdentityReproofInterval is how long a proven Host conversation identity
// stands before the live process is asked again. Proving a Codex Host means
// finding its rollout through the process table, which a one-second capture
// loop must not repeat for an identity that is saved and whose file is there.
// A /clear or resume in the Host shows up within this interval, or at once when
// the agent announces it (conversationRebinds).
const hostIdentityReproofInterval = 30 * time.Second

// hostIdentityProof remembers when the Host's saved conversation identity was
// last proven against its live process.
type hostIdentityProof struct {
	mu       sync.Mutex
	hostID   string
	identity work.HostTranscriptIdentity
	at       time.Time
	epoch    uint64
}

// fresh reports whether existing is the identity proven for hostID within the
// reproof interval, or since the agent last announced a new conversation when
// announcements cover the Host.
func (p *hostIdentityProof) fresh(hostID string, existing work.HostTranscriptIdentity, now time.Time, epoch uint64, covered bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !existing.Bound() || p.hostID != hostID || p.identity != existing || p.epoch != epoch {
		return false
	}
	return covered || now.Sub(p.at) < hostIdentityReproofInterval
}

func (p *hostIdentityProof) record(hostID string, resolved work.HostTranscriptIdentity, now time.Time, epoch uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostID, p.identity, p.at, p.epoch = hostID, resolved, now, epoch
}
