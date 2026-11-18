package control

import (
	"encoding/json"
	"net"
	"os"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/platform"
)

// A restarting daemon closes its control socket for the few seconds before
// the new one opens it. A command in that window waits for the new daemon
// instead of saying Mewla is not running: the stopping daemon leaves a marker
// beside the socket (its pid and how long the restart may take) and the next
// one removes it as soon as it listens. A daemon that stops for good leaves no
// marker, and a marker whose process is gone or whose time has passed is
// ignored, so "not running" is still said at once when it is true.

const restartMarkerSuffix = ".restarting"

// restartPoll is how often a waiting command tries the socket again.
const restartPoll = 200 * time.Millisecond

type restartMarker struct {
	PID   int       `json:"pid"`
	Until time.Time `json:"until"`
}

// MarkRestarting records that the daemon serving socketPath (pid) is
// restarting and should answer again before until.
func MarkRestarting(socketPath string, pid int, until time.Time) error {
	raw, err := json.Marshal(restartMarker{PID: pid, Until: until.UTC()})
	if err != nil {
		return err
	}
	return atomicfile.Write(socketPath+restartMarkerSuffix, raw, 0o600)
}

// ClearRestarting removes the marker: the daemon is listening again.
func ClearRestarting(socketPath string) {
	_ = os.Remove(socketPath + restartMarkerSuffix)
}

// restartPending is the deadline of a restart in progress for socketPath.
func restartPending(socketPath string, now time.Time) (time.Time, bool) {
	raw, err := os.ReadFile(socketPath + restartMarkerSuffix)
	if err != nil {
		return time.Time{}, false
	}
	var marker restartMarker
	if json.Unmarshal(raw, &marker) != nil || marker.PID <= 0 || !now.Before(marker.Until) {
		return time.Time{}, false
	}
	if !platform.ProcessAlive(marker.PID) {
		return time.Time{}, false
	}
	return marker.Until, true
}

// dial connects to the control socket, waiting out a restart in progress.
func dial(socketPath string) (net.Conn, error) {
	for {
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err == nil || !platform.NoListener(err) {
			return conn, err
		}
		until, pending := restartPending(socketPath, time.Now())
		if !pending {
			return nil, err
		}
		wait := restartPoll
		if left := time.Until(until); left < wait {
			wait = left
		}
		time.Sleep(wait)
	}
}
