package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// On Windows `mewla boot` registers a per-user Task Scheduler task that
// starts the daemon when this user logs in. A standard user can register it;
// it needs no administrator and no service.
//
// Task names are machine-wide, so each Windows user's task carries the user's
// name: "Mewla (ana)". Before 0.4.7 every user's task was "Mewla", and the
// second user to install replaced the first one's. That name is still read,
// and replaced, only when the task runs as this user.
const (
	bootLegacyTaskName = "Mewla"
	bootTaskMarker     = "Managed by mewla boot install"
)

// bootTaskCurrentUser is overridden by tests.
var bootTaskCurrentUser = platform.CurrentUser

// bootTaskNameFor is the task name of the Windows user account (DOMAIN\name).
func bootTaskNameFor(account string) string {
	name := account
	if index := strings.LastIndexAny(name, `\/`); index >= 0 {
		name = name[index+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return bootLegacyTaskName
	}
	return "Mewla (" + name + ")"
}

// bootTaskRef is this user's logon task: Name is the one it has or gets, and
// Legacy is set while it still is the "Mewla" of an earlier version.
type bootTaskRef struct {
	Name       string
	XML        string
	Registered bool
	Legacy     bool
}

// bootTaskFind looks up this user's task. A "Mewla" task that runs as another
// user is that user's and is not looked at again.
func bootTaskFind(runner bootRunner) (bootTaskRef, error) {
	account, sid, err := bootTaskCurrentUser()
	if err != nil {
		return bootTaskRef{}, fmt.Errorf("identify the current user: %w", err)
	}
	ref := bootTaskRef{Name: bootTaskNameFor(account)}
	if text, ok := bootTaskQueryName(runner, ref.Name); ok {
		ref.XML, ref.Registered = text, true
		return ref, nil
	}
	if ref.Name == bootLegacyTaskName {
		return ref, nil
	}
	if text, ok := bootTaskQueryName(runner, bootLegacyTaskName); ok && strings.Contains(text, bootTaskMarker) && bootTaskRunsAs(text, account, sid) {
		return bootTaskRef{Name: bootLegacyTaskName, XML: text, Registered: true, Legacy: true}, nil
	}
	return ref, nil
}

var bootTaskUserIDRe = regexp.MustCompile(`(?s)<Principal\b.*?<UserId>([^<]*)</UserId>`)

// bootTaskRunsAs: the task's principal is this user, by SID or account name
// (Task Scheduler may give back either).
func bootTaskRunsAs(text, account, sid string) bool {
	match := bootTaskUserIDRe.FindStringSubmatch(text)
	if match == nil {
		return false
	}
	user := strings.TrimSpace(match[1])
	if strings.EqualFold(user, sid) || strings.EqualFold(user, account) {
		return true
	}
	// A bare name is the local account's.
	short := account
	if index := strings.LastIndex(short, `\`); index >= 0 {
		short = short[index+1:]
	}
	return !strings.Contains(user, `\`) && strings.EqualFold(user, short)
}

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
		Name:        bootTaskNameFor(account),
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

// bootTaskQueryName returns the XML of the task called name, or ok=false when
// there is none.
func bootTaskQueryName(runner bootRunner, name string) (string, bool) {
	output, err := runner.run("schtasks", "/query", "/tn", name, "/xml")
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
	existing, err := bootTaskFind(runner)
	if err != nil {
		return err
	}
	if existing.Registered && !strings.Contains(existing.XML, bootTaskMarker) {
		return fmt.Errorf("refusing to replace a scheduled task named %q that mewla boot did not create", existing.Name)
	}
	name := task.Name
	running, note, err := bootTaskPickAddr(&config)
	if err != nil {
		return err
	}
	if note != "" {
		// The address moved: the task runs the daemon on the new one.
		if task, err = bootTaskDefinition(config); err != nil {
			return err
		}
		text = task.XML()
	}
	path := bootTaskXMLPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create boot directory: %w", err)
	}
	if err := bootWriteFileAtomic(path, platform.UTF16File(text), 0o600); err != nil {
		return fmt.Errorf("write task definition: %w", err)
	}
	if output, err := runner.run("schtasks", "/create", "/tn", name, "/xml", path, "/f"); err != nil {
		return fmt.Errorf("schtasks /create: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if existing.Legacy {
		// This user's task of an earlier version, now under its own name.
		// /delete does not stop the daemon it started.
		if output, err := runner.run("schtasks", "/delete", "/tn", bootLegacyTaskName, "/f"); err != nil {
			fmt.Fprintf(out, "Could not remove the earlier scheduled task %q: %v: %s\n", bootLegacyTaskName, err, strings.TrimSpace(string(output)))
		}
	}
	if err := writeBootMetadata(bootMetadataPath(path), bootMetadataFor(config)); err != nil {
		return fmt.Errorf("write boot metadata: %w", err)
	}
	action := "Installed; Mewla is already running"
	if !running {
		action = "Installed and started"
		if output, err := runner.run("schtasks", "/run", "/tn", name); err != nil {
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
	fmt.Fprintf(out, "%s: scheduled task %q\n", action, name)
	fmt.Fprintf(out, "State directory: %s\n", config.StateDir)
	fmt.Fprintf(out, "Address: %s%s\n", config.Addr, note)
	fmt.Fprintf(out, "Log: %s\n", filepath.Join(config.StateDir, "logs", "daemon.log"))
	fmt.Fprintln(out, "Starts when you log in to Windows. Remove: mewla boot uninstall")
	return nil
}

// bootTaskStartWait is how long the task gets to bring Mewla up before it
// is started directly.
var bootTaskStartWait = 8 * time.Second

// bootTaskStartDirect starts the daemon as the task would, detached from this
// console and session: under a headless console host of its own, exactly the
// task's command, so `mewla update` and `mewla restart` restart it the same
// way as one the task started (a daemon is restarted only when its parent is
// that console host). It gets no standard handles, as from the task: the
// console host would end at once on any it was given.
func bootTaskStartDirect(config bootConfig) error {
	cmd := bootTaskDirectCommand(config)
	return platform.StartDetachedBare(cmd.Path, cmd.Args[1:], config.WorkDir)
}

// bootTaskDirectCommand is the task's command: conhost --headless and the
// daemon with its state directory and address.
func bootTaskDirectCommand(config bootConfig) *exec.Cmd {
	args := []string{"--headless", config.Binary, "-state-dir", config.StateDir}
	if config.LAN {
		args = append(args, "-lan")
	} else {
		args = append(args, "-addr", config.Addr)
	}
	return exec.Command(bootTaskConhost(), args...)
}

func bootTaskWaitHealthy(config bootConfig, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	var err error
	for time.Now().Before(deadline) {
		if err = bootServedByThisUser(config); err == nil {
			return nil
		}
		time.Sleep(bootVerifyInterval)
	}
	return err
}

// errBootServedByAnother: a Mewla answers on the address, as another state
// directory's daemon (on Windows, another user's).
var errBootServedByAnother = errors.New("another Mewla answers on this address")

// bootServedByThisUser: the daemon on config.Addr is the one of
// config.StateDir. A state directory whose daemon never ran has no identity
// yet, so nothing answering is it.
func bootServedByThisUser(config bootConfig) error {
	served, err := bootProbeHealth(config.Addr)
	if err != nil {
		return err
	}
	if expected, err := bootStateDaemonID(config.StateDir); err != nil || expected != served {
		return errBootServedByAnother
	}
	return nil
}

// bootTaskPortsTried is how many ports after the default boot install tries
// when another user's Mewla has the default.
const bootTaskPortsTried = 20

// bootTaskScanFrom is the address that moves to a later port when it is
// taken (tests move it off 9876).
var bootTaskScanFrom = bootDefaultAddr

// bootTaskPickAddr says whether this user's Mewla already runs on
// config.Addr. When the default address belongs to another user's Mewla (two
// people sharing one Windows computer), it moves config.Addr to the first port
// after it that is this user's or free, and note says so.
func bootTaskPickAddr(config *bootConfig) (running bool, note string, err error) {
	first := config.Addr
	candidates := []string{first}
	if first == bootTaskScanFrom {
		host, port, _ := net.SplitHostPort(first)
		base, _ := strconv.Atoi(port)
		for offset := 1; offset <= bootTaskPortsTried; offset++ {
			candidates = append(candidates, net.JoinHostPort(host, strconv.Itoa(base+offset)))
		}
	}
	var firstErr error
	why := "in use"
	for _, addr := range candidates {
		probe := *config
		probe.Addr = addr
		served := bootServedByThisUser(probe)
		switch {
		case served == nil:
			running = true
		case errors.Is(served, errBootServedByAnother):
			if firstErr == nil {
				firstErr, why = fmt.Errorf("another user's Mewla is running on %s", addr), "another user's Mewla"
			}
			continue
		default:
			if free := bootAddrFree(addr); free != nil {
				if firstErr == nil {
					firstErr = free
				}
				continue
			}
		}
		config.Addr = addr
		if addr != first {
			note = fmt.Sprintf(" (%s is %s)", first, why)
		}
		return running, note, nil
	}
	return false, "", fmt.Errorf("listen address %s is not available: %v; stop the process using it first", first, firstErr)
}

func bootTaskStatus(runner bootRunner, out io.Writer) error {
	task, err := bootTaskFind(runner)
	if err != nil {
		return err
	}
	if !task.Registered {
		fmt.Fprintf(out, "No scheduled task %q is installed. Install: mewla boot install\n", task.Name)
		return nil
	}
	if !strings.Contains(task.XML, bootTaskMarker) {
		fmt.Fprintf(out, "A scheduled task %q exists but was not created by mewla boot.\n", task.Name)
		return nil
	}
	fmt.Fprintf(out, "Scheduled task %q: starts Mewla when you log in\n", task.Name)
	if output, err := runner.run("schtasks", "/query", "/tn", task.Name, "/fo", "list", "/v"); err == nil {
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
	task, err := bootTaskFind(runner)
	if err != nil {
		return err
	}
	if !task.Registered {
		fmt.Fprintf(out, "No scheduled task %q is installed.\n", task.Name)
		return nil
	}
	if !strings.Contains(task.XML, bootTaskMarker) {
		return fmt.Errorf("refusing to remove a scheduled task named %q that mewla boot did not create", task.Name)
	}
	// /end stops the daemon the task started; sessions in the session host
	// keep running, as they do when a Unix unit stops.
	_, _ = runner.run("schtasks", "/end", "/tn", task.Name)
	if output, err := runner.run("schtasks", "/delete", "/tn", task.Name, "/f"); err != nil {
		return errors.New("schtasks /delete: " + strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(out, "Removed scheduled task %q; Mewla no longer starts at login.\n", task.Name)
	return nil
}
