package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
)

const gcUsage = `Usage: mewla gc <status|restore> [flags]

  status            What this Session holds and what happens to it when it ends,
                    and what the collector moved to its trash (--all: everyone's)
  restore ID        Put a trashed item back where it was
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: mewla gc status [--all] [--json]")
	}
	session, sessionRoot := gcCallingSession()
	root, err := gcStateRoot(sessionRoot)
	if err != nil {
		return err
	}
	items, err := gc.List(root)
	if err != nil {
		return err
	}
	status := gcStatus{Session: session, Trash: []gcTrashItem{}}
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla gc restore ID")
	}
	_, sessionRoot := gcCallingSession()
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
