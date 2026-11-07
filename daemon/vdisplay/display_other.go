//go:build !linux

package vdisplay

import "errors"

// Capability reports that virtual displays are a Linux feature.
func Capability() error { return errors.New("virtual displays are only available on Linux") }
