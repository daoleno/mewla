package main

import (
	"flag"
	"fmt"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/work"
	"io"
	"os"
)

func parseWorkerDefaultsArgs(args []string, stderr io.Writer) (cliConfig, control.Request, error) {
	fs := flag.NewFlagSet("zen worker defaults", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_defaults_get"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.BoolVar(&cfg.json, "json", true, "print JSON")
	fs.StringVar(&req.Executor, "executor", "", "client executor (e.g. codex)")
	fs.StringVar(&req.ModelID, "model", "", "default Worker model (empty inherits client config)")
	fs.StringVar(&req.ReasoningEffort, "reasoning", "", "default Worker reasoning (empty inherits client config)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen worker defaults [-executor codex -model gpt-6-astra -reasoning medium] [-json]")
		fmt.Fprintln(stderr, "No selection flags queries the resolved future delegated launch. Setting requires all three; existing Sessions and Brain are unchanged.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cfg, req, err
	}
	if fs.NArg() != 0 {
		return cfg, req, fmt.Errorf("unexpected arguments")
	}
	n := 0
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "executor" || f.Name == "model" || f.Name == "reasoning" {
			n++
		}
	})
	if n != 0 && n != 3 {
		return cfg, req, fmt.Errorf("set defaults with -executor, -model and -reasoning together (empty values clear overrides)")
	}
	if n == 3 {
		req.Type = "worker_defaults_set"
	}
	return cfg, req, nil
}
func runWorkerDefaults(args []string, stderr io.Writer) error {
	cfg, req, err := parseWorkerDefaultsArgs(args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}
func (a *controlApp) handleWorkerDefaults(req control.Request) control.Response {
	if a == nil || a.execs == nil {
		return control.ErrorResponse("executors_unavailable", "Executor config unavailable")
	}
	var d work.WorkerDefaults
	var err error
	if req.Type == "worker_defaults_set" {
		d, err = a.execs.SetWorkerDefaults(work.WorkerDefaults{Executor: req.Executor, Model: req.ModelID, Reasoning: req.ReasoningEffort})
	} else {
		d, err = a.execs.WorkerDefaults()
	}
	if err != nil {
		return control.ErrorResponse("worker_defaults_failed", err.Error())
	}
	return control.Response{OK: true, WorkerDefaults: &d}
}
