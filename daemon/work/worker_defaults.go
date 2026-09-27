package work

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// WorkerDefaults is owned by ExecutorConfig and applies only to future
// delegated launches. Brain and ordinary interactive Sessions keep their own
// configuration. Command is the adapter's resolved output, never a second store.
type WorkerDefaults struct {
	Executor  string `json:"executor"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
	Command   string `json:"command"`
}

func (c *ExecutorConfig) WorkerDefaults() (WorkerDefaults, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d := WorkerDefaults{Executor: c.effectiveDelegatedLocked()}
	if d.Executor == c.delegatedExecutor {
		d.Model, d.Reasoning = c.delegatedModel, c.delegatedReasoning
	}
	command, err := c.resolveWorkerCommandLocked(d.Executor, "", "", "", true)
	d.Command = command
	return d, err
}

// Save all three values before publishing any of them. Spawn reads the same
// lock, so a failed persistence cannot partially change live configuration.
func (c *ExecutorConfig) SetWorkerDefaults(d WorkerDefaults) (WorkerDefaults, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d.Executor, d.Model, d.Reasoning = strings.TrimSpace(d.Executor), strings.TrimSpace(d.Model), strings.TrimSpace(d.Reasoning)
	e, ok := c.ByName[d.Executor]
	if !ok {
		return WorkerDefaults{}, fmt.Errorf("%w: %s", ErrUnknownExecutor, d.Executor)
	}
	if lock, active := c.activeEnvLockLocked(); active && lock != d.Executor {
		return WorkerDefaults{}, fmt.Errorf("%w: %s", ErrDelegatedExecutorLocked, lock)
	}
	provider := NewWorkerExecutor(d.Executor, e).Provider
	if d.Executor != provider {
		return WorkerDefaults{}, fmt.Errorf("use the client executor name %q", provider)
	}
	if provider != WorkerProviderCodex && (d.Model != "" || d.Reasoning != "") {
		return WorkerDefaults{}, fmt.Errorf("model/reasoning defaults are supported by the codex adapter only")
	}
	var command string
	var err error
	if provider == WorkerProviderCodex {
		command, err = codexWorkerCommand(e.Command, d.Model, d.Reasoning, true)
	} else {
		command, err = PrepareDelegatedCommand(provider, e.Command)
	}
	if err != nil {
		return WorkerDefaults{}, err
	}
	d.Command = command
	if d.Executor == c.delegatedExecutor && d.Model == c.delegatedModel && d.Reasoning == c.delegatedReasoning {
		return d, nil
	}
	if err := c.persistWorkerDefaultsLocked(d); err != nil {
		return WorkerDefaults{}, err
	}
	c.delegatedExecutor, c.delegatedModel, c.delegatedReasoning = d.Executor, d.Model, d.Reasoning
	return d, nil
}

func (c *ExecutorConfig) persistWorkerDefaultsLocked(d WorkerDefaults) error {
	if c.path == "" {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	raw = rewriteDelegatedExecutorTOML(raw, d.Executor)
	prefix, rest := splitTopLevelPrefix(raw)
	for _, field := range []struct{ key, value string }{{"delegated_model", d.Model}, {"delegated_reasoning", d.Reasoning}} {
		re := regexp.MustCompile(`(?m)^[ \t]*` + field.key + `[ \t]*=[^\r\n]*(?:\r?\n|$)`)
		line := field.key + ` = "` + escapeTOMLString(field.value) + "\"\n"
		if re.Match(prefix) {
			prefix = re.ReplaceAllFunc(prefix, func([]byte) []byte { return []byte(line) })
		} else if field.value != "" {
			prefix = append([]byte(line), prefix...)
		}
	}
	return atomicWriteFile(c.path, append(prefix, rest...), 0600)
}

func (c *ExecutorConfig) ResolveWorkerCommand(executor, explicitCommand, model, reasoning string, delegated bool) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resolveWorkerCommandLocked(executor, explicitCommand, model, reasoning, delegated)
}

func (c *ExecutorConfig) resolveWorkerCommandLocked(executor, explicitCommand, model, reasoning string, delegated bool) (string, error) {
	executor = strings.TrimSpace(executor)
	if executor == "" {
		executor = c.effectiveDelegatedLocked()
	}
	e, ok := c.ByName[executor]
	if !ok && strings.TrimSpace(explicitCommand) == "" {
		return "", fmt.Errorf("%w: %s", ErrUnknownExecutor, executor)
	}
	command := strings.TrimSpace(explicitCommand)
	explicit := command != ""
	provider := NewWorkerExecutor(executor, e).Provider
	if explicit {
		provider = InferWorkerProvider(command)
	} else {
		command = e.Command
	}
	if !delegated {
		return command, nil
	}

	dm, dr := "", ""
	if executor == c.delegatedExecutor && provider == NewWorkerExecutor(c.delegatedExecutor, c.ByName[c.delegatedExecutor]).Provider {
		dm, dr = c.delegatedModel, c.delegatedReasoning
	}
	if provider == WorkerProviderCodex {
		if explicit {
			_, cm, cr, err := codexCommandParts(command, false)
			if err != nil {
				return "", err
			}
			if cm != "" {
				dm = cm
			}
			if cr != "" {
				dr = cr
			}
		}
		if model != "" {
			dm = model
		}
		if reasoning != "" {
			dr = reasoning
		}
		return codexWorkerCommand(command, dm, dr, true)
	}
	if model != "" || reasoning != "" {
		return "", fmt.Errorf("model/reasoning overrides unsupported for client %q", provider)
	}
	return PrepareDelegatedCommand(provider, command)
}
