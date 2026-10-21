package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/memorysearch"
)

func printMemoryUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: mewla memory <search|show> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Look up what the user said or decided in Brain chat (visible text only).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  search <query>   Find messages; exact phrase first, then newest")
	fmt.Fprintln(w, "  show <id>        Print one message with the ones around it")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  mewla memory search \"不要用写死的规则\" --from user --since 30d")
	fmt.Fprintln(w, "  mewla memory show <id> --around 5 --json")
}

func runMemoryCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printMemoryUsage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "search":
		return runMemorySearch(args[1:], stderr)
	case "show":
		return runMemoryShow(args[1:], stderr)
	default:
		return fmt.Errorf("unknown memory command: %s", args[0])
	}
}

// parseInterspersed parses flags that may follow positional arguments, so
// `search "query" --limit 5` works like `search --limit 5 "query"`.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func runMemorySearch(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla memory search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg cliConfig
	var from, since, until string
	var limit int
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", false, "print JSON output")
	fs.StringVar(&from, "from", "all", "whose messages: user (typed), brain, context (summaries, forwarded events) or all")
	fs.StringVar(&since, "since", "", "only at or after this time: RFC3339, YYYY-MM-DD, or an age such as 36h or 30d")
	fs.StringVar(&until, "until", "", "only at or before this time (same formats)")
	fs.IntVar(&limit, "limit", 20, "maximum hits")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positional, " "))
	if query == "" {
		return fmt.Errorf("usage: mewla memory search <query> [--from user|brain|all] [--since T] [--until T] [--limit N] [--json]")
	}
	now := time.Now()
	sinceAt, err := parseMemoryTime(since, now, false)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}
	untilAt, err := parseMemoryTime(until, now, true)
	if err != nil {
		return fmt.Errorf("--until: %w", err)
	}
	resp, err := callControl(cfg, control.Request{Type: "memory_search", Memory: &control.MemoryRequest{
		Query: query, From: from, Since: sinceAt, Until: untilAt, Limit: limit,
	}})
	if err != nil {
		return err
	}
	if cfg.json || !resp.OK {
		return writeControlResponse(os.Stdout, resp, true)
	}
	writeMemoryHits(os.Stdout, resp.MemoryHits)
	return nil
}

func runMemoryShow(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla memory show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg cliConfig
	var around int
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", false, "print JSON output")
	fs.IntVar(&around, "around", 3, "messages to include before and after")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: mewla memory show <id> [--around N] [--json]")
	}
	if around < 0 {
		return fmt.Errorf("--around must not be negative")
	}
	resp, err := callControl(cfg, control.Request{Type: "memory_show", Memory: &control.MemoryRequest{
		ID: positional[0], Around: around,
	}})
	if err != nil {
		return err
	}
	if cfg.json || !resp.OK {
		return writeControlResponse(os.Stdout, resp, true)
	}
	writeMemoryMessages(os.Stdout, resp.MemoryMessages)
	return nil
}

func memoryStamp(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05Z")
}

func writeMemoryHits(w io.Writer, hits []memorysearch.Hit) {
	if len(hits) == 0 {
		fmt.Fprintln(w, "No matching messages.")
		return
	}
	for _, hit := range hits {
		line := fmt.Sprintf("%s  %-5s  %s", memoryStamp(hit.At), hit.Who, hit.ID)
		if hit.WorkID != "" {
			line += "  work=" + hit.WorkID
		}
		if hit.Duplicates > 0 {
			line += fmt.Sprintf("  (+%d same)", hit.Duplicates)
		}
		fmt.Fprintln(w, line)
		fmt.Fprintln(w, "    "+hit.Snippet)
	}
}

func writeMemoryMessages(w io.Writer, messages []memorysearch.Message) {
	for i, message := range messages {
		marker := " "
		if message.Match {
			marker = ">"
		}
		header := fmt.Sprintf("%s %s  %s  %s", marker, memoryStamp(message.At), message.Who, message.ID)
		if message.WorkID != "" {
			header += "  work=" + message.WorkID
		}
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, header)
		fmt.Fprintln(w, message.Text)
	}
}

// parseMemoryTime reads RFC3339, a date (start of day UTC, or end of day when
// endOfDay), or an age like 36h / 30d / 2w meaning that long before now.
func parseMemoryTime(value string, now time.Time, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at, nil
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDay {
			return day.Add(24*time.Hour - time.Nanosecond), nil
		}
		return day, nil
	}
	if n := len(value); n > 1 {
		if count, err := strconv.Atoi(value[:n-1]); err == nil && count >= 0 {
			unit := map[byte]time.Duration{
				'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour,
			}[value[n-1]]
			if unit > 0 {
				return now.Add(-time.Duration(count) * unit), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("%q is not RFC3339, YYYY-MM-DD or an age like 30d", value)
}
