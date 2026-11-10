package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/daoleno/mewla/daemon/tmuxsocket"
)

// attachEntry is one Session `mewla attach` can show: one the session host
// runs, or one an earlier Mewla left in tmux. The person never has to care
// which; Tmux is set only for the second kind.
type attachEntry struct {
	ID      string
	Name    string // the Worker's name, "Brain", or "Shell"
	Agent   string // claude, codex, ... or the foreground command
	State   string // working, idle, needs input, running, exited
	Cwd     string
	Created time.Time // zero when the backend does not say
	Live    bool
	Brain   bool
	Hidden  bool
	Tmux    *attachTmux
}

// attachTmux is where a Session still in tmux lives: its server (empty is the
// user's default one), the tmux session that holds its pane, and the pane.
type attachTmux struct {
	Socket  string
	Session string
	Pane    string
}

// listed reports whether the picker and name lookups see the entry: a live
// Session, and of the hidden ones only Brain.
func (e attachEntry) listed() bool { return e.Live && (!e.Hidden || e.Brain) }

var workerNameSuffix = regexp.MustCompile(` \([^()]*\)$`)

// bareWorkerName drops the " (id)" the daemon appends to a Worker's name in
// its list.
func bareWorkerName(name string) string {
	return strings.TrimSpace(workerNameSuffix.ReplaceAllString(strings.TrimSpace(name), ""))
}

// hostAttachEntries turns the session host's Sessions into entries. needsInput
// names the Sessions the daemon sees stopped at a prompt.
func hostAttachEntries(infos []sessionhost.Info, needsInput map[string]bool) []attachEntry {
	out := make([]attachEntry, 0, len(infos))
	for _, info := range infos {
		name := bareWorkerName(info.Labels["name"])
		hidden := info.Labels["hidden"] == "1"
		entry := attachEntry{
			ID:      info.ID,
			Name:    name,
			Agent:   info.Labels["executor"],
			Cwd:     firstNonEmpty(info.Cwd, info.Dir),
			Created: info.CreatedAt,
			Live:    info.Alive,
			Hidden:  hidden,
			Brain:   hidden && name == "Brain",
		}
		if entry.Name == "" {
			entry.Name = "Shell"
		}
		if entry.Agent == "" {
			entry.Agent = strings.TrimSpace(info.Command)
		}
		entry.State = hostSessionState(info)
		if entry.Live && needsInput[info.ID] {
			entry.State = "needs input"
		}
		out = append(out, entry)
	}
	return out
}

// hostSessionState is what the Session's program last reported, or "running"
// for one that reports nothing.
func hostSessionState(info sessionhost.Info) string {
	if !info.Alive {
		return "exited"
	}
	var latest *sessionhost.ProgramStatus
	for i := range info.Status {
		if latest == nil || info.Status[i].UpdatedAt.After(latest.UpdatedAt) {
			latest = &info.Status[i]
		}
	}
	if latest == nil {
		return "running"
	}
	switch latest.State {
	case "working":
		return "working"
	case "blocked":
		return "needs input"
	case "idle", "done":
		return "idle"
	case "error":
		return "error"
	}
	return "running"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// tmuxPane is a pane of one tmux server and the tmux session that holds it.
type tmuxPane struct {
	Session string
}

// tmuxAttachEntries adds the daemon's Sessions that still run in tmux. panes
// asks a tmux server which of its panes exist; a Session whose pane is gone is
// not live and is left out. hostIDs are the ids the session host has, which
// always win.
func tmuxAttachEntries(workers []control.Worker, hostIDs map[string]bool, panes func(socket string) map[string]tmuxPane) []attachEntry {
	if runtime.GOOS == "windows" {
		return nil
	}
	var out []attachEntry
	known := map[string]map[string]tmuxPane{}
	for _, worker := range workers {
		if worker.Terminal == "host" || hostIDs[worker.ID] || !strings.HasPrefix(worker.ID, "%") {
			continue
		}
		list, seen := known[worker.TmuxSocket]
		if !seen {
			list = panes(worker.TmuxSocket)
			known[worker.TmuxSocket] = list
		}
		pane, ok := list[worker.ID]
		if !ok {
			continue
		}
		entry := attachEntry{
			ID:    worker.ID,
			Name:  bareWorkerName(worker.Name),
			Agent: worker.ExecutorID,
			State: "running",
			Cwd:   worker.Cwd,
			Live:  true,
			Tmux:  &attachTmux{Socket: worker.TmuxSocket, Session: pane.Session, Pane: worker.ID},
		}
		if worker.PromptID != "" {
			entry.State = "needs input"
		}
		if entry.Name == "" {
			entry.Name = "Shell"
		}
		out = append(out, entry)
	}
	return out
}

// listTmuxPanes asks one tmux server for its panes; a missing tmux or server
// has none.
func listTmuxPanes(socket string) map[string]tmuxPane {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := append(tmuxsocket.ClientArgs(socket), "list-panes", "-a", "-F", "#{pane_id} #{session_id}")
	raw, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return nil
	}
	return parseTmuxPanes(string(raw))
}

func parseTmuxPanes(raw string) map[string]tmuxPane {
	panes := map[string]tmuxPane{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			panes[fields[0]] = tmuxPane{Session: fields[1]}
		}
	}
	return panes
}

// sortAttachEntries puts the newest Session first. Sessions with no known age
// (still in tmux) follow, highest pane number first.
func sortAttachEntries(entries []attachEntry) {
	number := func(id string) int {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "%"))
		return n
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		switch {
		case !a.Created.IsZero() && !b.Created.IsZero() && !a.Created.Equal(b.Created):
			return a.Created.After(b.Created)
		case a.Created.IsZero() != b.Created.IsZero():
			return !a.Created.IsZero()
		case a.Created.IsZero() && number(a.ID) != number(b.ID):
			return number(a.ID) > number(b.ID)
		}
		return a.ID < b.ID
	})
}

// listedAttachEntries are the entries a person can pick, newest first.
func listedAttachEntries(all []attachEntry) []attachEntry {
	var out []attachEntry
	for _, entry := range all {
		if entry.listed() {
			out = append(out, entry)
		}
	}
	sortAttachEntries(out)
	return out
}

// attachAmbiguousError says a name matched several Sessions.
type attachAmbiguousError struct {
	Query   string
	Matches []attachEntry
}

func (e *attachAmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d Sessions; name one by its id:\n", e.Query, len(e.Matches))
	writeAttachTable(&b, e.Matches, time.Now(), true)
	return strings.TrimRight(b.String(), "\n")
}

// attachNotFoundError says nothing matched.
type attachNotFoundError struct{ Query string }

func (e *attachNotFoundError) Error() string {
	return fmt.Sprintf("no live Session matches %q; run mewla attach to pick one", e.Query)
}

// resolveAttach finds the one Session query names: an exact id (any Session
// the host still holds, even an ended one), "brain", a Worker name in any case,
// or the start of exactly one Worker name. An exact name wins over longer
// names that start with it. Several matches fail with the matches listed.
func resolveAttach(all []attachEntry, query string) (attachEntry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return attachEntry{}, &attachNotFoundError{Query: query}
	}
	for _, entry := range all {
		if entry.ID == query {
			return entry, nil
		}
	}
	listed := listedAttachEntries(all)
	lower := strings.ToLower(query)
	pick := func(matches []attachEntry) (attachEntry, bool, error) {
		switch len(matches) {
		case 0:
			return attachEntry{}, false, nil
		case 1:
			return matches[0], true, nil
		}
		return attachEntry{}, false, &attachAmbiguousError{Query: query, Matches: matches}
	}
	if lower == "brain" {
		var brains []attachEntry
		for _, entry := range listed {
			if entry.Brain {
				brains = append(brains, entry)
			}
		}
		if entry, ok, err := pick(brains); ok || err != nil {
			return entry, err
		}
	}
	var exact, prefix []attachEntry
	for _, entry := range listed {
		name := strings.ToLower(entry.Name)
		switch {
		case name == lower:
			exact = append(exact, entry)
		case strings.HasPrefix(name, lower):
			prefix = append(prefix, entry)
		}
	}
	if entry, ok, err := pick(exact); ok || err != nil {
		return entry, err
	}
	if entry, ok, err := pick(prefix); ok || err != nil {
		return entry, err
	}
	return attachEntry{}, &attachNotFoundError{Query: query}
}

// humanAge is how long ago, in the largest whole unit.
func humanAge(created, now time.Time) string {
	if created.IsZero() {
		return "-"
	}
	d := now.Sub(created)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// homeShortened writes the user's home directory as ~.
func homeShortened(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// writeAttachTable prints entries one per line, with the id column first when
// withID is set (the list a script reads and the matches of an ambiguous name).
func writeAttachTable(out io.Writer, entries []attachEntry, now time.Time, withID bool) {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	header := "NAME\tAGENT\tSTATE\tCWD\tAGE"
	if withID {
		header = "ID\t" + header
	}
	fmt.Fprintln(table, header)
	for _, entry := range entries {
		line := strings.Join([]string{entry.Name, dash(entry.Agent), entry.State, dash(homeShortened(entry.Cwd)), humanAge(entry.Created, now)}, "\t")
		if withID {
			line = entry.ID + "\t" + line
		}
		fmt.Fprintln(table, line)
	}
	table.Flush()
}

func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
