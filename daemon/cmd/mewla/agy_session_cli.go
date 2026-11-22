package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/daoleno/mewla/daemon/work"
)

type repeatedFlag []string

func (r *repeatedFlag) String() string     { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

// runAntigravitySessionCLI is `mewla agy-session`: the program an Antigravity
// Session Mewla starts runs in its terminal (work/antigravity_session.go).
func runAntigravitySessionCLI(args []string) error {
	flags := flag.NewFlagSet("agy-session", flag.ContinueOnError)
	id := flags.String("agy-session", "", "the Session's identity")
	stateDir := flags.String("state-dir", "", "Mewla's state directory")
	conversation := flags.String("conversation", "", "the agy conversation to continue")
	model := flags.String("model", "", "agy model id")
	effort := flags.String("effort", "", "thinking level")
	mode := flags.String("mode", "", "agy mode")
	skip := flags.Bool("dangerously-skip-permissions", false, "agy runs tools without asking")
	mcpConfig := flags.String("mcp-config", "", "MCP servers for this session, as JSON")
	var addDirs repeatedFlag
	flags.Var(&addDirs, "add-dir", "another folder agy may use")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	servers, err := work.ParseACPMCPConfig(*mcpConfig)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	return work.RunAntigravitySession(ctx, work.AntigravitySessionOptions{
		ID: *id, Cwd: cwd, StateDir: *stateDir, Conversation: *conversation,
		Model: *model, Effort: *effort, Mode: *mode, SkipPermissions: *skip,
		MCPServers: servers, AddDirs: addDirs, Supervised: os.Getenv("MEWLA_WORKER_DELEGATED") == "1",
	})
}
