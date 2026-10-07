package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/statedir"
)

// migrateLegacyState moves ~/.zen to ~/.mewla before any command resolves
// state paths. An explicit MEWLA_STATE_DIR (or legacy ZEN_STATE_DIR) owns its
// own root, so only the daemon start or `mewla state migrate` act then.
// Routine outcomes stay quiet so CLI output that scripts parse is unchanged;
// daemon start, setup and doctor also surface deferrals and conflicts.
func migrateLegacyState(args []string, stderr io.Writer) {
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
	}
	if command == "state" || command == "dsh-session" || command == "help" {
		return
	}
	for _, arg := range args {
		// A help probe (the installer's --help check) must not move state.
		if arg == "-h" || arg == "-help" || arg == "--help" {
			return
		}
	}
	daemonStart := command == "" || command == "serve"
	if strings.TrimSpace(os.Getenv("MEWLA_STATE_DIR")) != "" && !daemonStart {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	result, err := statedir.Migrate(home)
	if err != nil {
		fmt.Fprintf(stderr, "mewla: state migration: %v\n", err)
		return
	}
	loud := daemonStart || command == "setup" || command == "doctor"
	if result.Status == statedir.StatusMigrated || (loud && result.Notable()) {
		fmt.Fprintln(stderr, statedir.Notice(result))
	}
}

func runStateCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 || args[0] != "migrate" {
		fmt.Fprintln(stderr, "Usage: mewla state migrate")
		fmt.Fprintln(stderr, "  Move the legacy ~/.zen state root to ~/.mewla and leave ~/.zen as a link.")
		fmt.Fprintln(stderr, "  Refuses while a daemon still holds ~/.zen; idempotent once migrated.")
		return fmt.Errorf("usage: mewla state migrate")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	result, err := statedir.Migrate(home)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "status: %s\nroot: %s\n", result.Status, result.Root)
	if result.Message != "" {
		fmt.Fprintf(stdout, "note: %s\n", result.Message)
	}
	if result.Status == statedir.StatusDeferred || result.Status == statedir.StatusConflict || result.Status == statedir.StatusUnsupported {
		return fmt.Errorf("state root not migrated: %s", result.Status)
	}
	return nil
}
