package envcompat

import (
	"os"
	"reflect"
	"testing"
)

func TestNormalizeMirrorsLegacyAndCanonicalWins(t *testing.T) {
	t.Setenv("ZEN_ONLY_LEGACY", "legacy")
	t.Setenv("MEWLA_ONLY_CANONICAL", "canonical")
	t.Setenv("ZEN_BOTH", "old")
	t.Setenv("MEWLA_BOTH", "new")
	t.Cleanup(func() {
		for _, key := range []string{"MEWLA_ONLY_LEGACY", "ZEN_ONLY_CANONICAL"} {
			_ = os.Unsetenv(key)
		}
	})

	Normalize()

	for key, want := range map[string]string{
		"MEWLA_ONLY_LEGACY":    "legacy",
		"ZEN_ONLY_LEGACY":      "legacy",
		"MEWLA_ONLY_CANONICAL": "canonical",
		"ZEN_ONLY_CANONICAL":   "canonical",
		"MEWLA_BOTH":           "new",
		"ZEN_BOTH":             "new",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestMirrorMakesChildEnvironmentConsistent(t *testing.T) {
	got := Mirror([]string{
		"PATH=/bin",
		"ZEN_WORKER_ID=%9",
		"MEWLA_WORKER_ID=%1",
		"ZEN_STATE_DIR=/home/u/.zen",
		"MEWLA_BUILD_TMPDIR=/tmp/b",
		"PATH=/usr/bin",
	})
	want := []string{
		"PATH=/usr/bin",
		"ZEN_WORKER_ID=%1",
		"MEWLA_WORKER_ID=%1",
		"ZEN_STATE_DIR=/home/u/.zen",
		"MEWLA_BUILD_TMPDIR=/tmp/b",
		"MEWLA_STATE_DIR=/home/u/.zen",
		"ZEN_BUILD_TMPDIR=/tmp/b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Mirror =\n%q\nwant\n%q", got, want)
	}
}

func TestPrefixHelpers(t *testing.T) {
	if !HasPrefix("ZEN_WORKER_ID", "WORKER_") || !HasPrefix("MEWLA_WORKER_ID", "WORKER_") || HasPrefix("WORKER_ID", "WORKER_") {
		t.Fatal("HasPrefix mismatch")
	}
	if !Is("ZEN_STATE_DIR", "MEWLA_STATE_DIR") || !Is("MEWLA_STATE_DIR", "MEWLA_STATE_DIR") || Is("STATE_DIR", "MEWLA_STATE_DIR") {
		t.Fatal("Is mismatch")
	}
}
