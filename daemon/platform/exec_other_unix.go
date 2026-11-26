//go:build unix && !darwin

package platform

import "syscall"

func execve(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
