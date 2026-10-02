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

	"github.com/daoleno/zen/daemon/connections"
	"github.com/daoleno/zen/daemon/control"
	"golang.org/x/term"
)

func runConnectionsCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, `Usage: zen connections <list|add|import-gh|oauth-configure|oauth-start|get|refresh|search|describe|invoke|enable|disable|disconnect|policy> [flags]
Plugins are shared by Brain and Workers on this daemon.
Search: --query text. Describe/invoke: --id account --tool name.
Invoke: --args '{"query":{...}}' or --args-file path. One bounded call, no implicit pagination.
Add: --integration github|notion|google|slack|linear|mcp|openapi --name account [--endpoint https://...] [--spec-file path].
Operator-owned OAuth override (not end-user installation): oauth-configure --integration github|slack|google|notion|linear|mcp --oauth-config-file private.json; oauth-start --integration ... --name account opens a system-browser URL.
Custom internal endpoints: --trust-networks 127.0.0.1/32 (explicit, per account).
Credentials are read from a hidden terminal prompt; --credential-stdin supports a secure pipe.
import-gh explicitly copies the current github.com gh login into this daemon's private vault.
policy --id account --tool name --allow grants future calls to that tool; only change on explicit user consent.
Never send messages or make updates without task-specific user authorization, even when a tool is enabled.`)
		return flag.ErrHelp
	}
	action := args[0]
	fs := flag.NewFlagSet("zen connections "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{}
	q := connections.Request{Action: action}
	in := connections.Input{}
	var argsJSON, argsFile, specFile, oauthFile, trustRanges string
	var stdinSecret bool
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.BoolVar(&cfg.json, "json", true, "print JSON")
	fs.StringVar(&q.ID, "id", "", "account ID")
	fs.StringVar(&q.Query, "query", "", "search text")
	fs.StringVar(&q.Tool, "tool", "", "tool name")
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
	case "list", "add", "import-gh", "oauth-configure", "oauth-start", "get", "refresh", "search", "describe", "invoke", "enable", "disable", "disconnect", "policy":
	default:
		return errors.New("unknown plugin command")
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
	response, err := callControl(cfg, control.Request{Type: "connections", ConnectionRequest: &q})
	in.Credential = ""
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, response, true)
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
