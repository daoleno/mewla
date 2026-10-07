//go:build linux

package statedir

import (
	"errors"

	"golang.org/x/sys/unix"
)

func exchange(a, b string) error {
	err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return errExchangeUnsupported
	}
	return err
}
