package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"golang.org/x/term"
)

func runScreenCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, `Usage: mewla screen <enable|disable|status> [flags]

Lets paired phones and browsers view and control this computer's screen.
Off until you turn it on here. On a Mac it uses Screen Sharing, which must be
on, with "VNC viewers may control screen with password" set in its Computer
Settings. Mewla signs in to it on 127.0.0.1 with that password, which stays
in this computer's Mewla state directory.

  enable               Turn screen control on; asks for the VNC password
  disable              Turn screen control off and forget the password
  status               Show whether a paired device can open the screen

Flags for enable:
  -password-stdin      Read the VNC password from standard input
  -address host:port   Loopback RFB address (default 127.0.0.1:5900)
  -allow-sleep         Let this computer sleep; by default it stays awake
                       (display may sleep) while screen control is on

A Mac laptop still sleeps when its lid is closed without an external display.
Keep the lid open and the charger connected while you are away.
`)
		return flag.ErrHelp
	}
	fs := flag.NewFlagSet("mewla screen "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir, address string
	var passwordStdin, outputJSON, allowSleep bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	if args[0] == "enable" {
		fs.BoolVar(&passwordStdin, "password-stdin", false, "read the VNC password from standard input")
		fs.StringVar(&address, "address", screen.DefaultAddress, "loopback RFB address")
		fs.BoolVar(&allowSleep, "allow-sleep", false, "let this computer sleep while screen control is on")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	dir := resolveCLIStateDir(stateDir)
	switch args[0] {
	case "enable":
		if err := screen.ValidateAddress(address); err != nil {
			return err
		}
		password, err := readScreenPassword(passwordStdin, stderr)
		if err != nil {
			return err
		}
		cfg := screen.Config{Enabled: true, Password: password, AllowSleep: allowSleep}
		if address != screen.DefaultAddress {
			cfg.Address = address
		}
		if err := screen.SaveConfig(dir, cfg); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Screen control is on for paired devices.")
		return printScreenStatus(dir, outputJSON)
	case "disable":
		if err := screen.SaveConfig(dir, screen.Config{}); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Screen control is off.")
		return nil
	case "status":
		return printScreenStatus(dir, outputJSON)
	}
	return fmt.Errorf("unknown screen command %q; see mewla screen --help", args[0])
}

func readScreenPassword(fromStdin bool, stderr io.Writer) (string, error) {
	var password string
	if fromStdin || !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		password = strings.TrimRight(line, "\r\n")
	} else {
		fmt.Fprint(stderr, "VNC password from Screen Sharing → Computer Settings (empty if none): ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		password = string(raw)
	}
	if len(password) > screen.MaxPasswordLength {
		return "", fmt.Errorf("VNC passwords are at most %d characters; Screen Sharing ignores the rest", screen.MaxPasswordLength)
	}
	return password, nil
}

func printScreenStatus(dir string, outputJSON bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := screen.LoadConfig(dir)
	if err != nil {
		return err
	}
	status := screen.NewService(dir).Status(ctx, true)
	if outputJSON {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	if !status.Enabled {
		fmt.Fprintln(os.Stdout, "disabled")
		return nil
	}
	address := cfg.Address
	if address == "" {
		address = screen.DefaultAddress
	}
	fmt.Fprintln(os.Stdout, "enabled")
	if status.KeepAwake {
		fmt.Fprintln(os.Stdout, "Keeping this computer awake while screen control is on (display may sleep)")
	} else {
		fmt.Fprintln(os.Stdout, "This computer may sleep; viewers cannot reach it while it sleeps")
	}
	if status.Available {
		fmt.Fprintf(os.Stdout, "Screen Sharing reachable on %s: %q %dx%d\n", address, status.Name, status.Width, status.Height)
	} else {
		fmt.Fprintf(os.Stdout, "Not reachable on %s: %s\n", address, status.Reason)
	}
	return nil
}
