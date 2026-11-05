package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionRunner is the tmux side of starting Calendar-owned Work.
type SessionRunner interface {
	Spawn(role, cwd, command string) (string, error)
	SendWhenReady(sessionID, command, text string) error
	Abort(sessionID string) error
}

var (
	ErrAlreadyStarted        = errors.New("work item already started")
	ErrExecutorNotConfigured = errors.New("executor not configured")
	ErrSpawnFailed           = errors.New("spawn failed")
)

// Launcher starts Calendar-owned Work in a fresh delegated executor session.
type Launcher struct {
	run   SessionRunner
	execs *ExecutorConfig
	now   func() time.Time
}

func NewLauncher(run SessionRunner, execs *ExecutorConfig) *Launcher {
	return &Launcher{
		run:   run,
		execs: execs,
		now:   time.Now,
	}
}

// LaunchRoute is the executor a scheduled launch runs on, with the model and
// reasoning level the person chose for it. Empty Model and Reasoning keep the
// executor's own command, as `mewla worker spawn` does.
type LaunchRoute struct {
	Executor  string
	Model     string
	Reasoning string
}

// ScheduledLaunchCommand resolves the unattended command of a scheduled
// launch: the executor's catalog command with the route's model and reasoning
// applied by the same helpers `mewla worker spawn` uses, then the unattended
// policy of ScheduledActionCommand. A route the client cannot honor is an
// error, so Calendar validates it when an item is saved, not when it fires.
func (c *ExecutorConfig) ScheduledLaunchCommand(route LaunchRoute) (string, error) {
	role := strings.TrimSpace(route.Executor)
	model, reasoning := strings.TrimSpace(route.Model), strings.TrimSpace(route.Reasoning)
	if c == nil {
		return "", fmt.Errorf("executor config required")
	}
	executor, ok := c.ByName[role]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrExecutorNotConfigured, role)
	}
	if model != "" || reasoning != "" {
		worker := NewWorkerExecutor(role, executor)
		var command string
		var err error
		if worker.Provider == WorkerProviderCodex {
			command, err = codexWorkerCommand(worker.Command, model, reasoning, true)
		} else {
			command, err = withLaunchModel(worker.Provider, worker.Command, model, reasoning)
		}
		if err != nil {
			return "", fmt.Errorf("executor %q: %w", role, err)
		}
		executor.Command = command
	}
	return ScheduledActionCommand(role, executor)
}

// ScheduledRoute resolves the Worker of a scheduled action: the item's own
// route, else the person's default. See ExecutorConfig.ResolveScheduledRoute.
func (l *Launcher) ScheduledRoute(item LaunchRoute) (LaunchRoute, string, error) {
	return l.execs.ResolveScheduledRoute(item)
}

// StartDedicated always spawns a fresh session of the routed executor. The
// caller chooses the route and persists the returned started metadata.
func (l *Launcher) StartDedicated(item *Item, cwd string, route LaunchRoute) (*Item, error) {
	if item == nil {
		return nil, fmt.Errorf("work item required")
	}
	if item.Frontmatter.Started != nil {
		return nil, ErrAlreadyStarted
	}
	if l.execs == nil {
		return nil, fmt.Errorf("executor config required")
	}

	role := strings.TrimSpace(route.Executor)
	command, err := l.execs.ScheduledLaunchCommand(route)
	if err != nil {
		return nil, err
	}

	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil, fmt.Errorf("work cwd required")
	}
	sessionID, err := l.run.Spawn(role, cwd, command)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
	}

	prompt := buildInitialPrompt(item.Path)
	if err := l.run.SendWhenReady(sessionID, command, prompt); err != nil {
		launchErr := fmt.Errorf("%w: send prompt: %v", ErrSpawnFailed, err)
		if abortErr := l.run.Abort(sessionID); abortErr != nil {
			return nil, fmt.Errorf("%w; abort fresh session: %v", launchErr, abortErr)
		}
		return nil, launchErr
	}

	next := cloneItem(item)
	now := l.now()
	next.Frontmatter.Started = &now
	next.Frontmatter.WorkerSession = sessionID
	return next, nil
}

func buildInitialPrompt(path string) string {
	return strings.TrimSpace(fmt.Sprintf(`
	Your work item is described in this file: %s
	Read it, do the work, and edit the file as you progress.
When finished, set `+"`done: <ISO8601 timestamp>`"+` in the frontmatter.
`, path)) + "\n"
}
