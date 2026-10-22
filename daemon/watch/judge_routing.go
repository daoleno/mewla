package watch

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Judgment is routed like Worker work: one row of routing.md, whose Task cell
// starts with "Watch judgment", names the executor, model and reasoning.
//
//	| Watch judgment | pi | opencode-go/deepseek-v4.1-flash | low |
//
// The file belongs to the user and Brain. It is read strictly, checked against
// the executors that can judge and are installed, and read again whenever it
// changes. A missing or invalid row falls back to the automatic choice, and
// the status says why. The row picks only who judges: the no-tools, no-hooks,
// no-session, redaction, batch, daily and cost limits hold whatever it says.

const routingTask = "watch judgment"

// judgeRoute is a validated routing row. Model and Effort are empty when the
// row says "default": the executor's own choice.
type judgeRoute struct {
	Executor string
	Model    string
	Effort   string
}

// Reasoning levels per executor, as routing.md documents them.
var judgeEfforts = map[string][]string{
	"claude": {"low", "medium", "high", "xhigh", "max"},
	"codex":  {"none", "minimal", "low", "medium", "high", "xhigh", "ultra"},
	"pi":     {"off", "minimal", "low", "medium", "high", "xhigh", "max"},
}

func isDefaultCell(value string) bool {
	switch strings.ToLower(value) {
	case "", "default", "client default", "-":
		return true
	}
	return false
}

// parseRoutingRow finds the Watch judgment row. found is false when the file
// has none; err explains a row that is there but cannot be used.
func parseRoutingRow(text string, installed func(string) bool) (route judgeRoute, found bool, err error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for index := range cells {
			cells[index] = strings.TrimSpace(strings.Trim(strings.TrimSpace(cells[index]), "`"))
		}
		if !strings.HasPrefix(strings.ToLower(cells[0]), routingTask) {
			continue
		}
		if len(cells) != 4 {
			return judgeRoute{}, true, fmt.Errorf("the Watch judgment row needs four cells (Task | Executor | Model | Reasoning), found %d", len(cells))
		}
		executor := strings.ToLower(cells[1])
		efforts, supported := judgeEfforts[executor]
		if !supported {
			return judgeRoute{}, true, fmt.Errorf("executor %q cannot judge here (claude, codex or pi)", cells[1])
		}
		if !installed(executor) {
			return judgeRoute{}, true, fmt.Errorf("executor %q is not installed", executor)
		}
		route = judgeRoute{Executor: executor}
		if !isDefaultCell(cells[2]) {
			if strings.ContainsAny(cells[2], " \t\"'") || strings.HasPrefix(cells[2], "-") {
				return judgeRoute{}, true, fmt.Errorf("model %q is not a model id", cells[2])
			}
			route.Model = cells[2]
		}
		if !isDefaultCell(cells[3]) {
			level := strings.ToLower(cells[3])
			valid := false
			for _, known := range efforts {
				valid = valid || known == level
			}
			if !valid {
				return judgeRoute{}, true, fmt.Errorf("reasoning %q is not one of %s for %s", cells[3], strings.Join(efforts, ", "), executor)
			}
			route.Effort = level
		}
		return route, true, nil
	}
	return judgeRoute{}, false, nil
}

// Routing says where the judgment executor came from.
type Routing struct {
	// Source is "routing.md" or "automatic".
	Source string `json:"source"`
	// Reason says why routing.md was not used, when it was not.
	Reason string `json:"reason,omitempty"`
}

type routingCache struct {
	modified time.Time
	size     int64
	route    judgeRoute
	valid    bool
	reason   string
}

// routed returns the route routing.md gives now. The file is stat'ed on every
// call and parsed again only when it changed.
func (j *judge) routed() (judgeRoute, Routing) {
	automatic := Routing{Source: "automatic"}
	if j.routingPath == "" {
		return judgeRoute{}, automatic
	}
	info, err := os.Stat(j.routingPath)
	if err != nil {
		automatic.Reason = "routing.md is not readable: " + err.Error()
		return judgeRoute{}, automatic
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	cached := j.routing
	if cached == nil || !cached.modified.Equal(info.ModTime()) || cached.size != info.Size() {
		cached = &routingCache{modified: info.ModTime(), size: info.Size()}
		raw, readErr := os.ReadFile(j.routingPath)
		installed := func(name string) bool { _, err := j.lookPath(name); return err == nil }
		switch route, found, parseErr := parseRoutingRow(string(raw), installed); {
		case readErr != nil:
			cached.reason = "routing.md is not readable: " + readErr.Error()
		case parseErr != nil:
			cached.reason = parseErr.Error()
		case !found:
			cached.reason = "routing.md has no Watch judgment row"
		default:
			cached.route, cached.valid = route, true
		}
		j.routing = cached
	}
	if cached.valid {
		return cached.route, Routing{Source: "routing.md"}
	}
	automatic.Reason = cached.reason
	return judgeRoute{}, automatic
}
