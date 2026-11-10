package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/remote"
)

// setupRemoteSteps are the moving parts of `mewla setup remote`, so the order
// and the skip rules can be tested without a service, a daemon or a phone.
type setupRemoteSteps struct {
	// signedIn reports whether remote access is already on for this computer.
	signedIn func() bool
	// login signs in with a device code (any device approves it) and turns
	// remote access on.
	login func(ctx context.Context) error
	// service makes sure Mewla runs in the background and says how.
	service func() (string, error)
	// pair prints the phone QR and link for one pairing code.
	pair func() error
	// phones counts the phones and browsers paired so far.
	phones func() (int, error)
	// agentReady reports whether an agent CLI is installed and signed in.
	agentReady func() bool
	sleep      func(time.Duration)
	now        func() time.Time
}

type setupRemoteConfig struct {
	stateDir string
	command  string // how this computer's mewla is invoked, for hints
	wait     bool
	waitFor  time.Duration
	// lan skips sign-in: the phone reaches this computer over the local network.
	lan bool
}

// errSetupRemoteStopped marks a step that failed; the run already printed why
// and how to resume, so main only sets the exit status.
var errSetupRemoteStopped = errors.New("setup stopped")

// errSetupRemoteIncomplete marks a run that finished its steps but did not see
// a phone pair; the printed hint says what to do next.
var errSetupRemoteIncomplete = errors.New("no phone paired yet")

func runSetupRemote(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla setup remote", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory for daemon identity and trusted devices")
	service := fs.String("service", "", "remote access service URL (default: the built-in service)")
	serviceCA := fs.String("service-ca", "", "extra CA certificate file for the service's HTTPS (development only)")
	name := fs.String("name", "", "word for this computer's address (default: random words)")
	address := fs.String("address", "", "pair through this address instead of remote access, e.g. https://mewla.example.com")
	lan := fs.Bool("lan", false, "skip sign-in: listen on this computer's Wi-Fi/LAN (plain HTTP, trusted networks only) and pair over it")
	noWait := fs.Bool("no-wait", false, "print the pairing code and return without waiting for the phone")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla setup remote [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Set this computer up from a terminal with no browser, such as over SSH:")
		fmt.Fprintln(stderr, "  1. sign in with a short code you approve on your phone (or any device);")
		fmt.Fprintln(stderr, "     with -lan there is no sign-in and the phone must share this computer's network")
		fmt.Fprintln(stderr, "  2. run Mewla in the background and start it with the computer")
		fmt.Fprintln(stderr, "  3. show a QR code and link that pair your phone, and wait for it")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Safe to run again: steps already done are skipped.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *lan && (strings.TrimSpace(*address) != "" || strings.TrimSpace(*name) != "" || strings.TrimSpace(*service) != "") {
		return errors.New("-lan pairs over the local network; it cannot be combined with -address, -name or -service")
	}
	resolved, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	prepareToolPath()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	steps := setupRemoteSteps{
		signedIn: func() bool {
			_, ok, _ := remote.LoadRemote(resolved)
			return ok && strings.TrimSpace(*address) == ""
		},
		login: func(ctx context.Context) error {
			if strings.TrimSpace(*address) != "" {
				return nil
			}
			_, err := remoteLogin(ctx, stderr, resolved, remoteLoginOptions{
				serviceURL:    *service,
				serviceCAFile: *serviceCA,
				machineName:   *name,
			})
			if err != nil {
				return err
			}
			return saveStartupChoice(resolved, choiceRemote)
		},
		service: func() (string, error) { return ensureBackgroundService(resolved, *stateDir, *lan) },
		pair: func() error {
			return runPair(pairConfig{stateDir: *stateDir, endpoint: strings.TrimSpace(*address), showLink: true}, stderr)
		},
		phones: func() (int, error) {
			devices, err := listDevices(*stateDir)
			if err != nil {
				return 0, err
			}
			count := 0
			for _, device := range devices {
				if device.Kind != auth.DeviceKindComputer {
					count++
				}
			}
			return count, nil
		},
		agentReady: func() bool {
			options := doctor.Options{LookPath: hostdeps.LookPath}
			return readyAgent(doctor.CheckExecutors(options)) != ""
		},
		sleep: time.Sleep,
		now:   time.Now,
	}
	cfg := setupRemoteConfig{
		stateDir: *stateDir,
		command:  "mewla",
		wait:     !*noWait,
		waitFor:  auth.DefaultPairingTTL,
		lan:      *lan,
	}
	return setupRemote(ctx, cfg, steps, stderr)
}

// setupRemote runs the steps in order. Sign-in comes first because it needs no
// daemon, so the service the next step starts already knows the address; the
// pairing code is issued last so its fifteen minutes start when the person is
// looking at it.
func setupRemote(ctx context.Context, cfg setupRemoteConfig, steps setupRemoteSteps, w io.Writer) error {
	style := styleForWriter(w)
	command := cfg.command
	if command == "" {
		command = "mewla"
	}
	step := func(number int, text string) {
		fmt.Fprintf(w, "\n%s %s\n", style.bold(fmt.Sprintf("%d/4", number)), text)
	}

	step(1, "Sign in")
	if cfg.lan {
		fmt.Fprintln(w, "  Skipped: this phone reaches the computer over the local network. Reach it from anywhere later: "+style.cmd(command+" login"))
	} else if steps.signedIn() {
		fmt.Fprintln(w, "  Remote access is already on for this computer.")
	} else if err := steps.login(ctx); err != nil {
		fmt.Fprintf(w, "\nSetup stopped: %s\n", firstLine(err))
		fmt.Fprintf(w, "Run %s again; steps already done are skipped.\n", style.cmd(command+" setup remote"))
		return errSetupRemoteStopped
	}

	step(2, "Run in the background")
	note, err := steps.service()
	if err != nil {
		fmt.Fprintf(w, "\nSetup stopped: %s\n", firstLine(err))
		fmt.Fprintf(w, "Run %s again once that is fixed.\n", style.cmd(command+" setup remote"))
		return errSetupRemoteStopped
	}
	if note != "" {
		fmt.Fprintln(w, "  "+note)
	}

	step(3, "Pair your phone")
	before, countErr := steps.phones()
	if err := steps.pair(); err != nil {
		fmt.Fprintf(w, "\nSetup stopped: %s\n", firstLine(err))
		fmt.Fprintf(w, "Run %s when it is fixed.\n", style.cmd(command+" pair"))
		return errSetupRemoteStopped
	}
	if !cfg.wait || countErr != nil {
		printSetupRemoteNext(w, style, command, steps.agentReady())
		return nil
	}

	step(4, "Waiting for your phone")
	deadline := steps.now().Add(cfg.waitFor)
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(w, "\nStopped waiting. The code still works until it expires; show it again with %s.\n", style.cmd(command+" pair"))
			return errSetupRemoteIncomplete
		default:
		}
		count, err := steps.phones()
		if err == nil && count > before {
			fmt.Fprintln(w, style.check(true, "Phone paired", "it shows in "+command+" devices list"))
			printSetupRemoteNext(w, style, command, steps.agentReady())
			return nil
		}
		if !steps.now().Before(deadline) {
			fmt.Fprintf(w, "\nNo phone paired before the code expired. Show a new one with %s.\n", style.cmd(command+" pair"))
			return errSetupRemoteIncomplete
		}
		steps.sleep(2 * time.Second)
	}
}

// firstLine is an error's first line: some carry a how-to-fix second line the
// run prints its own version of.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func printSetupRemoteNext(w io.Writer, style termStyle, command string, agentReady bool) {
	fmt.Fprintln(w)
	if !agentReady {
		fmt.Fprintln(w, style.bold("One more thing: no agent is signed in on this computer yet."))
		fmt.Fprintf(w, "  Brain cannot answer until one is. Run %s, then sign in to it.\n", style.cmd(command+" deps"))
		return
	}
	fmt.Fprintln(w, "Open Mewla on your phone and send Brain your first message.")
}

// bootServiceManagerAvailable reports whether `mewla boot install` can work
// here: a systemd user manager on Linux, launchd on macOS.
func bootServiceManagerAvailable() bool {
	switch bootGOOS {
	case "linux":
		_, err := (execBootRunner{}).run("systemctl", "--user", "show-environment")
		return err == nil
	case "darwin":
		return true
	default:
		return false
	}
}

// installBootService is `mewla boot install` for the current platform. A
// missing linger setting is not a failure: the service runs now and starts at
// the next login; the returned error then wraps errBootLingerRequired.
func installBootService(stateDir string, lan bool) error {
	args := []string{}
	if stateDir != "" {
		args = append(args, "-state-dir", stateDir)
	}
	if lan {
		args = append(args, "-lan")
	}
	config, err := parseBootConfig("mewla boot install", args)
	if err != nil {
		return err
	}
	var report bytes.Buffer
	if bootGOOS == "darwin" {
		return bootLaunchdInstall(config, execBootRunner{}, &report)
	}
	return bootInstall(config, execBootRunner{}, &report)
}

// ensureBackgroundService leaves Mewla running for this state directory and
// says how. An already running daemon is left alone. Where there is no
// service manager (a container, WSL without systemd) Mewla starts detached
// from this terminal and the note says it will not survive a restart.
func ensureBackgroundService(resolvedStateDir, stateDirFlag string, lan bool) (string, error) {
	if running, _ := bootStateLockHeld(resolvedStateDir); running {
		return "Mewla is already running.", nil
	}
	if bootServiceManagerAvailable() {
		note := "Mewla now runs in the background and starts with this computer. Turn it off: mewla boot uninstall"
		switch err := installBootService(stateDirFlag, lan); {
		case err == nil:
		case errors.Is(err, errBootLingerRequired):
			note = fmt.Sprintf("Mewla now runs in the background and starts when you log in.\n  To start it at boot before anyone logs in: sudo loginctl enable-linger %s", currentUserName())
		default:
			return "", fmt.Errorf("could not start Mewla in the background: %w", err)
		}
		if err := waitForDaemon(resolvedStateDir, 30*time.Second); err != nil {
			return "", err
		}
		return note, nil
	}
	logPath, err := startDetachedDaemon(resolvedStateDir, stateDirFlag, lan)
	if err != nil {
		return "", fmt.Errorf("could not start Mewla: %w", err)
	}
	if err := waitForDaemon(resolvedStateDir, 30*time.Second); err != nil {
		return "", fmt.Errorf("%w (log: %s)", err, logPath)
	}
	return "This computer has no service manager, so Mewla runs detached from this terminal.\n  It stops when the computer restarts; start it again with: mewla", nil
}

// startDetachedDaemon runs `mewla serve` in its own session with output in the
// state directory's log, and returns the log path.
func startDetachedDaemon(resolvedStateDir, stateDirFlag string, lan bool) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(resolvedStateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return "", err
	}
	logPath := filepath.Join(logDir, "serve.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	args := []string{"serve"}
	if stateDirFlag != "" {
		args = append(args, "-state-dir", stateDirFlag)
	}
	if lan {
		args = append(args, "--lan")
	}
	command := exec.Command(executable, args...)
	command.Stdin = nil
	command.Stdout, command.Stderr = logFile, logFile
	command.SysProcAttr = detachedSysProcAttr()
	if err := command.Start(); err != nil {
		return "", err
	}
	return logPath, command.Process.Release()
}

// waitForDaemon waits until a daemon owns the state directory.
func waitForDaemon(resolvedStateDir string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		if running, _ := bootStateLockHeld(resolvedStateDir); running {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("Mewla did not start; check mewla boot status")
		}
		time.Sleep(250 * time.Millisecond)
	}
}
