package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// On Windows `mewla boot` registers a per-user Task Scheduler task that
// starts the daemon when this user logs in. A standard user can register it;
// it needs no administrator and no service.
const (
	bootTaskName   = "Mewla"
	bootTaskMarker = "Managed by mewla boot install"
)

// bootTaskCurrentUser is overridden by tests.
var bootTaskCurrentUser = platform.CurrentUser

func bootTaskXMLPath(config bootConfig) string {
	return filepath.Join(config.StateDir, "boot", "mewla-task.xml")
}

// bootTaskDefinition is the task: conhost --headless hosts the daemon's
// console, so logging in opens no window. The daemon writes its own log
// under the state directory.
func bootTaskDefinition(config bootConfig) (platform.LogonTask, error) {
	if err := validateBootConfig(config); err != nil {
		return platform.LogonTask{}, err
	}
	account, sid, err := bootTaskCurrentUser()
	if err != nil {
		return platform.LogonTask{}, fmt.Errorf("identify the current user: %w", err)
	}
	daemon := []string{config.Binary, "-state-dir", config.StateDir}
	if config.LAN {
		daemon = append(daemon, "-lan")
	} else {
		daemon = append(daemon, "-addr", config.Addr)
	}
	return platform.LogonTask{
		Name:        bootTaskName,
		User:        account,
		SID:         sid,
		Command:     bootTaskConhost(),
		Arguments:   "--headless " + platform.QuoteArgs(daemon),
		WorkDir:     config.WorkDir,
		Description: "Starts Mewla when you log in. " + bootTaskMarker + ".",
	}, nil
}

func bootTaskConhost() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root + `\System32\conhost.exe`
}

// bootTaskQuery returns the registered task's XML, or ok=false when there is
// none.
func bootTaskQuery(runner bootRunner) (string, bool) {
	output, err := runner.run("schtasks", "/query", "/tn", bootTaskName, "/xml")
	if err != nil {
		return "", false
	}
	return string(output), true
}

func bootTaskInstall(config bootConfig, runner bootRunner, out io.Writer) error {
	if err := bootValidateLocalContext(config); err != nil {
		return err
	}
	task, err := bootTaskDefinition(config)
	if err != nil {
		return err
	}
	text := task.XML()
	if config.DryRun {
		fmt.Fprint(out, text)
		return nil
	}
	existing, registered := bootTaskQuery(runner)
	if registered && !strings.Contains(existing, bootTaskMarker) {
		return fmt.Errorf("refusing to replace a scheduled task named %q that mewla boot did not create", bootTaskName)
	}
	_, healthErr := bootProbeHealth(config.Addr)
	running := healthErr == nil
	if !running {
		if err := bootAddrFree(config.Addr); err != nil {
			return fmt.Errorf("listen address %s is not available: %v; stop the process using it first", config.Addr, err)
		}
	}
	path := bootTaskXMLPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create boot directory: %w", err)
	}
	if err := bootWriteFileAtomic(path, platform.UTF16File(text), 0o600); err != nil {
		return fmt.Errorf("write task definition: %w", err)
	}
	if output, err := runner.run("schtasks", "/create", "/tn", bootTaskName, "/xml", path, "/f"); err != nil {
		return fmt.Errorf("schtasks /create: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if err := writeBootMetadata(bootMetadataPath(path), bootMetadataFor(config)); err != nil {
		return fmt.Errorf("write boot metadata: %w", err)
	}
	action := "Installed; Mewla is already running"
	if !running {
		action = "Installed and started"
		if output, err := runner.run("schtasks", "/run", "/tn", bootTaskName); err != nil {
			return fmt.Errorf("schtasks /run: %v: %s", err, strings.TrimSpace(string(output)))
		}
		// The task runs only in this user's desktop session. Installed from
		// an SSH session with nobody logged in at the desktop, it waits for
		// the next login; Mewla is then started here, detached, for now.
		if bootTaskWaitHealthy(config, bootTaskStartWait) != nil {
			if err := bootTaskStartDirect(config); err != nil {
				return fmt.Errorf("start Mewla: %w", err)
			}
			action = "Installed; started now (the task starts it from your next login)"
		}
		if err := bootTaskWaitHealthy(config, bootVerifyTimeout); err != nil {
			return fmt.Errorf("Mewla did not answer on %s: %w; see %s", config.Addr, err, filepath.Join(config.StateDir, "logs", "daemon.log"))
		}
	}
	fmt.Fprintf(out, "%s: scheduled task %q\n", action, bootTaskName)
	fmt.Fprintf(out, "State directory: %s\n", config.StateDir)
	fmt.Fprintf(out, "Address: %s\n", config.Addr)
	fmt.Fprintf(out, "Log: %s\n", filepath.Join(config.StateDir, "logs", "daemon.log"))
	fmt.Fprintln(out, "Starts when you log in to Windows. Remove: mewla boot uninstall")
	return nil
}

// bootTaskStartWait is how long the task gets to bring Mewla up before it
// is started directly.
var bootTaskStartWait = 8 * time.Second

// bootTaskStartDirect starts the daemon the task would start, detached from
// this console and session.
func bootTaskStartDirect(config bootConfig) error {
	args := []string{"-state-dir", config.StateDir}
	if config.LAN {
		args = append(args, "-lan")
	} else {
		args = append(args, "-addr", config.Addr)
	}
	cmd := exec.Command(config.Binary, args...)
	cmd.Dir = config.WorkDir
	if err := platform.StartDetached(cmd); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func bootTaskWaitHealthy(config bootConfig, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	var err error
	for time.Now().Before(deadline) {
		if _, err = bootProbeHealth(config.Addr); err == nil {
			return nil
		}
		time.Sleep(bootVerifyInterval)
	}
	return err
}

func bootTaskStatus(runner bootRunner, out io.Writer) error {
	text, registered := bootTaskQuery(runner)
	if !registered {
		fmt.Fprintf(out, "No scheduled task %q is installed. Install: mewla boot install\n", bootTaskName)
		return nil
	}
	if !strings.Contains(text, bootTaskMarker) {
		fmt.Fprintf(out, "A scheduled task %q exists but was not created by mewla boot.\n", bootTaskName)
		return nil
	}
	fmt.Fprintf(out, "Scheduled task %q: starts Mewla when you log in\n", bootTaskName)
	if output, err := runner.run("schtasks", "/query", "/tn", bootTaskName, "/fo", "list", "/v"); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			for _, key := range []string{"Status:", "Last Run Time:", "Last Result:", "Task To Run:"} {
				if strings.HasPrefix(line, key) {
					fmt.Fprintf(out, "  %s\n", line)
				}
			}
		}
	}
	return nil
}

func bootTaskUninstall(runner bootRunner, out io.Writer) error {
	text, registered := bootTaskQuery(runner)
	if !registered {
		fmt.Fprintf(out, "No scheduled task %q is installed.\n", bootTaskName)
		return nil
	}
	if !strings.Contains(text, bootTaskMarker) {
		return fmt.Errorf("refusing to remove a scheduled task named %q that mewla boot did not create", bootTaskName)
	}
	// /end stops the daemon the task started; sessions in the session host
	// keep running, as they do when a Unix unit stops.
	_, _ = runner.run("schtasks", "/end", "/tn", bootTaskName)
	if output, err := runner.run("schtasks", "/delete", "/tn", bootTaskName, "/f"); err != nil {
		return errors.New("schtasks /delete: " + strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(out, "Removed scheduled task %q; Mewla no longer starts at login.\n", bootTaskName)
	return nil
}
