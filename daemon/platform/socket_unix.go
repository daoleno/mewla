//go:build unix

package platform

func connRefused(error) bool { return false }
