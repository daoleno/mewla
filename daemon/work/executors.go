package work

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrUnknownExecutor is returned when a delegated selection names an executor
// that is not present in the loaded catalog.
var ErrUnknownExecutor = errors.New("unknown executor")

// ExecutorConfig is the static launch catalog parsed from executors.toml plus
// built-in defaults. It holds no Worker routing: Brain chooses each delegated
// Worker's executor, model and reasoning per launch from its routing guide.
type ExecutorConfig struct {
	ByName map[string]Executor
	// DeprecatedKeys lists retired top-level keys (delegated_executor,
	// delegated_model, delegated_reasoning) found in the file. They are ignored.
	DeprecatedKeys []string
}

// NewExecutorConfig builds an in-memory ExecutorConfig.
func NewExecutorConfig(byName map[string]Executor) *ExecutorConfig {
	if byName == nil {
		byName = map[string]Executor{}
	}
	return &ExecutorConfig{ByName: byName}
}

// Roles returns executor names sorted alphabetically.
func (c *ExecutorConfig) Roles() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.ByName))
	for name := range c.ByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type executorFile struct {
	Executors []Executor `toml:"executors"`
}

var retiredExecutorKeys = []string{"delegated_executor", "delegated_model", "delegated_reasoning"}

// LoadExecutors reads path, or returns built-in defaults when missing.
func LoadExecutors(path string) (*ExecutorConfig, error) {
	cfg := &ExecutorConfig{
		ByName: map[string]Executor{
			"agent":    {Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
			"claude":   {Name: "claude", Command: "claude"},
			"codex":    {Name: "codex", Command: "codex"},
			"grok":     {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
			"opencode": {Name: "opencode", Command: "opencode", Kind: "opencode"},
			"pi":       {Name: "pi", Command: "pi", Kind: "pi"},
			"dsh":      {Name: "dsh", Command: "dsh", Kind: "dsh"},
		},
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var file executorFile
	meta, err := toml.Decode(string(raw), &file)
	if err != nil {
		return nil, err
	}
	for _, key := range retiredExecutorKeys {
		if meta.IsDefined(key) {
			cfg.DeprecatedKeys = append(cfg.DeprecatedKeys, key)
		}
	}
	for _, executor := range file.Executors {
		name := strings.TrimSpace(executor.Name)
		if name == "" {
			continue
		}
		executor.Name = name
		executor.Command = strings.TrimSpace(executor.Command)
		if executor.Command == "" {
			executor.Command = name
		}
		cfg.ByName[name] = executor
	}
	return cfg, nil
}
