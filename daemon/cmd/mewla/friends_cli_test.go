package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/friends"
)

func runFriendsForTest(t *testing.T, stateDir, stdin string, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := runFriends(append(args[:1:1], append([]string{"-state-dir", stateDir}, args[1:]...)...), strings.NewReader(stdin), &stdout, &stderr)
	return stdout.String(), err
}

func TestFriendsDraftScrubsAndNeedsALinkToKeep(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEWLA_FRIENDS_PROJECT_ROOTS", t.TempDir())

	out, err := runFriendsForTest(t, dir, "", "status", "-json")
	if err != nil || !strings.Contains(out, `"linked":false`) {
		t.Fatalf("status = %q, %v", out, err)
	}
	// Unlinked: the checker works, keeping does not.
	if _, err := runFriendsForTest(t, dir, "", "draft", "-check", "Deleted a lot of code today."); err != nil {
		t.Fatalf("check of a plain post: %v", err)
	}
	if _, err := runFriendsForTest(t, dir, "", "draft", "Deleted a lot of code today."); err != friends.ErrNotLinked {
		t.Fatalf("keeping while unlinked = %v", err)
	}
	if _, err := runFriendsForTest(t, dir, "", "draft", "-check", "Moved it to 192.0.2.7 today."); err == nil || !strings.Contains(err.Error(), "IP address") {
		t.Fatalf("leaky draft = %v", err)
	}

	// Linked: a clean draft is kept; a leaky one is not.
	if err := friends.NewStore(dir).SaveState(friends.State{Handle: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runFriendsForTest(t, dir, "Short and clean post from stdin.", "draft", "-source", "A work title", "-"); err != nil {
		t.Fatalf("keep: %v", err)
	}
	if _, err := runFriendsForTest(t, dir, "", "draft", "mail bob@example.com about it"); err == nil {
		t.Fatal("a draft with an email was kept")
	}
	out, _ = runFriendsForTest(t, dir, "", "drafts", "-json")
	var listed struct{ Drafts []friends.Draft }
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed.Drafts) != 1 || listed.Drafts[0].Source != "A work title" {
		t.Fatalf("drafts = %q, %v", out, err)
	}
}

func TestFriendsHasNoSocialCommands(t *testing.T) {
	for _, name := range []string{"like", "comment", "follow", "bookmark", "feed", "post", "timeline", "link", "block", "report", "unfollow"} {
		if _, err := runFriendsForTest(t, t.TempDir(), "", name); err == nil || !strings.Contains(err.Error(), "unknown friends command") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFriendsUsageOffersNoSocialAbilities(t *testing.T) {
	for _, word := range []string{"like", "comment ", "follow ", "bookmark"} {
		for _, line := range strings.Split(friendsUsage, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "There are no commands") || strings.HasPrefix(strings.TrimSpace(line), "like, follow") {
				continue
			}
			if strings.HasPrefix(line, "  ") && strings.Contains(line, word) {
				t.Errorf("usage offers %q: %s", word, line)
			}
		}
	}
}
