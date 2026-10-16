//go:build !darwin

package screen

import (
	"os/exec"
	"time"
)

func keepAwakeCommand() *exec.Cmd { return nil }

func keepDisplayCommand() *exec.Cmd { return nil }

func WakeDisplay() {}

// wakeDeclares is how long WakeDisplay may declare activity; here, none.
var wakeDeclares time.Duration

func readPower() *Power { return nil }
