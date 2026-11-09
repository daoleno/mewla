//go:build unix

package platform

import "errors"

// CurrentUser names the user for a Windows task; Unix has no use for it.
func CurrentUser() (account, sid string, err error) {
	return "", "", errors.New("CurrentUser is only used on Windows")
}
