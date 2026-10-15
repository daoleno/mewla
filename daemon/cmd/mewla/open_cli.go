package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/remote"
)

// errMewlaNotRunning ends mewla open with status 1 after it said so.
var errMewlaNotRunning = errors.New("mewla is not running")

type openConfig struct {
	stateDir string
	print    bool
}

// openEnv is what mewla open needs from this computer, so tests run it
// without a daemon, a desktop or a browser.
type openEnv struct {
	running func(stateDir string) (bool, error)
	pair    func(stateDir string) (*control.PairingInfo, error)
	desktop func() (bool, string)
	open    func(target string) error
}

func defaultOpenEnv() openEnv {
	return openEnv{
		running: bootStateLockHeld,
		pair:    requestPairingToken,
		desktop: desktopSession,
		open:    openURL,
	}
}

func runOpenCommand(args []string, stderr io.Writer) error {
	cfg, err := parseOpenConfig(args, stderr)
	if err != nil {
		return err
	}
	return runOpen(cfg, stderr, defaultOpenEnv())
}

func parseOpenConfig(args []string, stderr io.Writer) (openConfig, error) {
	fs := flag.NewFlagSet("mewla open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := openConfig{}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.BoolVar(&cfg.print, "print", false, "print the addresses and a one-time link; do not open a browser")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla open [flags]   (also: mewla web)")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Open the Mewla web UI in this computer's browser, paired on first use, and")
		fmt.Fprintln(stderr, "print the addresses other browsers use. Without a desktop (over SSH, or no")
		fmt.Fprintln(stderr, "display) it only prints them.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}

// openView is everything mewla open prints.
type openView struct {
	stateDir  string
	local     string // this computer's web UI origin; "" when Mewla does not listen on loopback
	localLink string // one-time link that opens and pairs this computer's browser
	expiresAt time.Time
	opened    bool
	notOpened string // why nothing was opened, when it was not
	ssh       bool
	remoteURL string
	https     []string            // other HTTPS addresses serving the web UI
	phoneOnly []addressbook.Entry // Wi-Fi/LAN and Tailscale: the app only
}

func runOpen(cfg openConfig, w io.Writer, env openEnv) error {
	stateDir, err := auth.ResolveStorageDir(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("resolve auth storage directory: %w", err)
	}
	running, err := env.running(stateDir)
	if err != nil {
		return err
	}
	style := styleForWriter(w)
	if !running {
		fmt.Fprintln(w, "Mewla is not running. Start it with "+style.cmd(startCommand(cfg.stateDir))+", then run "+style.cmd(openCommand(cfg.stateDir))+" again.")
		return errMewlaNotRunning
	}
	pairingInfo, err := env.pair(cfg.stateDir)
	if err != nil {
		return err
	}
	book, err := addressbook.New(stateDir)
	if err != nil {
		return err
	}
	entries, err := book.List()
	if err != nil {
		return err
	}
	view := buildOpenView(entries, pairingInfo, stateDir)
	view.stateDir = cfg.stateDir

	desktop, reason := env.desktop()
	view.ssh = reason == "over SSH"
	switch {
	case view.localLink == "":
	case cfg.print:
		view.notOpened = "--print"
	case !desktop:
		view.notOpened = reason
	default:
		if err := env.open(view.localLink); err != nil {
			view.notOpened = "could not start a browser: " + err.Error()
		} else {
			view.opened = true
		}
	}
	printOpen(w, view)
	return nil
}

// buildOpenView sorts the address book into this computer, remote access,
// other HTTPS addresses and the phone-only plain HTTP addresses.
func buildOpenView(entries []addressbook.Entry, pairingInfo *control.PairingInfo, stateDir string) openView {
	view := openView{expiresAt: pairingInfo.ExpiresAt}
	if config, ok, err := remote.LoadConfig(stateDir); err == nil && ok {
		view.remoteURL = strings.TrimRight(config.URL(), "/")
	}
	pairing := auth.PairingToken{Value: pairingInfo.Token, ExpiresAt: pairingInfo.ExpiresAt}
	var localSeen time.Time
	for _, entry := range entries {
		address := strings.TrimRight(entry.URL, "/")
		switch classifyAddress(entry.URL) {
		case kindThisComputer:
			if view.local != "" && !entry.LastSeenAt.After(localSeen) {
				continue
			}
			view.local, localSeen = address, entry.LastSeenAt
			view.localLink = ""
			if pairingInfo.ShortLinks {
				view.localLink = shortBrowserLink(address, pairing.Value)
			}
			if view.localLink == "" {
				view.localLink, _ = buildWebPairingURL(address, pairingInfo.DaemonPublicKey, pairing)
			}
		case kindHTTPS:
			if address != view.remoteURL {
				view.https = append(view.https, address)
			}
		default:
			view.phoneOnly = append(view.phoneOnly, entry)
		}
	}
	sort.SliceStable(view.phoneOnly, func(i, j int) bool {
		return classifyAddress(view.phoneOnly[i].URL).phoneRank() < classifyAddress(view.phoneOnly[j].URL).phoneRank()
	})
	return view
}

// printOpen says what was opened and lists where else the web UI is.
func printOpen(w io.Writer, view openView) {
	style := styleForWriter(w)
	const pad = 16
	row := func(label, value string) {
		fmt.Fprintln(w, "  "+padRight(label, pad)+value)
	}
	more := func(text string) {
		fmt.Fprintln(w, "  "+strings.Repeat(" ", pad)+text)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.title("Mewla web UI"))
	switch {
	case view.local == "":
		row("This computer", style.dim("not served here: Mewla does not listen on 127.0.0.1"))
	case view.opened:
		row("This computer", view.local+style.dim(" · opened in your browser"))
	default:
		row("This computer", view.local)
	}
	if view.remoteURL != "" {
		row("Remote", view.remoteURL)
		more(style.dim("Open it in a browser on another computer, then match"))
		more(style.dim("its number here: ") + style.cmd(devicesPendingCommand(view.stateDir)))
	} else {
		row("Remote", "off "+style.dim("· reach it from other computers: ")+style.cmd("mewla login"))
	}
	for _, address := range view.https {
		row("HTTPS", address)
	}
	for _, entry := range view.phoneOnly {
		row(entryLabel(entry), entry.URL+style.dim(" · phone app only"))
	}
	if view.opened || view.localLink == "" {
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintln(w)
	switch {
	case view.notOpened == "--print":
		fmt.Fprintln(w, style.bold("Open on this computer"))
	case view.ssh:
		fmt.Fprintln(w, style.bold("Not opened: this is an SSH session"))
	default:
		fmt.Fprintln(w, style.bold("Not opened: "+view.notOpened))
	}
	fmt.Fprintln(w, "  "+view.localLink)
	fmt.Fprintln(w, "  "+style.dim("Pairs the browser that opens it first, once, until "+view.expiresAt.Local().Format("15:04")+"."))
	if view.ssh {
		port := "9876"
		if parsed, err := url.Parse(view.local); err == nil && parsed.Port() != "" {
			port = parsed.Port()
		}
		fmt.Fprintln(w, "  "+style.dim("From your own computer, forward the port first:"))
		fmt.Fprintln(w, "  "+style.cmd("ssh -L "+port+":127.0.0.1:"+port+" <this computer>"))
	}
	fmt.Fprintln(w)
}

// desktopSession reports whether a browser opened here reaches the person at
// this terminal, and why not otherwise. Over SSH it would open on the remote
// screen, if any.
func desktopSession() (bool, string) {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(name) != "" {
			return false, "over SSH"
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return true, ""
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false, "no desktop session"
		}
		return true, ""
	}
	return false, "opening a browser is not supported on " + runtime.GOOS
}

func openCommand(stateDir string) string {
	if strings.TrimSpace(stateDir) != "" {
		return "mewla open -state-dir " + stateDir
	}
	return "mewla open"
}

func startCommand(stateDir string) string {
	if strings.TrimSpace(stateDir) != "" {
		return "mewla -state-dir " + stateDir
	}
	return "mewla"
}

func devicesPendingCommand(stateDir string) string {
	if strings.TrimSpace(stateDir) != "" {
		return "mewla devices pending -state-dir " + stateDir
	}
	return "mewla devices pending"
}
