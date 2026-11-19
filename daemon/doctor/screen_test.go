package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// On Linux, doctor says what watching agents' displays needs and is missing:
// Xvnc and xauth, and an ffmpeg that has an H.264 encoder.
func TestScreenSaysWhatWatchingAgentsNeeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts")
	}
	tools := func(t *testing.T, encoders string) string {
		dir := t.TempDir()
		for name, body := range map[string]string{
			"Xvnc":   "exit 0",
			"xauth":  "exit 0",
			"ffmpeg": "printf 'Encoders:\\n V....D mpeg4                MPEG-4 part 2\\n" + encoders + "'",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	none := newEnv(Options{GOOS: "linux", PathEnv: t.TempDir()}).checkScreen()
	if none.Status != StatusWarn || !slices.Equal(none.Missing, []string{"TigerVNC's Xvnc", "xauth", "ffmpeg"}) ||
		!strings.Contains(none.Summary, "without TigerVNC's Xvnc, xauth and ffmpeg") {
		t.Fatalf("nothing installed: %+v", none)
	}

	ready := newEnv(Options{GOOS: "linux", PathEnv: tools(t, " V....D libx264              libx264 H.264 / AVC\\n")}).checkScreen()
	if ready.Status != StatusOK || len(ready.Missing) != 0 {
		t.Fatalf("all installed: %+v", ready)
	}

	noH264 := newEnv(Options{GOOS: "linux", PathEnv: tools(t, "")}).checkScreen()
	if noH264.Status != StatusWarn || len(noH264.Missing) != 1 || !strings.Contains(noH264.Missing[0], "H.264 encoder") {
		t.Fatalf("an ffmpeg without H.264: %+v", noH264)
	}

	if mac := newEnv(Options{GOOS: "darwin", PathEnv: t.TempDir()}).checkScreen(); mac != nil {
		t.Fatalf("a Mac needs nothing installed for Screen: %+v", mac)
	}
}
