package doctor

import (
	"context"
	"strings"
)

// h264Encoders are the ffmpeg encoders Screen's video uses (package
// screencast): VA-API, x264 and OpenH264.
var h264Encoders = map[string]bool{"h264_vaapi": true, "libx264": true, "libopenh264": true}

func (e env) checkScreen() *ScreenCheck {
	if e.opts.GOOS != "linux" {
		return nil
	}
	check := &ScreenCheck{Status: StatusOK, Summary: "agents' displays and their video are ready to watch"}
	if _, err := e.opts.LookPath("Xvnc"); err != nil {
		check.Missing = append(check.Missing, "TigerVNC's Xvnc")
	}
	if _, err := e.opts.LookPath("xauth"); err != nil {
		check.Missing = append(check.Missing, "xauth")
	}
	if ffmpeg, err := e.opts.LookPath("ffmpeg"); err != nil {
		check.Missing = append(check.Missing, "ffmpeg")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
		out, err := e.opts.RunCommand(ctx, ffmpeg, "-hide_banner", "-encoders")
		cancel()
		if err != nil || !hasH264Encoder(string(out)) {
			check.Missing = append(check.Missing, "an ffmpeg with an H.264 encoder (libx264, libopenh264 or VA-API)")
		}
	}
	if len(check.Missing) > 0 {
		check.Status = StatusWarn
		check.Summary = "Screen cannot show agents' displays without " + joinAnd(check.Missing) + "; install them (agents work without)"
	}
	return check
}

// hasH264Encoder reads `ffmpeg -encoders`: one encoder a line, its flags then
// its name.
func hasH264Encoder(list string) bool {
	for line := range strings.SplitSeq(list, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && h264Encoders[fields[1]] {
			return true
		}
	}
	return false
}

func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
