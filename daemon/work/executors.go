package work

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/daoleno/mewla/daemon/atomicfile"
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

// retiredRuntimeLine is an executor's `runtime = "..."` line. Every Session
// runs in the session host, so the key names nothing any more.
var retiredRuntimeLine = regexp.MustCompile(`^[ \t]*runtime[ \t]*=[ \t]*("[^"]*"|'[^']*')[ \t]*(#.*)?\r?$`)

// executorsDefine reports whether any [[executors]] entry defines key.
func executorsDefine(meta toml.MetaData, key string) bool {
	for _, k := range meta.Keys() {
		if len(k) == 2 && k[0] == "executors" && k[1] == key {
			return true
		}
	}
	return false
}

// dropRetiredRuntime rewrites executors.toml without its runtime lines,
// keeping everything else as it was. The file loads the same either way; a
// rewrite that fails is only logged.
func dropRetiredRuntime(path string, raw []byte) {
	lines := strings.SplitAfter(string(raw), "\n")
	kept := lines[:0]
	for _, line := range lines {
		if retiredRuntimeLine.MatchString(strings.TrimSuffix(line, "\n")) {
			continue
		}
		kept = append(kept, line)
	}
	rewritten := strings.Join(kept, "")
	var check executorFile
	if meta, err := toml.Decode(rewritten, &check); err != nil || executorsDefine(meta, "runtime") {
		return
	}
	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	if err := atomicfile.Write(path, []byte(rewritten), perm); err != nil {
		log.Printf("executors.toml: drop the retired runtime key: %v", err)
	}
}

// LoadExecutors reads path, or returns built-in defaults when missing. An
// executor's retired runtime key is dropped from the file.
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
	if executorsDefine(meta, "runtime") {
		dropRetiredRuntime(path, raw)
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
		// A command an earlier release wrote in Auto mode runs in full authority.
		if NewWorkerExecutor(name, executor).Provider == WorkerProviderClaude {
			executor.Command = MigrateClaudeAutoMode(executor.Command)
		}
		cfg.ByName[name] = executor
	}
	return cfg, nil
}

// IDForCommand names the executor a launch command belongs to: the one whose
// configured command it is, or else the single executor that starts the same
// program (an app preset may launch "claude --model m" for the "claude"
// executor). It returns "" when no executor claims the command, and the caller
// then reports no executor rather than guessing one.
func (c *ExecutorConfig) IDForCommand(command string) string {
	if c == nil {
		return ""
	}
	command = strings.TrimSpace(command)
	program := commandProgram(command)
	if command == "" || program == "" {
		return ""
	}
	best := ""
	bestFields := 0
	for _, name := range c.Roles() {
		worker := NewWorkerExecutor(name, c.ByName[name])
		configured := strings.TrimSpace(worker.Command)
		if configured == command {
			return name
		}
		if worker.ConfigDir != "" {
			// A separate sign-in is recognized by its own config directory, and
			// never claimed by a launch that does not carry it.
			prefix := strings.TrimSuffix(configured, worker.BareCommand)
			if strings.HasPrefix(command, prefix) {
				return name
			}
			continue
		}
		if commandProgram(configured) != program {
			continue
		}
		// Several executors may start one program with different flags; the
		// plainest launch is the program's own executor.
		if fields := len(strings.Fields(configured)); best == "" || fields < bestFields {
			best, bestFields = name, fields
		}
	}
	return best
}

func commandProgram(command string) string {
	fields := strings.Fields(command)
	// "env NAME=value program" runs program.
	if len(fields) > 0 && fields[0] == "env" {
		fields = fields[1:]
		for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "-") {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}
