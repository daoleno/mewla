package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/watch"
)

const watchUsage = `Usage: mewla watch <command> [flags]
Watch looks over your projects and records what needs you.

  status                           Signal counts per source and project, last pass, errors
  projects [--project id]          Inferred projects, their remotes and linked sources
  signals [--project id] [--source human|process|runtime|external] [--kind k] [--since 24h] [--limit n]
  problems [--project id] [--rule ci_red|repeated_error|worker_stalls]
  collect [--source s]             Run a collection pass now and print what it added
  enable | disable                 Turn the Watch setting on or off
  project rename --project id --name "New name"
  project link-linear --project id --team KEY --confirm
  project unlink-linear --project id

Output is JSON unless --json=false.`

func runWatchCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, watchUsage)
		return flag.ErrHelp
	}
	action := args[0]
	rest := args[1:]
	if action == "project" {
		if len(rest) == 0 || isHelpArg(rest[0]) {
			fmt.Fprintln(stderr, watchUsage)
			return flag.ErrHelp
		}
		action, rest = "project "+rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("mewla watch "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{}
	request := watch.Request{}
	var since, source string
	var confirm bool
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.BoolVar(&cfg.json, "json", true, "print JSON")
	fs.StringVar(&request.ProjectID, "project", "", "project ID or name")
	fs.StringVar(&source, "source", "", "signal source")
	fs.StringVar(&request.Filter.Kind, "kind", "", "signal kind")
	fs.StringVar(&request.Filter.Kind, "rule", "", "problem rule")
	fs.StringVar(&since, "since", "", "only signals newer than this duration, e.g. 24h")
	fs.IntVar(&request.Filter.Limit, "limit", 0, "maximum signals (default 50)")
	fs.StringVar(&request.Name, "name", "", "new project name")
	fs.StringVar(&request.TeamKey, "team", "", "Linear team key")
	fs.BoolVar(&confirm, "confirm", false, "confirm the suggested Linear team")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if since != "" {
		duration, err := time.ParseDuration(since)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		request.Filter.Since = time.Now().Add(-duration)
	}
	request.Filter.Source = source
	timeout := 30 * time.Second
	switch action {
	case "status", "projects", "signals", "problems":
		request.Action = action
	case "collect":
		request.Action = "collect"
		if source != "" {
			request.Sources = strings.Split(source, ",")
		}
		// The first collection reads three weeks of transcripts.
		timeout = 20 * time.Minute
	case "enable", "disable":
		request.Action, request.Enabled = "set_enabled", action == "enable"
	case "project rename":
		request.Action = "rename"
	case "project link-linear":
		if !confirm {
			return errors.New("link-linear needs --confirm: it starts collecting that team's issues for this project")
		}
		request.Action = "link_linear"
	case "project unlink-linear":
		request.Action = "unlink_linear"
	default:
		return fmt.Errorf("unknown watch command %q; see mewla watch --help", action)
	}
	if strings.HasPrefix(action, "project ") || request.Action == "signals" || request.Action == "problems" {
		resolved, err := resolveWatchProject(cfg, request.ProjectID)
		if err != nil {
			return err
		}
		request.ProjectID = resolved
		request.Filter.ProjectID = resolved
	}
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return err
	}
	response, err := control.CallWithTimeout(socketPath, control.Request{Type: "watch", WatchRequest: &request}, timeout)
	if err != nil {
		return err
	}
	if cfg.json || !response.OK || response.Watch == nil {
		return writeControlResponse(os.Stdout, response, true)
	}
	return writeWatchText(os.Stdout, *response.Watch)
}

// resolveWatchProject accepts a project name as well as its ID.
func resolveWatchProject(cfg cliConfig, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "p_") {
		return value, nil
	}
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return "", err
	}
	response, err := control.Call(socketPath, control.Request{Type: "watch", WatchRequest: &watch.Request{Action: "projects", ProjectID: value}})
	if err != nil {
		return "", err
	}
	if response.Watch == nil || len(response.Watch.Projects) != 1 {
		return "", fmt.Errorf("no project named %q", value)
	}
	return response.Watch.Projects[0].ID, nil
}

func writeWatchText(w io.Writer, response watch.Response) error {
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer table.Flush()
	switch {
	case response.Status != nil:
		raw, _ := json.MarshalIndent(response.Status, "", "  ")
		fmt.Fprintln(table, string(raw))
	case response.Pass != nil:
		raw, _ := json.MarshalIndent(response.Pass, "", "  ")
		fmt.Fprintln(table, string(raw))
	case response.Projects != nil:
		for _, project := range response.Projects {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", project.ID, project.Name, project.LastSeen.Local().Format("2006-01-02 15:04"), project.Root)
		}
	case response.Signals != nil:
		for _, signal := range response.Signals {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", signal.Time.Local().Format("01-02 15:04"), signal.Source, signal.Kind, signal.ProjectID, signal.Excerpt)
		}
	case response.Problems != nil:
		for _, problem := range response.Problems {
			fmt.Fprintf(table, "%s\t%s\t%v\t%d\t%s\n", problem.ID, problem.Status, problem.Active, problem.Count, problem.Title)
		}
	case response.Project != nil:
		fmt.Fprintf(table, "%s\t%s\n", response.Project.ID, response.Project.Name)
	case response.Config != nil:
		fmt.Fprintf(table, "watch enabled: %v\n", response.Config.Enabled)
	}
	return nil
}
