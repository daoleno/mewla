package sessionhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// StartOptions say how to reach a host, and how to start one when none
// answers.
type StartOptions struct {
	SocketPath string
	// Executable is the mewla binary whose "host" subcommand serves; Args
	// come after "host --socket <path>".
	Executable string
	Args       []string
	// Env is the host's environment.
	Env []string
	// LogPath receives the host's output, appended.
	LogPath       string
	ClientVersion string
	// Wait bounds how long a started host may take to answer (default 10 s).
	Wait time.Duration
}

// Connect dials the host on opts.SocketPath and starts one first when none
// answers. started reports whether this call started it. The started host
// leads its own session, so it outlives the caller: Sessions keep running
// while the daemon restarts.
func Connect(ctx context.Context, opts StartOptions) (client *Client, started bool, err error) {
	if client, err := Dial(ctx, opts.SocketPath, opts.ClientVersion); err == nil {
		return client, false, nil
	} else if !IsAbsent(err) {
		return nil, false, err
	}
	exited, err := spawnHost(opts)
	if err != nil {
		return nil, false, err
	}
	wait := opts.Wait
	if wait <= 0 {
		wait = 10 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		client, err := Dial(ctx, opts.SocketPath, opts.ClientVersion)
		if err == nil {
			return client, true, nil
		}
		select {
		case exitErr := <-exited:
			// Another host may have won the socket's lock in the meantime.
			if client, dialErr := Dial(ctx, opts.SocketPath, opts.ClientVersion); dialErr == nil {
				return client, false, nil
			}
			return nil, false, fmt.Errorf("session host exited at start (%v); see %s", exitErr, opts.LogPath)
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return nil, false, fmt.Errorf("session host did not answer on %s within %s: %v; see %s", opts.SocketPath, wait, err, opts.LogPath)
		}
	}
}

// IsAbsent reports whether a dial failed because no host is there: no
// socket, or nothing listening on it.
func IsAbsent(err error) bool {
	return platform.NoListener(err)
}

// spawnHost starts `mewla host` detached and returns a channel that gets its
// exit if it ends.
func spawnHost(opts StartOptions) (<-chan error, error) {
	if opts.Executable == "" {
		return nil, fmt.Errorf("start session host: no executable")
	}
	var logFile *os.File
	if opts.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.LogPath), 0o700); err != nil {
			return nil, fmt.Errorf("start session host: log directory: %w", err)
		}
		f, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("start session host: log: %w", err)
		}
		logFile = f
		defer logFile.Close()
	}
	args := append([]string{"host", "--socket", opts.SocketPath}, opts.Args...)
	cmd, inUnit := hostUnitCommand(opts, args, logFile)
	if !inUnit {
		cmd = exec.Command(opts.Executable, args...)
		cmd.Env = opts.Env
		cmd.Dir = filepath.VolumeName(opts.Executable) + string(filepath.Separator)
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
	}
	// Its own session (Unix) or a detached process outside the caller's Job
	// (Windows): no terminal, and nothing sent to the caller reaches it.
	if err := platform.StartDetached(cmd); err != nil {
		return nil, fmt.Errorf("start session host: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}
