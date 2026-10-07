package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/server"
)

const defaultWebOrigin = "http://127.0.0.1:9876"

type webConfig struct {
	origin   string
	stateDir string
	noOpen   bool
}

// openBrowser hands a URL to the desktop's default browser.
var openBrowser = func(target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, target)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runWebCommand pairs a browser exactly like `mewla pair`: the local CLI asks
// the runtime owner for a one-time enrollment token, and the browser enrolls
// its own device key with it. The token rides in the URL fragment, which
// browsers never send to the daemon or a proxy.
func runWebCommand(args []string, stdout, stderr io.Writer) error {
	cfg, err := parseWebConfig(args, stderr)
	if err != nil {
		return err
	}
	pairingInfo, err := requestPairingToken(cfg.stateDir)
	if err != nil {
		return err
	}
	webURL, err := buildWebPairingURL(cfg.origin, pairingInfo.DaemonPublicKey, auth.PairingToken{
		Value:     pairingInfo.Token,
		ExpiresAt: pairingInfo.ExpiresAt,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(stderr, "Opening this link pairs the browser as a new device with access to sessions, terminal, Brain, Workers, and files.")
	fmt.Fprintf(stderr, "It works once and expires at %s. Revoke later with mewla devices revoke -id DEVICE_ID.\n", pairingInfo.ExpiresAt.Local().Format("15:04"))
	fmt.Fprintln(stdout, webURL)
	if cfg.noOpen {
		return nil
	}
	if err := openBrowser(webURL); err != nil {
		fmt.Fprintf(stderr, "Could not open a browser (%v); open the link above.\n", err)
	}
	return nil
}

func parseWebConfig(args []string, stderr io.Writer) (webConfig, error) {
	fs := flag.NewFlagSet("mewla web", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := webConfig{}
	fs.StringVar(&cfg.origin, "origin", defaultWebOrigin, "web UI origin: a loopback http address of this daemon, or an https -web-origin")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.BoolVar(&cfg.noOpen, "no-open", false, "print the pairing link without opening a browser")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla web [flags]")
		fmt.Fprintln(stderr, "Open the Mewla web UI served by the running daemon and pair this browser.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	origin, err := normalizeWebOrigin(cfg.origin)
	if err != nil {
		return cfg, err
	}
	cfg.origin = origin
	return cfg, nil
}

// normalizeWebOrigin accepts the daemon's loopback http address or an https
// origin the daemon admits through -web-origin.
func normalizeWebOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse web origin: %w", err)
	}
	if parsed.Scheme == "https" {
		return server.ParseWebOrigin(raw)
	}
	if parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
		return "", errors.New("web origin must be http on a loopback address (http://127.0.0.1:9876) or an https origin enabled with mewla -web-origin")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("web origin %q must not include credentials, a path, a query or a fragment", raw)
	}
	return "http://" + parsed.Host, nil
}

func buildWebPairingURL(origin, daemonPublicKey string, pairing auth.PairingToken) (string, error) {
	offers, err := buildConnectionOffersWithPublicKey(origin, daemonPublicKey, pairing)
	if err != nil {
		return "", fmt.Errorf("build connection info: %w", err)
	}
	if len(offers) == 0 {
		return "", errors.New("web origin is empty")
	}
	fragment := url.Values{}
	fragment.Set("pair", offers[0].ConnectLink)
	return origin + "/#" + fragment.Encode(), nil
}
