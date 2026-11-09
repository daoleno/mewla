//go:build windows

package platform

import "golang.org/x/sys/windows"

// CurrentUser is this process's user as DOMAIN\name and as a SID string.
func CurrentUser() (account, sid string, err error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", err
	}
	name, domain, _, err := user.User.Sid.LookupAccount("")
	if err != nil {
		return "", "", err
	}
	return domain + `\` + name, user.User.Sid.String(), nil
}
