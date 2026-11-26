//go:build !darwin

package main

import "errors"

// processPath: only Mewla for Mac runs the daemon under an app's host.
func processPath(int) (string, error) { return "", errors.New("no app host on this system") }
