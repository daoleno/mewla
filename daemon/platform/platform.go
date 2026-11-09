// Package platform holds the operating-system primitives the daemon needs and
// that differ between Unix and Windows: file locks, detached processes,
// process trees, replacing a running executable, directory durability and
// starting at login. Each primitive has one portable signature; the files
// named _unix.go and _windows.go implement it.
package platform
