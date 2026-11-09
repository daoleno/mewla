//go:build unix

package launchsh

import (
	"os/exec"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
)

func command(argv, env []string) (*exec.Cmd, error) {
	path, err := interp.LookPathDir("", expand.ListEnviron(env...), argv[0])
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Args = argv
	return cmd, nil
}
