package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/statedir"
)

func runAddressCommand(args []string, stderr interface{ Write([]byte) (int, error) }) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, `Usage: mewla address <list|add|remove> [flags]

An address is a URL where phones and browsers reach this computer.
Mewla finds this computer's own, Wi-Fi/LAN and Tailscale addresses each time
it starts, and learns an HTTPS address the first time a paired device uses it.

  list                 Show every address and how Mewla knows it
  add https://host     Add an HTTPS tunnel or proxy in front of this computer;
                       the web UI is served there too
  remove <address>     Forget an address
`)
		return flag.ErrHelp
	}
	fs := flag.NewFlagSet("mewla address "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	var outputJSON bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	resolvedStateDir := resolveCLIStateDir(stateDir)
	book, err := addressbook.New(resolvedStateDir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: mewla address add <https-url>")
		}
		entry, err := book.Add(fs.Arg(0), addressbook.SourceManual)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, entry.URL)
		return nil
	case "remove":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: mewla address remove <address>")
		}
		return book.Remove(fs.Arg(0))
	case "list":
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
		}
		entries, err := book.List()
		if err != nil {
			return err
		}
		linkConfig, _, linkEnabled, err := loadOptionalLinkConfig(resolvedStateDir, "")
		if err != nil {
			return err
		}
		if outputJSON {
			rows := make([]addressRow, 0, len(entries))
			for _, entry := range entries {
				rows = append(rows, addressRow{Entry: entry, Kind: classifyAddress(entry.URL)})
			}
			return json.NewEncoder(os.Stdout).Encode(rows)
		}
		linkRow := ""
		if linkEnabled {
			linkRow = "Mewla Link"
			if domains := strings.Join(link.RelayDomains(linkConfig), ", "); domains != "" {
				linkRow += " (" + domains + ")"
			}
		}
		return writeAddressList(os.Stdout, entries, linkRow)
	default:
		return fmt.Errorf("unknown address command: %s", args[0])
	}
}

type addressRow struct {
	addressbook.Entry
	Kind addressKind `json:"kind"`
}

// addressSourceLabel says how Mewla knows an address.
func addressSourceLabel(source addressbook.Source) string {
	switch source {
	case addressbook.SourceDiscovered:
		return "detected"
	case addressbook.SourceManual:
		return "added"
	case addressbook.SourceVerified:
		return "seen"
	}
	return string(source)
}

// writeAddressList prints HTTPS first and this computer last, the order a
// phone prefers them.
func writeAddressList(w io.Writer, entries []addressbook.Entry, linkRow string) error {
	sorted := append([]addressbook.Entry(nil), entries...)
	rank := func(entry addressbook.Entry) int {
		if r := classifyAddress(entry.URL).phoneRank(); r >= 0 {
			return r
		}
		return 3
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if rank(sorted[i]) != rank(sorted[j]) {
			return rank(sorted[i]) < rank(sorted[j])
		}
		return sorted[i].LastSeenAt.After(sorted[j].LastSeenAt)
	})
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "ADDRESS\tKIND\tFROM\tLAST SEEN")
	for _, entry := range sorted {
		fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\n",
			entry.URL,
			classifyAddress(entry.URL).label(),
			addressSourceLabel(entry.Source),
			entry.LastSeenAt.Local().Format("2006-01-02 15:04"),
		)
	}
	if linkRow != "" {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", linkRow, "Mewla Link", "link.json", "—")
	}
	return table.Flush()
}

func resolveCLIStateDir(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	dir, err := auth.DefaultStorageDir()
	if err != nil {
		return statedir.DirName
	}
	return dir
}
