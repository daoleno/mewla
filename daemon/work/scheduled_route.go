package work

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/daoleno/mewla/daemon/statedir"
)

// ErrNoScheduledRoute means a Calendar scheduled_action names no Worker and the
// person has chosen no default one. Mewla never substitutes the Brain host.
var ErrNoScheduledRoute = errors.New("scheduled action has no Worker route")

const (
	// ScheduledRouteItem and ScheduledRouteDefault say where a resolved route
	// came from; the Work file records it.
	ScheduledRouteItem    = "calendar_item"
	ScheduledRouteDefault = "scheduled_action_default"
)

// DefaultScheduledActionRoutePath returns ~/.mewla/scheduled-action-route.toml.
func DefaultScheduledActionRoutePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "scheduled-action-route.toml"), nil
}

// LoadScheduledActionRoute reads the person's default Worker for Calendar
// scheduled actions that name none:
//
//	executor  = "claude"
//	model     = "claude-sonnet-5-5"   # optional
//	reasoning = "medium"              # optional
//
// A missing file is no default. A file with an unknown key or without an
// executor is an error, so a typo cannot silently become "no default".
func LoadScheduledActionRoute(path string) (LaunchRoute, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LaunchRoute{}, nil
	}
	if err != nil {
		return LaunchRoute{}, err
	}
	var route LaunchRoute
	var file struct {
		Executor  string `toml:"executor"`
		Model     string `toml:"model"`
		Reasoning string `toml:"reasoning"`
	}
	meta, err := toml.Decode(string(raw), &file)
	if err != nil {
		return LaunchRoute{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return LaunchRoute{}, fmt.Errorf("%s: unknown key %s (expected executor, model, reasoning)", path, undecoded[0])
	}
	route = LaunchRoute{Executor: strings.TrimSpace(file.Executor), Model: strings.TrimSpace(file.Model), Reasoning: strings.TrimSpace(file.Reasoning)}
	if route.Executor == "" {
		return LaunchRoute{}, fmt.Errorf("%s: executor is required", path)
	}
	return route, nil
}

// ResolveScheduledRoute is the one place a scheduled action's Worker is chosen,
// for saving an item (CLI, control socket, phone) and for launching it: the
// item's own route when it names an executor, else the person's default. It
// checks that the route can launch, so a bad one fails when saved. The Brain
// host is never consulted.
func (c *ExecutorConfig) ResolveScheduledRoute(item LaunchRoute) (LaunchRoute, string, error) {
	if c == nil {
		return LaunchRoute{}, "", fmt.Errorf("executor config required")
	}
	route, source := item, ScheduledRouteItem
	if strings.TrimSpace(item.Executor) == "" {
		route, source = c.ScheduledAction, ScheduledRouteDefault
	}
	if strings.TrimSpace(route.Executor) == "" {
		return LaunchRoute{}, "", fmt.Errorf("%w: pass an executor, or set a default in scheduled-action-route.toml", ErrNoScheduledRoute)
	}
	if _, err := c.ScheduledLaunchCommand(route); err != nil {
		if source == ScheduledRouteDefault {
			err = fmt.Errorf("default Worker in scheduled-action-route.toml: %w", err)
		}
		return LaunchRoute{}, "", err
	}
	return LaunchRoute{Executor: strings.TrimSpace(route.Executor), Model: strings.TrimSpace(route.Model), Reasoning: strings.TrimSpace(route.Reasoning)}, source, nil
}
