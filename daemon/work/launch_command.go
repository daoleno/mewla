package work

import (
	"fmt"
	"slices"
	"strings"
)

// launchModelFlags is one client's verified per-launch selection surface.
// Empty reasoning means the CLI has no reasoning flag; empty levels skips
// local validation.
type launchModelFlags struct {
	model     []string // accepted spellings; the first is emitted
	reasoning []string
	levels    []string
}

// Checked against each CLI's --help: claude --model/--effort, pi
// --model/--thinking, grok -m/--model, cursor-agent --model, opencode -m/--model.
// Codex has its own parser in codex_worker_command.go.
var launchModelFlagsByProvider = map[string]launchModelFlags{
	WorkerProviderClaude:   {model: []string{"--model"}, reasoning: []string{"--effort"}, levels: []string{"low", "medium", "high", "xhigh", "max"}},
	WorkerProviderPi:       {model: []string{"--model"}, reasoning: []string{"--thinking"}, levels: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}},
	WorkerProviderGrok:     {model: []string{"--model", "-m"}},
	WorkerProviderCursor:   {model: []string{"--model"}},
	WorkerProviderOpenCode: {model: []string{"--model", "-m"}},
}

// ResolveWorkerCommand resolves one launch command from the catalog or an
// explicit command. Delegated launches apply the provider's autonomy contract
// and the per-launch model and reasoning Brain chose; an option the client
// cannot honor is an error, never silently dropped.
func (c *ExecutorConfig) ResolveWorkerCommand(executor, explicitCommand, model, reasoning string, delegated bool) (string, error) {
	executor = strings.TrimSpace(executor)
	command := strings.TrimSpace(explicitCommand)
	model, reasoning = strings.TrimSpace(model), strings.TrimSpace(reasoning)
	var provider string
	if command != "" {
		provider = InferWorkerProvider(command)
	} else {
		if executor == "" {
			return "", fmt.Errorf("%w: executor is required", ErrUnknownExecutor)
		}
		var e Executor
		ok := false
		if c != nil {
			e, ok = c.ByName[executor]
		}
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrUnknownExecutor, executor)
		}
		worker := NewWorkerExecutor(executor, e)
		command, provider = worker.Command, worker.Provider
	}
	if !delegated {
		return command, nil
	}
	if provider == WorkerProviderCodex {
		return codexWorkerCommand(command, model, reasoning, true)
	}
	command, err := withLaunchModel(provider, command, model, reasoning)
	if err != nil {
		return "", err
	}
	return PrepareDelegatedCommand(provider, command)
}

// withLaunchModel replaces any model/reasoning options already in command with
// the requested ones, so a catalog command never carries two values.
func withLaunchModel(provider, command, model, reasoning string) (string, error) {
	if model == "" && reasoning == "" {
		return command, nil
	}
	if strings.ContainsAny(model+reasoning, "\x00\r\n") {
		return "", fmt.Errorf("invalid model or reasoning value")
	}
	flags, ok := launchModelFlagsByProvider[provider]
	if !ok {
		return "", fmt.Errorf("client %q has no per-launch model/reasoning option; omit -model and -reasoning to use its native selection", firstNonEmpty(provider, WorkerProviderCustom))
	}
	if reasoning != "" {
		if len(flags.reasoning) == 0 {
			return "", fmt.Errorf("client %q has no per-launch reasoning option; omit -reasoning", provider)
		}
		if len(flags.levels) > 0 && !slices.Contains(flags.levels, reasoning) {
			return "", fmt.Errorf("unsupported %s reasoning level %q (accepted: %s)", provider, reasoning, strings.Join(flags.levels, ", "))
		}
	}
	if provider == WorkerProviderPi && reasoning != "" {
		if _, level, found := strings.Cut(model, ":"); found && slices.Contains(flags.levels, level) {
			return "", fmt.Errorf("pi model %q already carries a thinking level; pass the level only with -reasoning", model)
		}
	}
	fields, ok := splitSupportedLaunchFields(command)
	options, inspectable := inspectLaunchCommandOptions(command)
	if !ok || !inspectable || options.terminated {
		return "", fmt.Errorf("per-launch model/reasoning requires a direct %s command without shell composition or -- terminator", provider)
	}
	args := fields[:len(fields)-len(options.argv)]
	args = append([]string{}, args...)
	var drop []string
	if model != "" {
		drop = append(drop, flags.model...)
	}
	if reasoning != "" {
		drop = append(drop, flags.reasoning...)
	}
	for i := 0; i < len(options.argv); i++ {
		arg := options.argv[i]
		key, _, equals := strings.Cut(arg, "=")
		if slices.Contains(drop, key) {
			if !equals {
				i++
			}
			continue
		}
		args = append(args, arg)
	}
	if model != "" {
		args = append(args, flags.model[0], model)
	}
	if reasoning != "" {
		args = append(args, flags.reasoning[0], reasoning)
	}
	return joinLaunchTokens(args), nil
}
