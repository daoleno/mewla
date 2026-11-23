//go:build !linux

package userunit

import "time"

// Available: no user manager outside Linux.
func Available() bool { return false }

// EnclosingSlice: no Session slices outside Linux.
func EnclosingSlice() string { return "" }

// PlaceInScope: no user manager outside Linux.
func PlaceInScope(int, string, string) (string, error) { return "", ErrUnsupported }

// StopSession: no user manager outside Linux.
func StopSession(string, time.Duration) ([]string, error) { return nil, ErrUnsupported }
