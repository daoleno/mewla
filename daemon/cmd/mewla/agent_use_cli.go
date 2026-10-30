package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/control"
)

const agentUseUsage = `Usage: mewla agent-use <status|on|off|fix|key> [flags]

Let agents on this computer use it the way you do: your Chrome, with the
sites you are logged in to, in a tab group of their own. It is on by default
and needs no setup beyond adding the Playwright extension to Chrome and
giving Mewla its key once.

  status          What agents can use now, and the one thing to fix if not
  on | off        The same switch as the app's "Agents can use this computer"
  fix             Do the fix status names (opens Chrome on this computer)
  key [-clear]    Paste the Playwright extension's key (shown on its page in
                  Chrome) from stdin; -clear forgets it

The app shows the same status: Settings → Agents on this computer.
`

func runAgentUseCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, agentUseUsage)
		return flag.ErrHelp
	}
	action := args[0]
	if action == "computer-mcp" {
		return runComputerMCP(args[1:], stderr)
	}
	if action == "phone-mcp" {
		return runPhoneMCP(args[1:], stderr)
	}
	fs := flag.NewFlagSet("mewla agent-use "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	var outputJSON, clear bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	if action == "key" {
		fs.BoolVar(&clear, "clear", false, "forget the pasted key")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	service, err := agentuse.New(resolveCLIStateDir(stateDir), false)
	if err != nil {
		return err
	}
	var status agentuse.Status
	switch action {
	case "status":
		status = service.Refresh()
	case "on", "off":
		status, err = service.SetEnabled(action == "on")
	case "fix":
		current := service.Refresh()
		if current.Browser.Fix == "" {
			status = current
			break
		}
		status, err = service.Fix(current.Browser.Fix)
	case "key":
		key := ""
		if !clear {
			line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return readErr
			}
			key = strings.TrimSpace(line)
			if key == "" {
				return errors.New("paste the key on stdin, or use -clear")
			}
		}
		status, err = service.SetKey(key)
	case "chrome-mcp":
		// What agents run: the user's Chrome as an MCP server on stdio. A
		// detached helper runs Playwright and closes the agent's tabs when it
		// is done, however this process ends.
		if _, _, mcpErr := service.MCPServer(); mcpErr != nil {
			return fmt.Errorf("your Chrome isn't available to agents: %w", mcpErr)
		}
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return exeErr
		}
		helper := []string{exe, "agent-use", "chrome-mcp-supervise"}
		if stateDir != "" {
			helper = append(helper, "--state-dir", stateDir)
		}
		return agentuse.RelayMCP(helper, os.Environ(), os.Stdin, os.Stdout, os.Stderr)
	case "chrome-mcp-supervise":
		argv, env, mcpErr := service.MCPServer()
		if mcpErr != nil {
			return fmt.Errorf("your Chrome isn't available to agents: %w", mcpErr)
		}
		return agentuse.SuperviseMCP(argv, env, os.Stdin, os.Stdout, os.Stderr)
	default:
		return fmt.Errorf("unknown agent-use action %q", action)
	}
	if err != nil {
		return err
	}
	status = daemonAgentUse(resolveCLIStateDir(stateDir), status)
	if outputJSON {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	printAgentUse(os.Stdout, status)
	return nil
}

// daemonAgentUse is the running daemon's status, which a command's own look
// can't replace: only the daemon fetches the browser tools, and only it knows
// the screen. Without the daemon, a status that waits on it says so.
func daemonAgentUse(stateDir string, local agentuse.Status) agentuse.Status {
	resp, err := callControl(cliConfig{stateDir: stateDir}, control.Request{Type: "agent_use"})
	if err == nil && resp.OK && resp.AgentUse != nil {
		return *resp.AgentUse
	}
	if errors.Is(err, errMewlaNotRunning) && local.Browser.State == agentuse.BrowserPreparing {
		local.Browser.Summary = "Mewla fetches the browser tools. " + err.Error()
	}
	return local
}

// printPhone says whether Phone use is on, which only the person turns on (Settings → Labs).
func printPhone(w io.Writer, status agentuse.Status) {
	phone := status.Phone
	if phone == nil || !phone.Enabled {
		return
	}
	line := "Your phone: Phone use is on; "
	switch {
	case !phone.Connected:
		line += "the phone is not connected (open Mewla on it)"
	case phone.State == "ready":
		line += "connected (" + phone.Model + "); Workers here have mewla_phone"
	default:
		line += "connected (" + phone.Model + "), " + phone.State
	}
	fmt.Fprintln(w, line)
	if phone.Active != nil {
		name := phone.Active.Name
		if name == "" {
			name = phone.Active.Worker
		}
		fmt.Fprintf(w, "  %s is using it now.\n", name)
	}
}

func printAgentUse(w io.Writer, status agentuse.Status) {
	if !status.Enabled {
		fmt.Fprintln(w, "Agents can use this computer: off")
		printPhone(w, status)
		return
	}
	fmt.Fprintln(w, "Agents can use this computer: on")
	browser := status.Browser
	line := "Your Chrome: " + browser.Summary
	if browser.Profile != "" && browser.State == agentuse.BrowserReady {
		line += " (profile " + browser.Profile + ")"
	}
	fmt.Fprintln(w, line)
	if computer := status.Computer; computer != nil {
		fmt.Fprintln(w, "This computer's screen: "+computer.Summary)
		if computer.Active != nil {
			name := computer.Active.Name
			if name == "" {
				name = computer.Active.Worker
			}
			fmt.Fprintf(w, "  %s is using it now.\n", name)
		}
	}
	printPhone(w, status)
	if status.CodexComputerUse {
		fmt.Fprintln(w, "Codex's own computer use: Codex Workers here have it. Mewla can't watch or pause it.")
	}
	switch browser.Fix {
	case agentuse.FixInstallExtension:
		fmt.Fprintln(w, "  mewla agent-use fix   opens the extension's page in Chrome; click Add to Chrome.")
	case agentuse.FixOpenExtension:
		fmt.Fprintln(w, "  mewla agent-use fix   opens the extension in Chrome, which shows its key;")
		fmt.Fprintln(w, "  mewla agent-use key   then reads that key from stdin (paste it, press Enter).")
	case agentuse.FixRetry:
		fmt.Fprintln(w, "  mewla agent-use fix   tries the download again.")
	}
}
