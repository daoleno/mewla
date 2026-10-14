package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/screen"
)

func runScreenCommand(args []string, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "preflight" {
		// Internal: the daemon asks a fresh process for permission state,
		// since macOS keeps answering a running process from its start.
		return json.NewEncoder(os.Stdout).Encode(screen.ProcessPermissions())
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, `Usage: mewla screen <status|disable|enable> [flags]

Screen lets paired phones and browsers view and control this computer. A
device turns it on from the app; this computer then asks once whether to
allow that device, and the operating system asks for its own permissions.

  status               Show support, permissions and allowed devices
  disable              Turn Screen off for every device and forget them
  enable               Allow devices to ask again after disable

Flags for enable:
  -allow-sleep         Let this computer sleep; by default it stays awake
                       (the display may sleep) while any device is allowed
`)
		return flag.ErrHelp
	}
	fs := flag.NewFlagSet("mewla screen "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	var outputJSON, allowSleep bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	if args[0] == "enable" {
		fs.BoolVar(&allowSleep, "allow-sleep", false, "let this computer sleep while Screen is allowed")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	service := screen.NewService(resolveCLIStateDir(stateDir))
	switch args[0] {
	case "enable":
		if err := service.EnableLocally(allowSleep); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Screen is on. Each device still asks once on this computer.")
		return printScreenStatus(service, resolveCLIStateDir(stateDir), outputJSON)
	case "disable":
		if err := service.Disable(); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Screen is off. Open viewers close within seconds, and every device must be allowed again.")
		return nil
	case "status":
		return printScreenStatus(service, resolveCLIStateDir(stateDir), outputJSON)
	}
	return fmt.Errorf("unknown screen command %q; see mewla screen --help", args[0])
}

func printScreenStatus(service *screen.Service, dir string, outputJSON bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := screen.LoadConfig(dir)
	if err != nil {
		return err
	}
	// Permissions belong to the daemon's process, not this command's (which
	// may run over SSH or in another terminal app), so ask the daemon.
	capability, live := daemonScreenCapability(dir)
	if !live {
		capability = service.Capability(ctx)
		capability.Permissions = nil
	}
	if outputJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"capability": capability, "config": map[string]any{
			"disabled": cfg.Disabled, "devices": cfg.Devices, "keep_awake": cfg.KeepAwake(),
		}})
	}
	if !capability.Supported {
		fmt.Fprintf(os.Stdout, "unsupported: %s\n", capability.Reason)
		return nil
	}
	switch {
	case cfg.Disabled:
		fmt.Fprintln(os.Stdout, "off (mewla screen enable to allow devices again)")
	case len(cfg.Devices) == 0:
		fmt.Fprintln(os.Stdout, "on; no device allowed yet (turn Screen on from the app)")
	default:
		fmt.Fprintln(os.Stdout, "on")
	}
	if !live {
		fmt.Fprintln(os.Stdout, "  permissions: unknown (Mewla isn't running)")
	}
	for _, p := range capability.Permissions {
		state := "granted"
		if !p.Granted {
			state = "missing"
			if p.RestartRequired {
				state = "granted; restart Mewla to apply"
			} else if p.Guidance != "" {
				state += ": " + p.Guidance
			}
		}
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", p.Name, state)
	}
	ids := make([]string, 0, len(cfg.Devices))
	for id := range cfg.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := cfg.Devices[id]
		fmt.Fprintf(os.Stdout, "  allowed: %s (%s, since %s)\n", d.Name, id, d.ApprovedAt.Local().Format("2006-01-02 15:04"))
	}
	if cfg.KeepAwake() {
		fmt.Fprintln(os.Stdout, "  keeping this computer awake (the display may sleep)")
	}
	return nil
}

// daemonScreenCapability asks the running daemon for its Screen capability.
func daemonScreenCapability(stateDir string) (screen.Capability, bool) {
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return screen.Capability{}, false
	}
	response, err := control.CallWithTimeout(socketPath, control.Request{Type: "screen_capability"}, 15*time.Second)
	if err != nil || !response.OK || response.ScreenCapability == nil {
		return screen.Capability{}, false
	}
	return *response.ScreenCapability, true
}
