package calendar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// choiceRunner is a WorkRunner on a Claude and a Codex executor whose host is
// Claude, with a routing.md holding routing (no file when it is empty).
func choiceRunner(t *testing.T, routing string) (*WorkRunner, *scheduledActionLaunchRunner, *work.Store) {
	t.Helper()
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	launches := &scheduledActionLaunchRunner{}
	launcher := work.NewLauncher(launches, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude --configured"},
		"codex":  {Name: "codex", Command: "codex"},
	}))
	runner := &WorkRunner{Store: store, Launcher: launcher, Brain: &scheduledActionThreadRegistry{known: true, host: "claude"}}
	if routing != "" {
		runner.RoutingPath = filepath.Join(t.TempDir(), "routing.md")
		if err := os.WriteFile(runner.RoutingPath, []byte(routing), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return runner, launches, store
}

func runChoice(t *testing.T, runner *WorkRunner, item Item) error {
	t.Helper()
	item.ID, item.Title, item.ActionCwd = "item-1", "Scheduled action", "/calendar-cwd"
	_, err := runner.RunScheduledAction(context.Background(), item, Run{ID: "run-1", Title: "Frozen", SourceThreadID: "thread"})
	return err
}

const routingWithRow = `# Worker Routing

| Task | Executor | Model | Reasoning |
| --- | --- | --- | --- |
| Watch judgment | claude | sonnet | low |
| Scheduled action | codex | gpt-cheap | medium |
`

func TestScheduledActionStartsWithTheItemsOwnChoice(t *testing.T) {
	runner, launches, _ := choiceRunner(t, routingWithRow)
	err := runChoice(t, runner, Item{ActionExecutor: "claude", ActionModel: "claude-haiku", ActionReasoning: "low"})
	if err != nil {
		t.Fatal(err)
	}
	want := "claude --configured --model claude-haiku --effort low --permission-mode bypassPermissions"
	if len(launches.spawnCommands) != 1 || launches.spawnRoles[0] != "claude" || launches.spawnCommands[0] != want {
		t.Fatalf("spawn = %v %q, want claude %q (the routing row must not apply)", launches.spawnRoles, launches.spawnCommands, want)
	}
	if !strings.Contains(launches.sendReadyCalls[0], want+"|") {
		t.Fatalf("the prompt was sent to %q, not the launched command", launches.sendReadyCalls[0])
	}
}

func TestScheduledActionNamingOnlyAModelKeepsTheHostExecutor(t *testing.T) {
	runner, launches, _ := choiceRunner(t, routingWithRow)
	if err := runChoice(t, runner, Item{ActionModel: "claude-haiku"}); err != nil {
		t.Fatal(err)
	}
	if launches.spawnRoles[0] != "claude" || launches.spawnCommands[0] != "claude --configured --model claude-haiku --permission-mode bypassPermissions" {
		t.Fatalf("spawn = %v %q", launches.spawnRoles, launches.spawnCommands)
	}
}

func TestScheduledActionWithoutAChoiceUsesTheRoutingRow(t *testing.T) {
	runner, launches, _ := choiceRunner(t, routingWithRow)
	if err := runChoice(t, runner, Item{}); err != nil {
		t.Fatal(err)
	}
	want := "codex --model gpt-cheap -c 'model_reasoning_effort=\"medium\"' --dangerously-bypass-approvals-and-sandbox"
	if launches.spawnRoles[0] != "codex" || launches.spawnCommands[0] != want {
		t.Fatalf("spawn = %v %q, want codex %q", launches.spawnRoles, launches.spawnCommands, want)
	}
}

func TestScheduledActionRowDefaultCellsLeaveTheChoiceToTheExecutor(t *testing.T) {
	runner, launches, _ := choiceRunner(t, "| Scheduled action | claude | default | high |\n")
	if err := runChoice(t, runner, Item{}); err != nil {
		t.Fatal(err)
	}
	if launches.spawnCommands[0] != "claude --configured --effort high --permission-mode bypassPermissions" {
		t.Fatalf("spawn = %q", launches.spawnCommands)
	}
}

func TestScheduledActionWithNeitherChoiceNorRowUsesTheHostExecutor(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "routing.md")
	for name, routing := range map[string]string{
		"no file":        "",
		"no row":         "| Task | Executor | Model | Reasoning |\n| Work progress | codex | gpt-cheap | low |\n",
		"unreadable row": "| Scheduled action | codex | gpt-cheap |\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner, launches, _ := choiceRunner(t, routing)
			if routing == "" {
				runner.RoutingPath = missing
			}
			if err := runChoice(t, runner, Item{}); err != nil {
				t.Fatal(err)
			}
			if launches.spawnRoles[0] != "claude" || launches.spawnCommands[0] != "claude --configured --permission-mode bypassPermissions" {
				t.Fatalf("spawn = %v %q, want today's Host launch", launches.spawnRoles, launches.spawnCommands)
			}
		})
	}
}

func TestScheduledActionWithAnInvalidChoiceFailsTheRunAndLeavesNoWork(t *testing.T) {
	for name, test := range map[string]struct {
		item Item
		want string
	}{
		"unknown executor":    {Item{ActionExecutor: "nope"}, "unknown executor"},
		"bad reasoning":       {Item{ActionReasoning: "turbo"}, `unsupported claude reasoning level "turbo"`},
		"client without flag": {Item{ActionExecutor: "claude", ActionReasoning: "low", ActionModel: "bad\nmodel"}, "invalid model or reasoning value"},
	} {
		t.Run(name, func(t *testing.T) {
			runner, launches, store := choiceRunner(t, routingWithRow)
			err := runChoice(t, runner, test.item)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to say %q", err, test.want)
			}
			if len(launches.spawnCommands) != 0 {
				t.Fatalf("launched %q on a choice that cannot launch", launches.spawnCommands)
			}
			if _, ok := store.GetByID("run-1"); ok {
				t.Fatal("a Work item was left behind")
			}
		})
	}
}

func TestScheduledActionWhoseRoutingRowNamesAnUnknownExecutorFailsVisibly(t *testing.T) {
	runner, launches, _ := choiceRunner(t, "| Scheduled action | nope | m | low |\n")
	err := runChoice(t, runner, Item{})
	if err == nil || !strings.Contains(err.Error(), "unknown executor") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
	if len(launches.spawnCommands) != 0 {
		t.Fatalf("fell back to launching %q", launches.spawnCommands)
	}
}

func TestStoreRefusesAnInvalidActionChoiceAtCreateAndUpdate(t *testing.T) {
	runner, _, _ := choiceRunner(t, "")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.RefuseActionChoice(runner.CheckActionChoice)
	due := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	base := Item{Title: "x", Kind: KindScheduledAction, DueAt: &due, Timezone: "UTC", ActionInstruction: "go", SourceThreadID: "t"}

	bad := base
	bad.ActionReasoning = "turbo"
	if _, err := store.Create(bad); err == nil || !strings.Contains(err.Error(), `unsupported claude reasoning level "turbo"`) {
		t.Fatalf("create error = %v", err)
	}
	unknown := base
	unknown.ActionExecutor = "nope"
	if _, err := store.Create(unknown); !errors.Is(err, work.ErrUnknownExecutor) {
		t.Fatalf("create error = %v, want unknown executor", err)
	}

	good := base
	good.ActionModel, good.ActionReasoning = " claude-haiku ", "low"
	created, err := store.Create(good)
	if err != nil {
		t.Fatal(err)
	}
	if created.ActionModel != "claude-haiku" || created.ActionReasoning != "low" {
		t.Fatalf("stored choice = %q %q", created.ActionModel, created.ActionReasoning)
	}
	created.ActionReasoning = "turbo"
	if _, err := store.Update(created, created.Revision); err == nil || !strings.Contains(err.Error(), "turbo") {
		t.Fatalf("update error = %v", err)
	}
	fetched, err := store.Get(created.ID)
	if err != nil || fetched.ActionReasoning != "low" || fetched.Revision != created.Revision {
		t.Fatalf("a refused update changed the item: %+v %v", fetched, err)
	}
	created.ActionReasoning = ""
	if updated, err := store.Update(created, created.Revision); err != nil || updated.ActionReasoning != "" || updated.ActionModel != "claude-haiku" {
		t.Fatalf("update = %+v %v", updated, err)
	}
}

func TestOnlyAScheduledActionTakesAnActionChoice(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	_, err = store.Create(Item{Title: "x", Kind: KindReminder, NotifyAt: &at, Timezone: "UTC", ActionModel: "claude-haiku"})
	if err == nil || !strings.Contains(err.Error(), "only a scheduled_action") {
		t.Fatalf("error = %v", err)
	}
}
