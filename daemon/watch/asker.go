package watch

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// Asker lends Watch's way of asking a model to another judgment: the same
// print-mode run of the person's own agent CLI in an empty directory with no
// tools, hooks, MCP servers or saved session, the same schema-checked JSON
// answer, and the executor and model picked by its own routing.md row (for
// example "| Work progress | claude | sonnet | low |"). Without a valid row it
// tries the Brain host's executor, then the last that worked, then the rest.
type Asker struct {
	judge *judge
}

// AskerOptions configures an Asker.
type AskerOptions struct {
	// Task is the routing.md row's Task cell prefix, such as "Work progress".
	Task string
	// RoutingPath is routing.md in the Brain workspace; empty means automatic.
	RoutingPath string
	// Workdir is the empty directory the CLI runs in.
	Workdir string
	// Preferred names the executor to try first (the Brain host's).
	Preferred func() string
	// BudgetUSD caps one Claude run ("0.5"); empty keeps Watch's cap.
	BudgetUSD string
	// Effort is Claude's reasoning when the row names none ("low").
	Effort string
}

// NewAsker returns an Asker for one judgment.
func NewAsker(options AskerOptions) *Asker {
	return &Asker{judge: &judge{
		workdir:     options.Workdir,
		now:         func() time.Time { return time.Now().UTC() },
		preferred:   options.Preferred,
		lookPath:    exec.LookPath,
		routingPath: options.RoutingPath,
		routingTask: options.Task,
		budgetUSD:   options.BudgetUSD,
		effort:      options.Effort,
	}}
}

// Ask runs one judgment and returns its structured answer.
func (a *Asker) Ask(ctx context.Context, system, prompt, schema string) (json.RawMessage, Usage, error) {
	return a.judge.runAgents(ctx, system, prompt, schema)
}

// Expected names the executor and model the next Ask would use first, and
// where that choice came from.
func (a *Asker) Expected() (executor, model string, routing Routing) {
	executor, model = a.judge.expected()
	_, routing = a.judge.routed()
	return executor, model, routing
}
