package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
)

const gcUsage = `Usage: mewla gc <status|why|keep|release|restore> [flags]

  status            What this Session holds and what happens to it when it ends,
                    and what the collector moved to its trash (--all: everyone's)
  keep PATH --until DATE --why REASON
                    Keep a path in this Session's scratch past the Session, until
                    DATE (YYYY-MM-DD or a number of days: 7d; 30 days at most)
  release PATH|ID   Move a path in this Session's scratch to the trash now, or
                    end one of this Session's keeps
  restore ID        Put a trashed item back where it was
  why PID|PATH      What holds a process or a path: its Session and Work, or
                    that it is not Mewla's
`

func runGCCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, gcUsage)
		if len(args) == 0 {
			return errors.New("missing gc subcommand")
		}
		return flag.ErrHelp
	}
	switch args[0] {
	case "status":
		return runGCStatus(args[1:], os.Stdout, stderr)
	case "restore":
		return runGCRestore(args[1:], os.Stdout, stderr)
	case "keep":
		return runGCKeep(args[1:], os.Stdout, stderr)
	case "release":
		return runGCRelease(args[1:], os.Stdout, stderr)
	case "why":
		return runGCWhy(args[1:], os.Stdout, stderr)
	default:
		fmt.Fprint(stderr, gcUsage)
		return fmt.Errorf("unknown gc subcommand %q", args[0])
	}
}

// gcSession is the calling Session's own holdings, read from what Mewla gave
// it: its resource unit, its lease and its scratch directory.
type gcSession struct {
	ID        string `json:"id,omitempty"`
	Unit      string `json:"unit"`
	Scratch   string `json:"scratch,omitempty"`
	Bytes     int64  `json:"scratch_bytes"`
	Processes int    `json:"processes"` // -1: not known on this platform
	AtEnd     string `json:"at_end"`
}

type gcTrashItem struct {
	gc.Item
	Path        string    `json:"path"`
	Moved       bool      `json:"moved"`
	Bytes       int64     `json:"bytes"`
	DeleteAfter time.Time `json:"delete_after"`
}

type gcStatus struct {
	Session *gcSession    `json:"session,omitempty"`
	Kept    []gc.Keep     `json:"kept"`
	Trash   []gcTrashItem `json:"trash"`
}

const gcAtEnd = "processes stop; scratch goes to the trash for 3 days"

// gcCallingSession returns the calling Session's holdings and the state root
// they live under, or nil outside a delegated Session.
func gcCallingSession() (*gcSession, string) {
	unit := strings.TrimSpace(os.Getenv("MEWLA_WORKER_RESOURCE_UNIT"))
	if unit == "" {
		return nil, ""
	}
	session := &gcSession{ID: os.Getenv("MEWLA_WORKER_ID"), Unit: unit, AtEnd: gcAtEnd}
	root := ""
	if scratch := strings.TrimSpace(os.Getenv("TMPDIR")); scratch != "" {
		if raw, err := os.ReadFile(filepath.Join(scratch, ".mewla-worker-unit")); err == nil && strings.TrimSpace(string(raw)) == unit {
			session.Scratch = scratch
			session.Bytes = gc.Size(scratch)
			root = filepath.Dir(filepath.Dir(scratch)) // <root>/t/<digest>
		}
	}
	session.Processes = gcSessionProcesses(root, unit)
	return session, root
}

func gcStateRoot(sessionRoot string) (string, error) {
	if sessionRoot != "" {
		return sessionRoot, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return statedir.Default(home), nil
}

func runGCStatus(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla gc status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "list the whole trash, not only this Session's")
	stateDir := fs.String("state-dir", "", "state directory (default: this Session's, else ~/.mewla)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: mewla gc status [--all] [--json] [--state-dir DIR]")
	}
	session, sessionRoot := gcCallingSession()
	if *stateDir != "" {
		session, sessionRoot = nil, *stateDir
	}
	root, err := gcStateRoot(sessionRoot)
	if err != nil {
		return err
	}
	items, err := gc.List(root)
	if err != nil {
		return err
	}
	status := gcStatus{Session: session, Kept: []gc.Keep{}, Trash: []gcTrashItem{}}
	keeps, err := gc.ListKeeps(root)
	if err != nil {
		return err
	}
	for _, k := range keeps {
		if session == nil || *all || k.Owner == session.Unit {
			status.Kept = append(status.Kept, k)
		}
	}
	for _, item := range items {
		if session != nil && !*all && item.Owner != session.Unit {
			continue
		}
		entry := gcTrashItem{Item: item, Path: item.Path, Moved: item.Moved, DeleteAfter: item.DeleteAfter()}
		if item.Moved {
			entry.Bytes = gc.Size(item.Path)
		}
		status.Trash = append(status.Trash, entry)
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}
	writeGCStatus(stdout, status)
	return nil
}

func writeGCStatus(out io.Writer, status gcStatus) {
	if s := status.Session; s != nil {
		fmt.Fprintln(out, "This Session")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if s.Scratch != "" {
			fmt.Fprintf(tw, "  Scratch\t%s\t%s\n", s.Scratch, gcBytes(s.Bytes))
		}
		if s.Processes >= 0 {
			fmt.Fprintf(tw, "  Processes\t%d\t\n", s.Processes)
		}
		fmt.Fprintf(tw, "  When it ends\t%s\t\n", s.AtEnd)
		_ = tw.Flush()
		fmt.Fprintln(out)
	}
	if len(status.Kept) > 0 {
		fmt.Fprintf(out, "Kept: %d\n", len(status.Kept))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  ID\tUNTIL\tPATH\tWHY")
		for _, k := range status.Kept {
			path := k.Path
			if k.Moved != "" {
				path = k.Moved
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", k.ID, k.Until.Local().Format("01-02 15:04"), path, k.Why)
		}
		_ = tw.Flush()
		fmt.Fprintln(out)
	}
	var total int64
	for _, item := range status.Trash {
		total += item.Bytes
	}
	if len(status.Trash) == 0 {
		fmt.Fprintln(out, "Trash: empty")
		return
	}
	fmt.Fprintf(out, "Trash: %d, %s\n", len(status.Trash), gcBytes(total))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  ID\tKIND\tSIZE\tTRASHED\tDELETED AFTER\tFROM\tWHY")
	for _, item := range status.Trash {
		size := gcBytes(item.Bytes)
		if !item.Moved {
			size = "not moved"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Kind, size,
			item.TrashedAt.Local().Format("01-02 15:04"), item.DeleteAfter.Local().Format("01-02 15:04"), item.Original, item.Reason)
	}
	_ = tw.Flush()
}

func runGCRestore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla gc restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory (default: this Session's, else ~/.mewla)")
	if err := fs.Parse(gcFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla gc restore ID [--state-dir DIR]")
	}
	_, sessionRoot := gcCallingSession()
	if *stateDir != "" {
		sessionRoot = *stateDir
	}
	root, err := gcStateRoot(sessionRoot)
	if err != nil {
		return err
	}
	item, err := gc.Restore(root, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Restored %s\n", item.Original)
	return nil
}

func gcBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "KMGT"
	i := -1
	for value >= unit && i < len(suffix)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", value, suffix[i])
}

// gcOwnSession is the calling Session as the runtime recorded it: its unit,
// its scratch and the state root. The environment only names the unit; the
// scratch is found from the unit, and the caller must be the Session's by
// the runtime's own record (gcProveSession), never by what it says.
type gcOwnSession struct {
	Unit, Scratch, Root string
}

func gcOwnCallingSession() (gcOwnSession, error) {
	unit := strings.TrimSpace(os.Getenv("MEWLA_WORKER_RESOURCE_UNIT"))
	if unit == "" {
		return gcOwnSession{}, errors.New("not inside a Mewla Session: keep and release act on a Session's own scratch")
	}
	owner, _, ok := strings.Cut(strings.TrimPrefix(unit, "mewla-worker-"), "-")
	if !ok || !strings.HasPrefix(unit, "mewla-worker-") || !strings.HasSuffix(unit, ".scope") {
		return gcOwnSession{}, fmt.Errorf("%q is not a Session's resource unit", unit)
	}
	_, hint := gcCallingSession()
	root, err := gcStateRoot(hint)
	if err != nil {
		return gcOwnSession{}, err
	}
	scratch := filepath.Join(root, "t", gc.ScratchName(unit))
	raw, err := os.ReadFile(filepath.Join(scratch, gc.ScratchMarker))
	if err != nil || strings.TrimSpace(string(raw)) != unit {
		return gcOwnSession{}, fmt.Errorf("%s has no scratch at %s", unit, scratch)
	}
	if err := gcProveSession(root, owner, unit); err != nil {
		return gcOwnSession{}, err
	}
	return gcOwnSession{Unit: unit, Scratch: scratch, Root: root}, nil
}

// gcOwnPath resolves path (symlinks too) and requires it inside the
// Session's scratch, not the scratch itself nor its marker.
func gcOwnPath(session gcOwnSession, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	scratch, err := filepath.EvalSymlinks(session.Scratch)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(scratch, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not inside this Session's scratch %s", path, session.Scratch)
	}
	if rel == gc.ScratchMarker {
		return "", errors.New("the scratch marker stays with the scratch")
	}
	return filepath.Join(session.Scratch, rel), nil
}

// gcUntil reads --until: a date (kept through its end, local time) or Nd.
func gcUntil(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(value, "d"); ok {
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err == nil && n > 0 && fmt.Sprint(n) == days {
			return now.Add(time.Duration(n) * 24 * time.Hour), nil
		}
	}
	day, err := time.ParseInLocation(time.DateOnly, value, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("--until %q: give a date (YYYY-MM-DD) or a number of days (7d)", value)
	}
	return day.Add(24*time.Hour - time.Second), nil
}

func runGCKeep(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla gc keep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	untilFlag := fs.String("until", "", "keep it until this date (YYYY-MM-DD) or for this many days (7d); 30 days at most")
	why := fs.String("why", "", "why it must outlive the Session")
	if err := fs.Parse(gcFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 || *untilFlag == "" {
		return errors.New("usage: mewla gc keep PATH --until DATE --why REASON")
	}
	session, err := gcOwnCallingSession()
	if err != nil {
		return err
	}
	path, err := gcOwnPath(session, fs.Arg(0))
	if err != nil {
		return err
	}
	now := time.Now()
	until, err := gcUntil(*untilFlag, now)
	if err != nil {
		return err
	}
	k, err := gc.AddKeep(session.Root, path, session.Unit, until, *why, now)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Kept %s until %s (%s). When this Session ends it moves to %s.\n", path, k.Until.Local().Format("2006-01-02 15:04"), k.ID, gc.KeptPath(session.Root, k))
	return nil
}

func runGCRelease(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla gc release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla gc release PATH|ID")
	}
	session, err := gcOwnCallingSession()
	if err != nil {
		return err
	}
	now := time.Now()
	target := fs.Arg(0)
	keeps, err := gc.ListKeeps(session.Root)
	if err != nil {
		return err
	}
	for _, k := range keeps {
		if k.ID == target {
			if _, err := gc.ReleaseKeep(session.Root, k.ID, session.Unit, now); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Ended keep %s; %s goes with this Session.\n", k.ID, k.Path)
			return nil
		}
	}
	path, err := gcOwnPath(session, target)
	if err != nil {
		return err
	}
	for _, k := range keeps {
		if k.Owner == session.Unit && k.Path == path {
			if _, err := gc.ReleaseKeep(session.Root, k.ID, session.Unit, now); err != nil {
				return err
			}
		}
	}
	item, err := gc.Trash(session.Root, gc.KindScratch, path, session.Unit, gc.ReasonReleased, now)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Moved %s to the trash (%s); mewla gc restore %s puts it back.\n", path, item.ID, item.ID)
	return nil
}

// gcFlagsFirst lets the path come before the flags (keep PATH --until …).
func gcFlagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && !gcBoolFlag(fs, args[i]) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}

// gcBoolFlag reports whether arg names a flag of fs that takes no value.
func gcBoolFlag(fs *flag.FlagSet, arg string) bool {
	f := fs.Lookup(strings.TrimLeft(arg, "-"))
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func runGCWhy(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla gc why", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	stateDir := fs.String("state-dir", "", "state directory (default: this Session's, else ~/.mewla)")
	if err := fs.Parse(gcFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla gc why PID|PATH [--json] [--state-dir DIR]")
	}
	req := control.Request{Type: "gc_why"}
	if pid, err := strconv.Atoi(fs.Arg(0)); err == nil && pid > 0 {
		req.ProcessID = pid
	} else {
		path, err := filepath.Abs(fs.Arg(0)) // the daemon's working directory is not this one
		if err != nil {
			return err
		}
		req.Path = path
	}
	daemonDir := *stateDir
	if daemonDir == "" {
		daemonDir = strings.TrimSpace(os.Getenv("MEWLA_STATE_DIR"))
	}
	var why gc.Why
	resp, err := callControl(cliConfig{stateDir: daemonDir}, req)
	older := err == nil && !resp.OK && resp.Error != nil && resp.Error.Code == "unknown_request"
	switch {
	case err == nil && resp.OK && resp.Why != nil:
		why = *resp.Why
	case err == nil && !older:
		return controlResponseError(resp)
	default:
		// No daemon to ask, or one from before gc why: what the records on
		// disk tell, without the live Sessions' names and Work.
		_, sessionRoot := gcCallingSession()
		if *stateDir != "" {
			sessionRoot = *stateDir
		}
		root, rootErr := gcStateRoot(sessionRoot)
		if rootErr != nil {
			return rootErr
		}
		lookup := gc.WhyLookup{Root: root}
		if req.ProcessID > 0 {
			why, err = gc.WhyProcess(lookup, req.ProcessID)
		} else {
			why, err = gc.WhyPath(lookup, req.Path)
		}
		if err != nil {
			return err
		}
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(why)
	}
	writeGCWhy(stdout, why)
	return nil
}

var gcWhyKinds = map[string]string{"session": "Session", "work": "Work", "kept": "keep"}

func writeGCWhy(out io.Writer, why gc.Why) {
	for i, link := range why.Chain {
		indent := ""
		if i > 0 {
			indent = "  "
		}
		var line string
		switch link.Kind {
		case "unheld", "outside":
			line = "not Mewla's: " + link.Name
			if link.ID != "" {
				line += " " + link.ID
			}
		default:
			kind := link.Kind
			if title, ok := gcWhyKinds[kind]; ok {
				kind = title
			}
			line = strings.TrimSpace(kind + " " + link.ID + " " + link.Name)
			if link.State != "" {
				line += ", " + link.State
			}
			if link.By != "" {
				line += " (" + link.By + ")"
			}
		}
		fmt.Fprintln(out, indent+line)
	}
}
