package main

import (
	"io"
	"strings"
)

// runCLI runs one mewla command line. Help goes to stdout and exits 0, as a
// person asking for it expects: `mewla worker --help > help.txt` and
// PowerShell's `2>&1` capture (Windows PowerShell 5.1 turns every stderr line
// of a native program into an error record) both see plain text. Every
// command prints its help, from its flag set or its own usage text, to the
// writer it is given, so the choice is made here, once, for all of them. An
// error still goes to stderr: main prints it, and a usage shown because of a
// mistake (an unknown flag, a missing argument) goes with it.
func runCLI(args []string, stdout, stderr io.Writer) error {
	if helpRequested(args) {
		return run(args, stdout)
	}
	return run(args, stderr)
}

// helpRequested reports whether args ask for help: -h, -help or --help before
// a `--`; `help` as the last of only command words (`mewla help`, `mewla
// worker help`); or a command group named with nothing after it, which
// prints its usage (`mewla worker`, `mewla brain work`).
func helpRequested(args []string) bool {
	if len(args) == 0 {
		return false // bare mewla is the first-run flow
	}
	switch args[0] {
	case "gcloud", "aws", "gws":
		// Everything after the command is the vendor CLI's, except
		// mewla's own --help right after it.
		return len(args) == 1 || args[1] == "--help"
	case "sh", "dsh-session", "agent-event":
		// Internal commands whose arguments are scripts and payloads.
		return false
	}
	words := 0
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "-h", "-help", "--help":
			return true
		}
		if strings.HasPrefix(arg, "-") {
			words = -1
		}
		if words >= 0 {
			words++
		}
	}
	if words != len(args) {
		return false
	}
	if args[len(args)-1] == "help" {
		return true
	}
	return commandGroups[strings.Join(args, " ")]
}

// commandGroups are the commands that print their usage when named with no
// subcommand. A new group adds itself here.
var commandGroups = map[string]bool{
	"address":        true,
	"agent-use":      true,
	"agents":         true,
	"agents mcp":     true,
	"boot":           true,
	"brain":          true,
	"brain work":     true,
	"calendar":       true,
	"computers":      true,
	"connections":    true,
	"devices":        true,
	"memory":         true,
	"providers":      true,
	"screen":         true,
	"service":        true,
	"service tunnel": true,
	"watch":          true,
	"worker":         true,
}
