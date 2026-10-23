package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/control"
)

func runWorkerApprovals(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla worker approvals", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_approvals"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.StringVar(&req.WorkerID, "id", "", "only this Worker session id")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla worker approvals [-id <worker>]")
		fmt.Fprintln(stderr, "Lists the decisions agents are waiting on, with the options each one offers.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, true)
}

func runWorkerApprove(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla worker approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_approve"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.StringVar(&req.WorkerID, "id", "", "Worker session id")
	fs.StringVar(&req.ApprovalID, "approval", "", "approval id from `mewla worker approvals`")
	fs.StringVar(&req.OptionID, "option", "", "one of the options that approval offers")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla worker approve -id <worker> -approval <id> -option <option>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.ApprovalID) == "" || strings.TrimSpace(req.OptionID) == "" {
		return fmt.Errorf("-id, -approval and -option are required")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, true)
}
