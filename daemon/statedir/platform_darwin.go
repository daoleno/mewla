//go:build darwin

package statedir

import (
	"errors"

	"golang.org/x/sys/unix"
)

func exchange(a, b string) error {
	err := unix.RenamexNp(a, b, unix.RENAME_SWAP)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) {
		return errExchangeUnsupported
	}
	return err
}
