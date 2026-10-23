package screen

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// keepAwakeCommand is `caffeinate -i -s`: no idle or system sleep (the latter
// only on AC power, as macOS defines it), while the display may still sleep.
// -w ties the hold to this daemon, so a crashed daemon cannot leave it held.
func keepAwakeCommand() *exec.Cmd {
	return procspawn.Command("caffeinate keep-awake", "caffeinate", "-i", "-s", "-w", strconv.Itoa(os.Getpid()))
}

// keepDisplayCommand is `caffeinate -d`: the display stays on while an agent
// uses the screen, also on battery, and the hold ends with this daemon.
func keepDisplayCommand() *exec.Cmd {
	return procspawn.Command("caffeinate display", "caffeinate", "-d", "-w", strconv.Itoa(os.Getpid()))
}

// WakeDisplay declares user activity so a sleeping display turns on for a
// viewer that just connected; a locked Mac then shows its login window.
func WakeDisplay() {
	cmd := procspawn.Command("caffeinate wake", "caffeinate", "-u", "-t", "2")
	if cmd.Start() == nil {
		displayWoke.Store(time.Now().UnixNano())
		go func() { _ = cmd.Wait() }()
	}
}

func readPower() *Power {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := procspawn.CommandContext(ctx, "pmset batt", "pmset", "-g", "batt").Output()
	if err != nil {
		return nil
	}
	power := parsePMSetBattery(string(out))
	if clamshell, err := procspawn.CommandContext(ctx, "ioreg clamshell", "ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output(); err == nil {
		power.LidClosed = parseClamshell(string(clamshell))
	}
	return power
}
