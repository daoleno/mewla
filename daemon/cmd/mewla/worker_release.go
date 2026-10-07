package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/daoleno/mewla/daemon/control"
)

func runWorkerRelease(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla worker release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_release"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.BoolVar(&cfg.json, "json", true, "print JSON")
	fs.StringVar(&req.WorkerID, "id", "", "Worker session id")
	fs.IntVar(&req.ProcessID, "pid", 0, "exact owned tool PID from telemetry")
	fs.StringVar(&req.ProcessStart, "start", "", "exact process start token from telemetry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || req.WorkerID == "" || req.ProcessID <= 1 || req.ProcessStart == "" {
		return fmt.Errorf("usage: mewla worker release -id SESSION -pid PID -start START")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runResources(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("mewla resources", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "resource_telemetry"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, true)
}
