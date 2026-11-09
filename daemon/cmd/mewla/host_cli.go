package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/daoleno/mewla/daemon/watcher"
)

// errNoTerminalEngine is returned by `mewla host` in a build without
// libghostty-vt.
var errNoTerminalEngine = errors.New("this mewla was built without the session host's terminal engine (build tag ghosttyvt)")

func runHostCommand(args []string, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "status" {
		return runHostStatus(args[1:], stderr)
	}
	fs := flag.NewFlagSet("mewla host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir, socket string
	fs.StringVar(&stateDir, "state-dir", "", "state directory whose Sessions this host keeps (default: ~/.mewla)")
	fs.StringVar(&socket, "socket", "", "socket path (default: derived from the state directory)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla host [--state-dir DIR] [--socket PATH]")
		fmt.Fprintln(stderr, "       mewla host status [--state-dir DIR] [--socket PATH] [--json]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Run the session host: it owns the terminals of the Sessions it starts and")
		fmt.Fprintln(stderr, "keeps them running while the daemon restarts. The daemon starts it; run it by")
		fmt.Fprintln(stderr, "hand only to debug. Only this user can connect to its socket.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !ghosttyvt.Available() {
		return errNoTerminalEngine
	}
	path, err := hostSocketPath(stateDir, socket)
	if err != nil {
		return err
	}
	if err := sessionhost.EnsureSocketDir(path); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()
	server := sessionhost.NewServer(sessionhost.ServerOptions{SocketPath: path, HostVersion: hostVersion()})
	return server.Run(ctx)
}

func runHostStatus(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla host status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir, socket string
	var asJSON bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.StringVar(&socket, "socket", "", "socket path (default: derived from the state directory)")
	fs.BoolVar(&asJSON, "json", false, "print the host and its Sessions as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	path, err := hostSocketPath(stateDir, socket)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := sessionhost.Dial(ctx, path, hostVersion())
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("no session host is running on %s", path)
	}
	if err != nil {
		return err
	}
	defer client.Close()
	sessions, err := client.List(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Socket   string              `json:"socket"`
			Host     sessionhost.Welcome `json:"host"`
			Sessions []sessionhost.Info  `json:"sessions"`
		}{path, client.Welcome(), sessions})
	}
	return writeHostStatus(os.Stdout, path, client.Welcome(), sessions)
}

func writeHostStatus(out io.Writer, path string, host sessionhost.Welcome, sessions []sessionhost.Info) error {
	engine := "none"
	if host.Engine {
		engine = "libghostty-vt " + host.EngineVersion
	}
	fmt.Fprintf(out, "Session host %s, protocol %d, pid %d, up since %s\n", host.HostVersion, host.Protocol, host.PID, host.StartedAt.Local().Format(time.DateTime))
	fmt.Fprintf(out, "Socket %s, terminal engine %s\n", path, engine)
	if len(sessions) == 0 {
		fmt.Fprintln(out, "No Sessions.")
		return nil
	}
	fmt.Fprintln(out)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "SESSION\tPID\tSTATE\tSIZE\tCWD\tCOMMAND")
	for _, info := range sessions {
		state := "running"
		if !info.Alive {
			state = "exited"
			if info.ExitCode != nil {
				state = fmt.Sprintf("exited %d", *info.ExitCode)
			}
		}
		fmt.Fprintf(table, "%s\t%d\t%s\t%dx%d\t%s\t%s\n", info.ID, info.PID, state, info.Cols, info.Rows, info.Cwd, strings.Join(info.Argv, " "))
	}
	return table.Flush()
}

// configureSessionHost lets the watcher run Sessions in the session host
// (Settings → Labs → Session host). Sessions a host already runs are seen
// whatever the switch says; with the switch off and no host running, nothing
// is started, created or dialed. The switch only takes effect in a build that
// has the terminal engine, since the host runs from this binary.
func configureSessionHost(w *watcher.Watcher, stateDir string) {
	socket, err := sessionhost.SocketPath(stateDir)
	if err != nil {
		log.Printf("session host unavailable: %v", err)
		return
	}
	w.SetSessionHost(watcher.SessionHostConfig{
		SocketPath: socket,
		Executable: watcher.ExecutablePath(),
		StateDir:   stateDir,
		LogPath:    filepath.Join(stateDir, "session-host", "host.log"),
		Version:    hostVersion(),
		Enabled: func() bool {
			return ghosttyvt.Available() && sessionhost.LoadSettings(stateDir).Enabled
		},
	})
}

func hostSocketPath(stateDir, socket string) (string, error) {
	if strings.TrimSpace(socket) != "" {
		return socket, nil
	}
	return sessionhost.SocketPath(resolveCLIStateDir(stateDir))
}

func hostVersion() string {
	info := currentVersionInfo()
	if info.Commit == "" {
		return info.Version
	}
	return info.Version + " (" + info.Commit + ")"
}
