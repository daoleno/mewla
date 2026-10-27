package calendar

import (
	"strings"
	"testing"
)

func TestResultPreviewIsTheFirstReadableLine(t *testing.T) {
	cases := map[string]string{
		"今天的稿子好了，两条 X 和一条小红书。\n\n## X1\n\nbody":        "今天的稿子好了，两条 X 和一条小红书。",
		"## Daily briefing\n\n- three papers":          "Daily briefing",
		"![cover](/tmp/a.png)\n\n---\n\n- First point": "First point",
		"\n\n":                   "",
		strings.Repeat("长", 200): strings.Repeat("长", maxResultPreviewRunes-1) + "…",
	}
	for result, want := range cases {
		if got := ResultPreview(result); got != want {
			t.Fatalf("ResultPreview(%q) = %q, want %q", result, got, want)
		}
	}
}

func TestStaleItemFailureIsDropped(t *testing.T) {
	scheduled := WithoutStaleFailure(Item{Status: StatusScheduled, FailureReason: "Linked Work/agent is no longer observable after restart"})
	if scheduled.FailureReason != "" {
		t.Fatalf("scheduled item kept %q", scheduled.FailureReason)
	}
	failed := WithoutStaleFailure(Item{Status: StatusFailed, FailureReason: "Running calendar item has no durable run record."})
	if failed.FailureReason == "" {
		t.Fatal("a failed item lost its own failure")
	}
}
