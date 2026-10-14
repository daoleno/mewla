package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/remote"
)

func runLoginCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory for daemon identity and trusted devices")
	service := fs.String("service", "", "remote access service URL (default: the built-in service)")
	serviceCA := fs.String("service-ca", "", "extra CA certificate file for the service's HTTPS (development only)")
	name := fs.String("name", "", "name for this computer in its address (default: its hostname)")
	noBrowser := fs.Bool("no-browser", false, "do not open the sign-in page automatically")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla login [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Sign this computer in to remote access, so your phone and any browser")
		fmt.Fprintln(stderr, "reach it from any network. No Tailscale, port forwarding or domain needed.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resolvedStateDir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := remoteLogin(ctx, stderr, resolvedStateDir, remoteLoginOptions{
		serviceURL:    *service,
		serviceCAFile: *serviceCA,
		machineName:   *name,
		openBrowser:   !*noBrowser,
	}); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Mewla connects to it within a few seconds while it runs (start it with: mewla).\n")
	fmt.Fprintf(stderr, "Pair a phone or browser: %s\n\n", pairCommand(*stateDir))
	return nil
}

type remoteLoginOptions struct {
	serviceURL    string
	serviceCAFile string
	machineName   string
	openBrowser   bool
}

// remoteLogin signs this computer in with a device code: the person approves
// in a browser on any device, and the service assigns this computer its
// address. The account only finds the computer; every phone or browser still
// pairs with a code or a number confirmed here.
func remoteLogin(ctx context.Context, w io.Writer, stateDir string, options remoteLoginOptions) (remote.Config, error) {
	serviceURL := strings.TrimSuffix(strings.TrimSpace(options.serviceURL), "/")
	caPEM := ""
	if serviceURL == "" {
		// Signing in again keeps the service this computer already uses.
		if current, ok, _ := remote.LoadConfig(stateDir); ok {
			serviceURL, caPEM = current.ServiceURL, current.ServiceCAPEM
		}
	}
	if serviceURL == "" {
		serviceURL = remote.ServiceURL()
	}
	if serviceURL == "" {
		return remote.Config{}, errors.New("this Mewla build has no remote access service yet; pass -service URL")
	}
	if path := strings.TrimSpace(options.serviceCAFile); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return remote.Config{}, fmt.Errorf("read service CA: %w", err)
		}
		caPEM = string(raw)
	}
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		return remote.Config{}, fmt.Errorf("initialize auth manager: %w", err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, nil)
	if err != nil {
		return remote.Config{}, fmt.Errorf("initialize transport identity: %w", err)
	}
	client, err := remote.NewClient(serviceURL, caPEM, authManager)
	if err != nil {
		return remote.Config{}, err
	}
	machineName := strings.TrimSpace(options.machineName)
	if machineName == "" {
		machineName, _ = os.Hostname()
	}
	start, err := client.StartLogin(ctx, machineName, identity.RouteID)
	if err != nil {
		return remote.Config{}, fmt.Errorf("start sign-in: %w", err)
	}
	page := start.VerificationURI
	if start.VerificationURIComplete != "" {
		page = start.VerificationURIComplete
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Sign in to turn on remote access for this computer.")
	fmt.Fprintf(w, "  Open:  %s\n", start.VerificationURI)
	fmt.Fprintf(w, "  Code:  %s\n", start.UserCode)
	if options.openBrowser && openURL(page) == nil {
		fmt.Fprintln(w, "  (opened in your browser)")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Waiting for you to finish in the browser… (Ctrl-C to cancel)")

	interval := time.Duration(start.IntervalSeconds) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	deadline := time.UnixMilli(start.ExpiresAtMS)
	for {
		select {
		case <-ctx.Done():
			return remote.Config{}, errors.New("sign-in cancelled")
		case <-time.After(interval):
		}
		machine, err := client.PollLogin(ctx, start.LoginID)
		if errors.Is(err, remote.ErrLoginPending) {
			if start.ExpiresAtMS > 0 && time.Now().After(deadline) {
				return remote.Config{}, errors.New("the sign-in code expired; run mewla login again")
			}
			continue
		}
		if err != nil {
			return remote.Config{}, err
		}
		config := remote.Config{ServiceURL: serviceURL, ServiceCAPEM: caPEM, Machine: machine}
		if err := remote.SaveConfig(stateDir, config); err != nil {
			return remote.Config{}, fmt.Errorf("save remote access sign-in: %w", err)
		}
		account := machine.AccountLogin
		if machine.Provider != "" {
			account += " (" + machine.Provider + ")"
		}
		fmt.Fprintf(w, "\nSigned in as %s.\n", account)
		fmt.Fprintf(w, "This computer's address: %s\n", config.URL())
		return config, nil
	}
}

func runLogoutCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory for daemon identity and trusted devices")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resolvedStateDir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	config, ok, err := remote.LoadConfig(resolvedStateDir)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(stderr, "This computer is not signed in to remote access.")
		return nil
	}
	authManager, err := auth.NewManager(resolvedStateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}
	client, err := remote.NewClient(config.ServiceURL, config.ServiceCAPEM, authManager)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Logout(ctx); err != nil && !errors.Is(err, remote.ErrSignedOut) {
		return fmt.Errorf("sign out at the service: %w (nothing changed here; try again)", err)
	}
	if err := remote.RemoveSignIn(resolvedStateDir); err != nil {
		return err
	}
	if book, err := addressbook.New(resolvedStateDir); err == nil {
		_ = book.Remove(config.URL())
	}
	fmt.Fprintf(stderr, "Signed out. %s no longer reaches this computer.\n", config.URL())
	fmt.Fprintln(stderr, "Phones and browsers stay paired; they reach this computer again after mewla login.")
	return nil
}

// openURL opens a page in the default browser when this machine has one.
func openURL(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("no desktop session")
		}
		command = exec.Command("xdg-open", target)
	default:
		return errors.New("unsupported platform")
	}
	command.Stdout, command.Stderr = nil, nil
	return command.Start()
}

// buildRemotePairingLink pairs a phone through this computer's remote access
// relay with the pinned transport, or returns "" when signed out.
func buildRemotePairingLink(stateDir, daemonID, daemonPublicKey string, pairing auth.PairingToken) (string, string, error) {
	config, ok, err := remote.LoadConfig(stateDir)
	if err != nil || !ok {
		return "", "", err
	}
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		return "", "", fmt.Errorf("initialize auth manager: %w", err)
	}
	if authManager.DaemonID() != daemonID || authManager.PublicKeyHex() != daemonPublicKey {
		return "", "", errors.New("runtime owner pairing identity changed")
	}
	linkConfig, err := remote.ConnectorConfig(stateDir, config, authManager)
	if err != nil {
		return "", "", err
	}
	connectLink, err := buildLinkPairingLink(authManager, linkConfig, "remote access", pairing)
	if err != nil {
		return "", "", errors.New(remote.UserText(err.Error()))
	}
	return connectLink, "Remote access, " + config.URL(), nil
}

// writeRemoteStatus prints the remote access line under mewla address list.
func writeRemoteStatus(w io.Writer, stateDir string) {
	config, ok, err := remote.LoadConfig(stateDir)
	if err != nil || !ok {
		fmt.Fprintln(w, "\nRemote access is off. Reach this computer from anywhere: mewla login")
		return
	}
	status, running := remote.LoadStatus(stateDir)
	fmt.Fprintf(w, "\nRemote access: %s, signed in as %s\n", config.URL(), config.Machine.AccountLogin)
	switch {
	case !running:
		fmt.Fprintln(w, "  Mewla is not running; it connects when you start it.")
	case status.Phase == "connected":
		fmt.Fprintf(w, "  Connected through relay %s.\n", status.Relay)
	default:
		fmt.Fprintf(w, "  %s", status.Phase)
		if status.LastError != "" {
			fmt.Fprintf(w, ": %s", status.LastError)
		}
		fmt.Fprintln(w)
	}
	switch {
	case status.CertError != "":
		fmt.Fprintf(w, "  Certificate: %s\n", status.CertError)
	case !status.CertNotAfter.IsZero():
		fmt.Fprintf(w, "  Certificate valid until %s.\n", status.CertNotAfter.Local().Format("2006-01-02"))
	}
}
