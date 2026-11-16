package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/daoleno/mewla/daemon/control"
)

// runDevCommand holds commands for looking at the product while building it;
// it is left out of mewla --help.
//
//	mewla dev whats-new   show the newest What's New sheet again on the next quiet Brain home
func runDevCommand(args []string, stderr io.Writer) error {
	usage := "Usage: mewla dev whats-new [--state-dir DIR]\n\n  whats-new   show the What's New sheet again on the next quiet Brain home (phone or web)"
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help" || args[0] == "help") {
		// runCLI hands help's output in here (it goes to stdout).
		fmt.Fprintln(stderr, usage)
		return nil
	}
	if len(args) == 0 || args[0] != "whats-new" {
		return fmt.Errorf("%s", usage)
	}
	fs := flag.NewFlagSet("mewla dev whats-new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: false}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", false, "print JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "whats_new_replay"})
	if err != nil {
		return err
	}
	if cfg.json || !resp.OK {
		return writeControlResponse(os.Stdout, resp, true)
	}
	fmt.Fprintln(os.Stdout, resp.Text)
	return nil
}

func (a *controlApp) handleWhatsNewReplay() control.Response {
	if a == nil || a.whatsNew == nil {
		return control.ErrorResponse("whats_new_unavailable", "What's New is not available on this computer.")
	}
	record, err := a.whatsNew.Replay()
	if err != nil {
		return control.ErrorResponse("whats_new_failed", err.Error())
	}
	return control.Response{
		OK:      true,
		Context: record,
		Text:    "What's New shows again the next time Brain home is quiet, on the phone or the web.",
	}
}
