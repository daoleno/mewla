package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/control"
	"golang.org/x/term"
)

func runConnectionsCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, `Usage: mewla connections <list|add|import-gh|oauth-configure|oauth-start|get|refresh|search|describe|invoke|resources|read-resource|updates|confirm-updates|dismiss-updates|enable|disable|disconnect|policy> [flags]
Plugins are shared by Brain and Workers on this daemon.
Search: --query text. Describe/invoke: --id account --tool name.
Invoke: --args '{"query":{...}}' or --args-file path. One bounded call, no implicit pagination, never retried.
A call that times out may already have run; check its state with a read tool before calling it again.
MCP resources: resources --id account [--cursor c] lists documents and URI templates; read-resource --id account --uri onlora://projects/<id> reads one.
Updated tools: when a service adds or changes tools, unchanged tools keep working and the rest wait for the person.
updates --id account asks the service now and shows them; confirm-updates|dismiss-updates --id account --tools a,b on explicit user consent.
A call blocked by an update names the confirm-updates command that clears every update waiting on its account.
Add: --integration github|notion|google|slack|linear|mcp|openapi --name account [--endpoint https://...] [--spec-file path].
Cloudflare (connected in Extensions) covers the whole Cloudflare API: find the endpoint with its search tool, then call it with execute; docs searches Cloudflare's docs.
Deleting Cloudflare resources, changing DNS, or creating or revoking tokens needs the user's authorization for that task.
Operator-owned OAuth override (not end-user installation): oauth-configure --integration github|slack|google|notion|linear|mcp --oauth-config-file private.json; oauth-start --integration ... --name account opens a system-browser URL.
Custom internal endpoints: --trust-networks 127.0.0.1/32 (explicit, per account).
Credentials are read from a hidden terminal prompt; --credential-stdin supports a secure pipe.
import-gh explicitly copies the current github.com gh login into this daemon's private vault.
policy --id account --tool name --allow grants future calls to that tool; only change on explicit user consent.
policy --id account --tools a,b,c [--allow] sets every listed tool at once, all or none; same explicit consent.
Never send messages or make updates without task-specific user authorization, even when a tool is enabled.`)
		return flag.ErrHelp
	}
	action := args[0]
	fs := flag.NewFlagSet("mewla connections "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{}
	q := connections.Request{Action: action}
	in := connections.Input{}
	var argsJSON, argsFile, specFile, oauthFile, trustRanges, toolList string
	var stdinSecret bool
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.BoolVar(&cfg.json, "json", true, "print JSON")
	fs.StringVar(&q.ID, "id", "", "account ID")
	fs.StringVar(&q.Query, "query", "", "search text")
	fs.StringVar(&q.Tool, "tool", "", "tool name")
	fs.StringVar(&q.URI, "uri", "", "read-resource: resource URI")
	fs.StringVar(&q.Cursor, "cursor", "", "resources: continue a listing")
	fs.StringVar(&toolList, "tools", "", "policy: tool names, comma separated")
	fs.BoolVar(&q.Allowed, "allow", false, "allow future calls (explicit user consent required)")
	fs.StringVar(&in.Integration, "integration", "", "plugin ID")
	fs.StringVar(&in.Name, "name", "", "account name")
	fs.StringVar(&in.Endpoint, "endpoint", "", "remote HTTPS endpoint")
	fs.StringVar(&argsJSON, "args", "{}", "tool arguments JSON")
	fs.StringVar(&argsFile, "args-file", "", "tool arguments file")
	fs.StringVar(&specFile, "spec-file", "", "OpenAPI JSON document")
	fs.StringVar(&oauthFile, "oauth-config-file", "", "private JSON file: client_id, client_secret, redirect_url")
	fs.StringVar(&trustRanges, "trust-networks", "", "explicit internal CIDRs for this custom account, comma separated")
	fs.BoolVar(&in.AllowWrites, "allow-writes", false, "request OAuth write scopes (tools still need explicit grants)")
	fs.BoolVar(&stdinSecret, "credential-stdin", false, "read credential from stdin instead of hidden prompt")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	switch action {
	case "list", "add", "import-gh", "oauth-configure", "oauth-start", "get", "refresh", "search", "describe", "invoke", "enable", "disable", "disconnect", "policy", "resources", "updates":
	case "read-resource", "confirm-updates", "dismiss-updates":
		q.Action = strings.ReplaceAll(action, "-", "_")
	default:
		return errors.New("unknown plugin command")
	}
	if toolList != "" {
		for _, name := range strings.Split(toolList, ",") {
			if name = strings.TrimSpace(name); name != "" {
				q.Tools = append(q.Tools, name)
			}
		}
	}
	if trustRanges != "" {
		for _, cidr := range strings.Split(trustRanges, ",") {
			in.TrustedNetworks = append(in.TrustedNetworks, strings.TrimSpace(cidr))
		}
	}
	if action == "oauth-configure" || action == "oauth-start" {
		q.Action = strings.ReplaceAll(action, "-", "_")
		q.Input = &in
		if action == "oauth-configure" {
			raw, err := readBoundedFile(oauthFile, 32<<10)
			if err != nil {
				return err
			}
			if json.Unmarshal(raw, &in.OAuthClient) != nil || in.OAuthClient == nil {
				return errors.New("invalid OAuth client JSON")
			}
		}
	}
	if action == "invoke" {
		q.Arguments = json.RawMessage(argsJSON)
		if argsFile != "" {
			raw, err := readBoundedFile(argsFile, connections.MaxInputBytes)
			if err != nil {
				return err
			}
			q.Arguments = raw
		}
		if !json.Valid(q.Arguments) {
			return errors.New("arguments must be JSON")
		}
	}
	if action == "add" || action == "import-gh" {
		if action == "import-gh" {
			in.Integration = "github"
			if in.Name == "" {
				in.Name = "GitHub"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// gh is the explicit supported credential API, never arbitrary file scraping.
			token, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
			if err != nil {
				return errors.New("GitHub CLI login unavailable; run gh auth login first")
			}
			in.Credential = strings.TrimSpace(string(token))
		} else {
			if stdinSecret {
				raw, err := io.ReadAll(io.LimitReader(os.Stdin, 16385))
				if err != nil || len(raw) > 16384 {
					return errors.New("credential could not be read")
				}
				in.Credential = strings.TrimSpace(string(raw))
			} else {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return errors.New("use --credential-stdin for a secure pipe (empty input for a public endpoint)")
				}
				fmt.Fprint(stderr, "Account token (empty for public endpoint): ")
				raw, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(stderr)
				if err != nil {
					return errors.New("credential could not be read")
				}
				in.Credential = strings.TrimSpace(string(raw))
			}
		}
		if specFile != "" {
			raw, err := readBoundedFile(specFile, 1<<20)
			if err != nil {
				return err
			}
			in.Spec = raw
		}
		q.Action = "add"
		q.Input = &in
	}
	timeout := 30 * time.Second
	if action == "invoke" {
		// A remote tool that waits for its work can take minutes; the daemon
		// bounds each call itself.
		timeout = connections.MaxCallTimeout + time.Minute
	}
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return err
	}
	response, err := control.CallWithTimeout(socketPath, control.Request{Type: "connections", ConnectionRequest: &q}, timeout)
	in.Credential = ""
	if err != nil {
		return controlError(cfg.stateDir, err)
	}
	if action == "updates" {
		response = pendingUpdates(response)
	}
	return writeControlResponse(os.Stdout, response, true)
}

// pendingUpdates keeps only an account's tools that wait for confirmation.
func pendingUpdates(response control.Response) control.Response {
	if response.Connections == nil || response.Connections.Account == nil {
		return response
	}
	account := *response.Connections.Account
	tools := []connections.Tool{}
	for _, tool := range account.Tools {
		if tool.Updated {
			tools = append(tools, tool)
		}
	}
	account.Tools, account.History, account.Access = tools, nil, nil
	response.Connections = &connections.Response{Account: &account}
	return response
}
func readBoundedFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("input file could not be opened")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if err != nil || len(raw) > limit {
		return nil, errors.New("input file exceeds size limit")
	}
	return raw, nil
}
