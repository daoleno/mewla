package server

import (
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

func TestLiveSessionTitleIsTheAppsRowTitle(t *testing.T) {
	cases := []struct {
		worker classifier.Worker
		want   string
	}{
		{classifier.Worker{ID: "%7", Name: "Hello writer (mh-17b3337d43f38037916f0e5b)", Cwd: "/w/site"}, "Hello writer"},
		{classifier.Worker{ID: "mh-17b3337d43f38037916f0e5b", Name: "mh-17b3337d43f38037916f0e5b", Cwd: "/w/mewla-site"}, "mewla-site"},
		{classifier.Worker{ID: "%3", Name: "Claude Code", Cwd: "/home/x/perpetuo"}, "perpetuo"},
		{classifier.Worker{ID: "%3", Name: "", Cwd: ""}, "Session"},
	}
	for _, tc := range cases {
		worker := tc.worker
		if got := liveSessionTitle(&worker); got != tc.want {
			t.Errorf("liveSessionTitle(%q in %q) = %q, want %q", worker.Name, worker.Cwd, got, tc.want)
		}
	}
}

func TestLiveLineIsOnlyWhatTheSessionReported(t *testing.T) {
	screen := classifier.Worker{Summary: "⏺ Bash(ping -c 50 127.0.0.1)"}
	if got := liveReportedLine(&screen); got != "" {
		t.Fatalf("the terminal's text must not show, got %q", got)
	}
	reported := classifier.Worker{Summary: "Building the comparison table", Phase: "working"}
	if got := liveReportedLine(&reported); got != "Building the comparison table" {
		t.Fatalf("got %q", got)
	}
}
