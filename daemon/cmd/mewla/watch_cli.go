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
  problems [--project id] [--rule judgment|ci_red|repeated_error|worker_stalls]
  problem label <id> real|not|later [--note "why"] [--by brain]
                                   Tell Watch whether a problem was real; later runs learn from it.
                                   Brain passes --by brain when it triages, so its verdicts are not
                                   mistaken for yours
  problem link <id> --work <work id> [--by brain]
                                   Tie a problem to the Work handling it: in progress marks it
                                   fixing, accepted Work marks it fixed
  problem unlink <id> --work <work id>
  collect [--source s]             Run a collection pass now and print what it added
  judge [--batches n]              Judge pending signals now (Watch must be enabled; costs tokens)
  enable | disable                 Turn Watch on or off; judgment runs only while it is on
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
	var positional []string
	if action == "problem" {
		need := map[string]int{"label": 2, "link": 1, "unlink": 1}
		if len(rest) < 2 || need[rest[0]] == 0 || len(rest) < 1+need[rest[0]] || isHelpArg(rest[1]) {
			fmt.Fprintln(stderr, watchUsage)
			return flag.ErrHelp
		}
		count := need[rest[0]]
		action, positional, rest = "problem "+rest[0], rest[1:1+count], rest[1+count:]
	}
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
	fs.StringVar(&request.Note, "note", "", "why, in your words")
	fs.IntVar(&request.Batches, "batches", 1, "judgment batches to run")
	fs.StringVar(&request.WorkID, "work", "", "Work id")
	fs.StringVar(&request.By, "by", "", "who is acting: brain, when triaging for the user")
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
	case "judge":
		request.Action = "judge"
		timeout = time.Duration(max(request.Batches, 1)) * 16 * time.Minute
	case "problem label":
		request.Action, request.ProblemID, request.Label = "label", positional[0], positional[1]
	case "problem link", "problem unlink":
		request.Action, request.ProblemID = strings.TrimPrefix(action, "problem "), positional[0]
		if strings.TrimSpace(request.WorkID) == "" {
			return errors.New("--work is required")
		}
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
		return controlError(cfg.stateDir, err)
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
		return "", controlError(cfg.stateDir, err)
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
			github, linear := "-", "-"
			if project.GitHub != nil {
				github = strings.TrimSpace("github " + project.GitHub.Coverage + " " + project.GitHub.Reason)
			}
			if project.Linear != nil && project.Linear.Confirmed != nil {
				linear = strings.TrimSpace("linear " + project.Linear.Coverage + " " + project.Linear.Reason)
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", project.ID, project.Name, project.LastSeen.Local().Format("2006-01-02 15:04"), project.Root, github, linear)
		}
	case response.Signals != nil:
		for _, signal := range response.Signals {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", signal.Time.Local().Format("01-02 15:04"), signal.Source, signal.Kind, signal.ProjectID, signal.Excerpt)
		}
	case response.Problems != nil:
		for _, problem := range response.Problems {
			cost := 0
			if problem.Cost != nil {
				cost = problem.Cost.Score
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%v\t%d\t%d\t%s\n", problem.ID, problem.Rule, problem.Status, problem.Active, cost, problem.Count, problem.Title)
		}
	case response.Problem != nil:
		fmt.Fprintf(table, "%s\t%s\t%s\n", response.Problem.ID, response.Problem.Status, response.Problem.Title)
	case response.Runs != nil:
		for _, run := range response.Runs {
			fmt.Fprintf(table, "%s\t%d signals\t%d placed\t%d new\t%d updated\t$%.3f\t%s\n", run.ID, run.Signals, run.Placed, run.ProblemsNew, run.ProblemsUpdated, run.Usage.CostUSD, run.Error)
		}
	case response.Project != nil:
		fmt.Fprintf(table, "%s\t%s\n", response.Project.ID, response.Project.Name)
	case response.Config != nil:
		fmt.Fprintf(table, "watch enabled: %v\n", response.Config.Enabled)
	}
	return nil
}
