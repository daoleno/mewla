// Package envcompat keeps the legacy ZEN_* environment working beside the
// canonical MEWLA_* names. MEWLA_* always wins; both names carry the same
// value afterwards so legacy scripts and child processes read one truth.
package envcompat

import (
	"os"
	"sort"
	"strings"
)

const (
	Prefix       = "MEWLA_"
	LegacyPrefix = "ZEN_"
)

// Legacy returns the ZEN_* name for a MEWLA_* key, or "" for other keys.
func Legacy(key string) string {
	if rest, ok := strings.CutPrefix(key, Prefix); ok {
		return LegacyPrefix + rest
	}
	return ""
}

// Canonical returns the MEWLA_* name for a MEWLA_* or ZEN_* key, or "".
func Canonical(key string) string {
	if strings.HasPrefix(key, Prefix) {
		return key
	}
	if rest, ok := strings.CutPrefix(key, LegacyPrefix); ok {
		return Prefix + rest
	}
	return ""
}

// HasPrefix reports whether key starts with MEWLA_<suffix> or ZEN_<suffix>.
func HasPrefix(key, suffix string) bool {
	return strings.HasPrefix(key, Prefix+suffix) || strings.HasPrefix(key, LegacyPrefix+suffix)
}

// Is reports whether key is the MEWLA_* name or its ZEN_* alias.
func Is(key, canonical string) bool {
	return key == canonical || (Legacy(canonical) != "" && key == Legacy(canonical))
}

// Normalize mirrors the process environment in place. Call it first in every
// entrypoint, before any package reads MEWLA_* variables.
func Normalize() {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range values {
		canonical := Canonical(key)
		if canonical == "" || canonical == key {
			continue
		}
		if _, ok := values[canonical]; !ok {
			_ = os.Setenv(canonical, value)
			values[canonical] = value
		}
	}
	for key, value := range values {
		if legacy := Legacy(key); legacy != "" && values[legacy] != value {
			_ = os.Setenv(legacy, value)
		}
	}
}

// Mirror returns entries ("KEY=value") with every MEWLA_*/ZEN_* pair made
// consistent. Later entries override earlier ones, as exec does, and a MEWLA_*
// value overrides its ZEN_* alias. Order of first appearance is preserved.
func Mirror(entries []string) []string {
	order := make([]string, 0, len(entries))
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, seen := values[key]; !seen {
			order = append(order, key)
		}
		values[key] = value
	}
	var added []string
	for _, key := range order {
		canonical := Canonical(key)
		if canonical == "" {
			continue
		}
		value, ok := values[canonical]
		if !ok {
			value = values[key]
			values[canonical] = value
			added = append(added, canonical)
		}
		legacy := Legacy(canonical)
		if _, seen := values[legacy]; !seen {
			added = append(added, legacy)
		}
		values[legacy] = value
	}
	sort.Strings(added)
	result := make([]string, 0, len(order)+len(added))
	for _, key := range append(order, added...) {
		result = append(result, key+"="+values[key])
	}
	return result
}
