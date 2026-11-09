//go:build windows

package workerproc

// Windows has no process ownership registry yet: the session host owns
// Session processes through Job Objects there. These types let the shared
// code build.
type ProcessIdentity struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

func bootID() string { return "" }

type Process struct {
	PID, PPID, PGID, SID                    int
	Start                                   string
	RSS, Ticks                              uint64
	UID                                     int
	Args, ResourceID, WorkerID, ContainerID string
}
