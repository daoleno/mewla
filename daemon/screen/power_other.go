//go:build !darwin

package screen

import "os/exec"

func keepAwakeCommand() *exec.Cmd { return nil }

func keepDisplayCommand() *exec.Cmd { return nil }

func WakeDisplay() {}

func readPower() *Power { return nil }
