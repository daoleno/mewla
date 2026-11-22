package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/remote"
)

type macLANControl struct {
	stateDir string
	enabled  bool
	restart  func()
	mu       sync.Mutex
	changing bool
}

func (c *macLANControl) set(operation string) control.Response {
	if c == nil {
		return control.ErrorResponse("unmanaged", "This daemon's network settings are managed outside Mewla for Mac.")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changing {
		return control.ErrorResponse("restarting", "Mewla is applying its network setting.")
	}
	if operation != "on" && operation != "off" {
		return control.ErrorResponse("invalid_request", "Choose on or off.")
	}
	if (operation == "on") == c.enabled {
		return control.Response{OK: true}
	}
	choice := choiceLAN
	if operation == "off" {
		choice = choiceLater
		if _, signedIn, err := remote.LoadRemote(c.stateDir); err != nil {
			return control.ErrorResponse("read_failed", err.Error())
		} else if signedIn {
			choice = choiceRemote
		}
	}
	if err := saveStartupChoice(c.stateDir, choice); err != nil {
		return control.ErrorResponse("save_failed", err.Error())
	}
	c.changing = true
	return control.Response{OK: true, After: c.restart}
}

// The native host uses the owner's private control socket. Only an app-managed
// daemon handles the change, and the CLI waits for the new listener to answer.
func runMacLAN(args []string, output io.Writer) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		fmt.Fprintln(output, "usage: mewla mac-lan on|off")
		return flag.ErrHelp
	}
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		return errors.New("usage: mewla mac-lan on|off")
	}
	socket, err := control.DefaultSocketPath("")
	if err != nil {
		return err
	}
	response, err := control.CallWithTimeout(socket, control.Request{Type: "mac_lan", Operation: args[0]}, 5*time.Second)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("change trusted LAN access: %v", response.Error)
	}
	want := "127.0.0.1:9876"
	if args[0] == "on" {
		want = "0.0.0.0:9876"
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		status, err := bootDaemonStatus("")
		if err == nil && status.Addr == want {
			if _, err := probeDaemonHealth(want); err == nil {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("saved the network setting, but the new listener did not answer")
}
