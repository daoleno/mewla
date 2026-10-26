package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/remote"
	"golang.org/x/term"
)

func runLoginCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory for daemon identity and trusted devices")
	service := fs.String("service", "", "remote access service URL (default: the built-in service)")
	serviceCA := fs.String("service-ca", "", "extra CA certificate file for the service's HTTPS (development only)")
	name := fs.String("name", "", "word for this computer's address; renames it when already signed in (default: random words)")
	noBrowser := fs.Bool("no-browser", false, "do not open the sign-in page automatically")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla login [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Turn on remote access for this computer, so your phone and any browser")
		fmt.Fprintln(stderr, "reach it from any network. No Tailscale, port forwarding or domain needed.")
		fmt.Fprintln(stderr, "A computer already signed in to a Mewla account (turning on Friends does that)")
		fmt.Fprintln(stderr, "turns it on without a second sign-in.")
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
	if strings.TrimSpace(*name) != "" && strings.TrimSpace(*service) == "" {
		if current, ok, err := remote.LoadAccount(resolvedStateDir); err != nil {
			return err
		} else if ok {
			return remoteRename(ctx, stderr, resolvedStateDir, current, *name)
		}
	}
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

// remoteLogin turns remote access on. A computer already signed in to a Mewla
// account (Friends does that) keeps that sign-in; otherwise the person signs in
// with a device code, approving in a browser on any device, and the service
// assigns this computer its address. The account only finds the computer; every
// phone or browser still pairs with a code or a number confirmed here.
func remoteLogin(ctx context.Context, w io.Writer, stateDir string, options remoteLoginOptions) (remote.Config, error) {
	serviceURL := strings.TrimSuffix(strings.TrimSpace(options.serviceURL), "/")
	caPEM := ""
	current, signedIn, _ := remote.LoadAccount(stateDir)
	if serviceURL == "" && signedIn {
		// Signing in again keeps the service this computer already uses.
		serviceURL, caPEM = current.ServiceURL, current.ServiceCAPEM
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
	client, err := remote.NewClient(serviceURL, caPEM, authManager)
	if err != nil {
		return remote.Config{}, err
	}
	if signedIn && serviceURL == current.ServiceURL {
		machine, err := client.Machine(ctx)
		switch {
		case err == nil:
			config := remote.Config{ServiceURL: serviceURL, ServiceCAPEM: caPEM, RemoteAccess: true, Machine: machine}
			if err := remote.SaveConfig(stateDir, config); err != nil {
				return remote.Config{}, fmt.Errorf("save remote access sign-in: %w", err)
			}
			fmt.Fprintf(w, "Signed in as %s.\n", accountName(machine))
			fmt.Fprintf(w, "This computer's address: %s\n", config.URL())
			return config, nil
		case !errors.Is(err, remote.ErrSignedOut):
			return remote.Config{}, fmt.Errorf("reach the service: %w", err)
		}
		// The service forgot this computer: sign in again below.
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, nil)
	if err != nil {
		return remote.Config{}, fmt.Errorf("initialize transport identity: %w", err)
	}
	// An empty name lets the service pick random words: the address lands in
	// public certificate logs, so it never defaults to the hostname.
	start, err := client.StartLogin(ctx, strings.TrimSpace(options.machineName), identity.RouteID)
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
		config := remote.Config{ServiceURL: serviceURL, ServiceCAPEM: caPEM, RemoteAccess: true, Machine: machine}
		if err := remote.SaveConfig(stateDir, config); err != nil {
			return remote.Config{}, fmt.Errorf("save remote access sign-in: %w", err)
		}
		fmt.Fprintf(w, "\nSigned in as %s.\n", accountName(machine))
		fmt.Fprintf(w, "This computer's address: %s\n", config.URL())
		return config, nil
	}
}

// remoteRename moves a signed-in computer to a new address, and turns remote
// access on if it was off. Paired phones keep working; a browser pairs again at
// the new address because its key belongs to the old one.
func remoteRename(ctx context.Context, w io.Writer, stateDir string, config remote.Config, name string) error {
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}
	client, err := remote.NewClient(config.ServiceURL, config.ServiceCAPEM, authManager)
	if err != nil {
		return err
	}
	machine, err := client.Rename(ctx, strings.TrimSpace(name))
	if errors.Is(err, remote.ErrSignedOut) {
		return errors.New("this computer is no longer signed in to its Mewla account; run mewla login without -name first")
	}
	if err != nil {
		return fmt.Errorf("rename at the service: %w", err)
	}
	previous, wasOn := config.URL(), config.RemoteAccess
	config.Machine, config.RemoteAccess = machine, true
	if err := remote.SaveConfig(stateDir, config); err != nil {
		return fmt.Errorf("save remote access sign-in: %w", err)
	}
	if book, err := addressbook.New(stateDir); err == nil {
		if wasOn && previous != config.URL() {
			_ = book.Remove(previous)
		}
		_, _ = book.Add(config.URL(), addressbook.SourceRemote)
	}
	fmt.Fprintf(w, "This computer's address: %s\n", config.URL())
	if wasOn && previous != config.URL() {
		fmt.Fprintf(w, "%s no longer reaches it. Paired phones keep working; browsers pair again at the new address.\n", previous)
	}
	return nil
}

func runLogoutCommand(args []string, stderr io.Writer) error {
	return runLogout(args, os.Stdin, term.IsTerminal(int(os.Stdin.Fd())), stderr)
}

// runLogout turns remote access off. While Friends is on, the Mewla account
// stays signed in, because Friends is that account; otherwise the computer
// signs out completely. With -delete-account it deletes the whole account at
// the service, after the person types the account name (or passes -yes when no
// one is at a terminal).
func runLogout(args []string, stdin io.Reader, interactive bool, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory for daemon identity and trusted devices")
	deleteAccount := fs.Bool("delete-account", false, "delete your remote access account and sign out every computer on it")
	yes := fs.Bool("yes", false, "with -delete-account: do not ask for confirmation")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla logout [-delete-account [-yes]] [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Turn remote access off for this computer. Friends keeps working: while it is on,")
		fmt.Fprintln(stderr, "this computer stays signed in to its Mewla account. With -delete-account, delete")
		fmt.Fprintln(stderr, "the account (your GitHub id and login) and every computer signed in with it.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *yes && !*deleteAccount {
		return errors.New("-yes only confirms -delete-account")
	}
	resolvedStateDir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	config, ok, err := remote.LoadAccount(resolvedStateDir)
	if err != nil {
		return err
	}
	if !ok {
		if *deleteAccount {
			return errors.New("this computer is not signed in to a Mewla account; sign in with the account to delete (mewla login), then run mewla logout -delete-account")
		}
		fmt.Fprintln(stderr, "This computer is not signed in to a Mewla account.")
		return nil
	}
	if _, friendsOn := (&friends.Local{StateDir: resolvedStateDir}).Enabled(); friendsOn && !*deleteAccount {
		return stopRemoteKeepAccount(stderr, resolvedStateDir, config)
	}
	if *deleteAccount && !*yes {
		if err := confirmAccountDeletion(stdin, interactive, stderr, config.Machine); err != nil {
			return err
		}
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
	if *deleteAccount {
		// Friends people are Mewla accounts: they go first, while this
		// computer can still speak for the account.
		if err := deleteFriendsData(ctx, resolvedStateDir, authManager, stderr); err != nil {
			return fmt.Errorf("delete your Friends data first: %w (nothing changed; try again)", err)
		}
		if err := client.DeleteAccount(ctx); err != nil {
			if errors.Is(err, remote.ErrSignedOut) {
				return errors.New("the service no longer knows this computer, so it cannot tell which account to delete; sign in again (mewla login), then run mewla logout -delete-account")
			}
			return fmt.Errorf("delete the account at the service: %w (nothing changed here; try again)", err)
		}
	} else if err := client.Logout(ctx); err != nil && !errors.Is(err, remote.ErrSignedOut) {
		return fmt.Errorf("sign out at the service: %w (nothing changed here; try again)", err)
	}
	if err := remote.RemoveSignIn(resolvedStateDir); err != nil {
		return err
	}
	if book, err := addressbook.New(resolvedStateDir); err == nil {
		_ = book.Remove(config.URL())
	}
	if *deleteAccount {
		fmt.Fprintf(stderr, "Deleted the Mewla account %s.\n", accountName(config.Machine))
		if config.RemoteAccess {
			fmt.Fprintf(stderr, "%s no longer reaches this computer. ", config.URL())
		}
		fmt.Fprintln(stderr, "Other computers on the account drop off within an hour.")
		fmt.Fprintln(stderr, "To also remove Mewla's access on GitHub: https://github.com/settings/applications")
		return nil
	}
	if !config.RemoteAccess {
		fmt.Fprintf(stderr, "Signed out of the Mewla account %s.\n", accountName(config.Machine))
		return nil
	}
	fmt.Fprintf(stderr, "Signed out. %s no longer reaches this computer.\n", config.URL())
	fmt.Fprintln(stderr, "Phones and browsers stay paired; they reach this computer again after mewla login.")
	return nil
}

// stopRemoteKeepAccount turns remote access off and leaves the Mewla account
// signed in at the service, because Friends is that account.
func stopRemoteKeepAccount(w io.Writer, stateDir string, config remote.Config) error {
	if !config.RemoteAccess {
		fmt.Fprintf(w, "Remote access is already off. This computer stays signed in as %s for Friends.\n", accountName(config.Machine))
		return nil
	}
	if err := remote.StopRemote(stateDir); err != nil {
		return err
	}
	if book, err := addressbook.New(stateDir); err == nil {
		_ = book.Remove(config.URL())
	}
	fmt.Fprintf(w, "Remote access is off. %s no longer reaches this computer.\n", config.URL())
	fmt.Fprintf(w, "Friends keeps working: this computer stays signed in as %s. Turn Friends off in the App, then run mewla logout again to sign out.\n", accountName(config.Machine))
	fmt.Fprintln(w, "Phones and browsers stay paired; they reach this computer again after mewla login.")
	return nil
}

func accountName(machine remote.Machine) string {
	if machine.Provider == "" {
		return machine.AccountLogin
	}
	return machine.AccountLogin + " (" + machine.Provider + ")"
}

// confirmAccountDeletion asks the person to type the account name.
func confirmAccountDeletion(stdin io.Reader, interactive bool, w io.Writer, machine remote.Machine) error {
	if !interactive {
		return errors.New("deleting the account needs confirmation; run it in a terminal, or add -yes")
	}
	fmt.Fprintf(w, "This deletes the remote access account %s:\n", accountName(machine))
	fmt.Fprintln(w, "  - the service forgets your GitHub id and login;")
	fmt.Fprintln(w, "  - every computer signed in with it loses its address and is signed out;")
	fmt.Fprintln(w, "  - your Friends account, posts, comments and follows, if you turned Friends on, are deleted.")
	fmt.Fprintln(w, "Phones stay paired, and work on this network or another address you set up.")
	fmt.Fprintf(w, "Type %s to delete the account: ", machine.AccountLogin)
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(line), machine.AccountLogin) {
		return errors.New("not deleted: the name did not match")
	}
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
	config, ok, err := remote.LoadRemote(stateDir)
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
	config, ok, err := remote.LoadRemote(stateDir)
	if err != nil || !ok {
		if account, signedIn, _ := remote.LoadAccount(stateDir); signedIn {
			fmt.Fprintf(w, "\nRemote access is off. Signed in as %s for Friends only. Reach this computer from anywhere: mewla login\n", accountName(account.Machine))
			return
		}
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

// deleteFriendsData deletes this account's Friends person, if it has one.
// A service that does not serve Friends has nothing to delete.
func deleteFriendsData(ctx context.Context, stateDir string, authManager *auth.Manager, stderr io.Writer) error {
	local := &friends.Local{StateDir: stateDir, Auth: authManager}
	_, wasOn := local.Enabled()
	err := local.Disable(ctx)
	var refused *friends.ServiceError
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return nil
	}
	if err == nil && wasOn {
		fmt.Fprintln(stderr, "Deleted your Friends account and everything you posted there.")
	}
	return err
}
