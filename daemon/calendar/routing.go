package calendar

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// scheduledActionTask is the routing.md row that chooses the executor, model
// and reasoning of a scheduled_action that names none of its own:
//
//	| Scheduled action | claude | <model> | low |
//
// Like the "Watch judgment" and "Work progress" rows it has four cells (Task |
// Executor | Model | Reasoning), the Task cell is matched by prefix without
// regard to case, and "default" (or "client default", "-", empty) leaves a cell
// to the executor.
const scheduledActionTask = "Scheduled action"

// ActionRoute is a routing.md row's choice. A cell that says default is empty.
type ActionRoute struct {
	Executor, Model, Reasoning string
}

// ReadScheduledActionRoute reads the "Scheduled action" row of routing.md at
// path. No path, no file or no row is no choice and no error; a file that
// cannot be read or a row that is not four cells is an error and no choice,
// so the caller falls back as if there were no row. The cells' values are not
// judged here: the launch that uses them is.
func ReadScheduledActionRoute(path string) (ActionRoute, error) {
	if strings.TrimSpace(path) == "" {
		return ActionRoute{}, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ActionRoute{}, nil
	}
	if err != nil {
		return ActionRoute{}, fmt.Errorf("routing.md is not readable: %w", err)
	}
	return parseScheduledActionRoute(string(raw))
}

func parseScheduledActionRoute(text string) (ActionRoute, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for index := range cells {
			cells[index] = strings.TrimSpace(strings.Trim(strings.TrimSpace(cells[index]), "`"))
		}
		if !strings.HasPrefix(strings.ToLower(cells[0]), strings.ToLower(scheduledActionTask)) {
			continue
		}
		if len(cells) != 4 {
			return ActionRoute{}, fmt.Errorf("the %s row of routing.md needs four cells (Task | Executor | Model | Reasoning), found %d", scheduledActionTask, len(cells))
		}
		cell := func(value string) string {
			switch strings.ToLower(value) {
			case "", "default", "client default", "-":
				return ""
			}
			return value
		}
		return ActionRoute{Executor: cell(cells[1]), Model: cell(cells[2]), Reasoning: cell(cells[3])}, nil
	}
	return ActionRoute{}, nil
}
