// Package userunit keeps a Session's processes in systemd units of the
// person's user manager, so that ending the Session ends all of them: what
// its agent started with setsid or env -i, a tmux pane (tmux puts its panes
// in a scope of the caller's slice), a nested Mewla's host unit.
//
// A Session is a slice named after its resource unit
// (mewla-worker-<owner>-<token>.slice). Each program the session host starts
// for it runs in a scope of its own inside that slice; ending the Session
// stops the slice and with it everything in it. A Mewla started inside a
// Session (a nested daemon) finds that slice in its own cgroup and puts its
// host unit and its own Sessions under it.
//
// Only Linux has this; elsewhere every call reports ErrUnsupported.
package userunit

import (
	"errors"
	"strings"
)

// ErrUnsupported: there is no systemd user manager to reach here.
var ErrUnsupported = errors.New("no systemd user manager")

const (
	unitPrefix  = "mewla-worker-"
	scopeSuffix = ".scope"
	sliceSuffix = ".slice"
	tokenLen    = 32
)

// Token is the 32 hex digits that name a Session in its resource unit
// mewla-worker-<owner>-<token>.scope, "" when unit is not one.
func Token(unit string) string {
	if !strings.HasPrefix(unit, unitPrefix) || !strings.HasSuffix(unit, scopeSuffix) {
		return ""
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(unit, unitPrefix), scopeSuffix)
	owner, token, ok := strings.Cut(rest, "-")
	if !ok || owner == "" || !lowerAlnum(owner) || len(token) != tokenLen || !lowerHex(token) {
		return ""
	}
	return token
}

// SessionSlice is the slice of the Session whose resource unit is unit:
// mewla-worker-<owner>-<token>.slice, or <enclosing>-<token>.slice when this
// Mewla runs inside another one's Session slice enclosing, so that ending
// the outer Session ends this one's too. "" when unit is not a resource unit.
func SessionSlice(unit, enclosing string) string {
	token := Token(unit)
	if token == "" {
		return ""
	}
	if enclosing != "" {
		return strings.TrimSuffix(enclosing, sliceSuffix) + "-" + token + sliceSuffix
	}
	return strings.TrimSuffix(unit, scopeSuffix) + sliceSuffix
}

// sessionSlicePattern matches the slice of the Session with token, whether
// it sits at the top or inside another Session's slice.
func sessionSlicePattern(token string) string {
	return unitPrefix + "*" + token + sliceSuffix
}

// NestingSlice is the Session slice a Mewla nests in, given its cgroup text:
// the innermost Session slice that is not one of its own Sessions (own
// reports whether a Session token is one of this Mewla's). A daemon started
// or restarted from a shell inside one of its own Sessions must not nest
// there: its host and every Session would go under that one, and that
// Session's end would end them all.
func NestingSlice(cgroup string, own func(token string) bool) string {
	for _, line := range strings.Split(cgroup, "\n") {
		path, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		parts := strings.Split(path, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if isSessionSlice(parts[i]) && (own == nil || !own(sliceToken(parts[i]))) {
				return parts[i]
			}
		}
	}
	return ""
}

// SessionTokens lists the tokens of the Session slices in a cgroup text,
// outermost first: one for a Session's process, more when a Mewla nested in a
// Session runs Sessions of its own.
func SessionTokens(cgroup string) []string {
	var tokens []string
	for _, line := range strings.Split(cgroup, "\n") {
		path, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		for _, part := range strings.Split(path, "/") {
			if isSessionSlice(part) {
				tokens = append(tokens, sliceToken(part))
			}
		}
	}
	return tokens
}

// sliceToken is the token of the Session a Session slice is: its last one.
func sliceToken(slice string) string {
	name := strings.TrimSuffix(slice, sliceSuffix)
	return name[strings.LastIndex(name, "-")+1:]
}

func isSessionSlice(name string) bool {
	if !strings.HasPrefix(name, unitPrefix) || !strings.HasSuffix(name, sliceSuffix) {
		return false
	}
	// mewla-worker-<owner>-<token>[-<token>...].slice: at least one token
	// after the owner; mewla-worker-<owner>.slice is the parent of them all.
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, unitPrefix), sliceSuffix), "-")
	if len(parts) < 2 || parts[0] == "" || !lowerAlnum(parts[0]) {
		return false
	}
	for _, token := range parts[1:] {
		if len(token) != tokenLen || !lowerHex(token) {
			return false
		}
	}
	return true
}

func lowerAlnum(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func lowerHex(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'f') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
