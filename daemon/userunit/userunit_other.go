//go:build !linux

package userunit

import "time"

// Available: no user manager outside Linux.
func Available() bool { return false }

// NestingSliceOf: no Session slices outside Linux.
func NestingSliceOf(func(string) bool) string { return "" }

// PlaceInScope: no user manager outside Linux.
func PlaceInScope(int, string, string) (string, error) { return "", ErrUnsupported }

// StopSession: no user manager outside Linux.
func StopSession(string, time.Duration) ([]string, error) { return nil, ErrUnsupported }
