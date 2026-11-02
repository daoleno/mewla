package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/control"
)

func cloudCLIRequest(integration string, args []string) (connections.Request, error) {
	q := connections.Request{Action: "cloud_run", Integration: integration}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case integration == "gcp" && arg == "--account":
			i++
			if i >= len(args) {
				return q, errors.New("--account needs an email address")
			}
			q.Profile = "account:" + args[i]
		case integration == "gcp" && strings.HasPrefix(arg, "--account="):
			q.Profile = "account:" + strings.TrimPrefix(arg, "--account=")
		case arg == "--mewla-authorize":
			q.Authorize = true
		case arg == "--profile":
			i++
			if i >= len(args) {
				return q, errors.New("--profile needs a name")
			}
			q.Profile = args[i]
		case strings.HasPrefix(arg, "--profile="):
			q.Profile = strings.TrimPrefix(arg, "--profile=")
		default:
			q.CLIArgs = append(q.CLIArgs, arg)
		}
	}
	return q, nil
}
func runCloudCommand(integration string, args []string, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" {
		name := integration
		if name == "gcp" {
			name = "gcloud"
		}
		fmt.Fprintf(stderr, "Usage: mewla %s [--profile NAME] [--mewla-authorize] <CLI arguments>\nRead commands run directly. Other commands require enabled writes and explicit task-specific user authorization (--mewla-authorize). Profiles are managed in Extensions and the local vendor CLI. Output is the CLI's own output; JSON by default. Commands are never retried.\n", name)
		return nil
	}
	q, err := cloudCLIRequest(integration, args)
	if err != nil {
		return err
	}
	socket, err := control.DefaultSocketPath("")
	if err != nil {
		return err
	}
	response, err := control.CallWithTimeout(socket, control.Request{Type: "connections", ConnectionRequest: &q}, time.Minute)
	if err != nil {
		return err
	}
	if !response.OK {
		if response.Error == nil {
			return errors.New("cloud command failed")
		}
		return errors.New(response.Error.Message)
	}
	if response.Connections == nil || response.Connections.CLIOutput == nil {
		return errors.New("daemon returned no CLI output")
	}
	out := response.Connections.CLIOutput
	fmt.Fprint(os.Stdout, out.Stdout)
	fmt.Fprint(stderr, out.Stderr)
	if out.ExitCode != 0 {
		return fmt.Errorf("%s exited with status %d", integration, out.ExitCode)
	}
	return nil
}
