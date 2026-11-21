package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/grants"
)

// runAgentUseNeed is `mewla agent-use need <permission> --reason "..."`: it
// asks the running daemon for the permission card (daemon/grants) and prints
// what happened: asked, already asked, or already granted.
func runAgentUseNeed(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla agent-use need", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir, reason string
	var outputJSON bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.StringVar(&reason, "reason", "", "what the permission is for, one line the person reads")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	// The permission may come before, between or after the flags.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) > 1 {
		return fmt.Errorf("unexpected argument %q", positional[1])
	}
	var permission string
	if len(positional) == 1 {
		permission = positional[0]
	}
	if !grants.Valid(permission) {
		return errors.New("name the permission: screen_recording, accessibility or chrome")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New(`say what it is for: --reason "..."`)
	}
	resp, err := callControl(cliConfig{stateDir: stateDir}, control.Request{Type: "agent_use_need", Name: permission, Reason: reason})
	if err != nil {
		return err
	}
	if !resp.OK {
		message := "the request failed"
		if resp.Error != nil {
			message = resp.Error.Message
		}
		return errors.New(message)
	}
	if outputJSON {
		return json.NewEncoder(stdout).Encode(map[string]string{"permission": permission, "outcome": resp.Confirmation})
	}
	fmt.Fprintln(stdout, resp.Confirmation)
	return nil
}
