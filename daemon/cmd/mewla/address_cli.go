package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
)

func runAddressCommand(args []string, stderr interface{ Write([]byte) (int, error) }) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen address <add|remove|list> [flags]")
		return flag.ErrHelp
	}
	fs := flag.NewFlagSet("zen address "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	var outputJSON bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.zen)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	book, err := addressbook.New(resolveCLIStateDir(stateDir))
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: zen address add <https-url>")
		}
		entry, err := book.Add(fs.Arg(0), addressbook.SourceManual)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, entry.URL)
		return nil
	case "remove":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: zen address remove <https-url>")
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
		if outputJSON {
			return json.NewEncoder(os.Stdout).Encode(entries)
		}
		for _, entry := range entries {
			fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", entry.URL, entry.Source, entry.LastSeenAt.Format("2006-01-02 15:04:05Z07:00"))
		}
		return nil
	default:
		return fmt.Errorf("unknown address command: %s", args[0])
	}
}

func resolveCLIStateDir(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	dir, err := auth.DefaultStorageDir()
	if err != nil {
		return ".zen"
	}
	return dir
}
