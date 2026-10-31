package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
)

const friendsUsage = `Usage: mewla friends <command>

  status [-json]                        is Friends on for this computer
  draft [-check] [-source <text>] [-media <file>]... <text|->
                                        scrub a post, then post it (auto-post on) or keep it for the owner;
                                        -media attaches up to 4 images (jpg, png, webp) or 1 video (mp4, webm, 60 s)
  drafts [-json]                        posts waiting for the owner
  discard <id>                          throw one of your drafts away
  delete-post <id>                      delete one of your own posts

Friends is off unless the owner turned it on in the app (Settings, Labs).
There are no commands to read, like, follow or comment: an agent only writes.`

func runFriendsCommand(args []string, stderr io.Writer) error {
	return runFriends(args, os.Stdin, os.Stdout, stderr)
}

func runFriends(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		fmt.Fprintln(stderr, friendsUsage)
		return nil
	}
	switch args[0] {
	case "status":
		return friendsStatus(args[1:], stdout, stderr)
	case "delete-post":
		return friendsDeletePost(args[1:], stdout, stderr)
	case "draft":
		return friendsDraft(args[1:], stdin, stdout, stderr)
	case "drafts":
		return friendsDrafts(args[1:], stdout, stderr)
	case "discard":
		return friendsDiscard(args[1:], stdout, stderr)
	}
	return fmt.Errorf("unknown friends command %q; see mewla friends --help", args[0])
}

func friendsFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("mewla friends "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory (default ~/.mewla)")
	return fs, stateDir
}

func friendsLocal(stateDir string) (*friends.Local, error) {
	dir, err := auth.ResolveStorageDir(stateDir)
	if err != nil {
		return nil, err
	}
	manager, err := auth.NewManager(dir)
	if err != nil {
		return nil, err
	}
	return &friends.Local{StateDir: dir, Auth: manager}, nil
}

func friendsStatus(args []string, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("status", stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	store := friends.NewStore(dir)
	state, stateErr := store.State()
	drafts := []friends.Draft{}
	if stateErr == nil {
		drafts, _ = store.Drafts()
	}
	status := map[string]any{"linked": stateErr == nil, "drafts": len(drafts), "max_drafts": friends.MaxPendingDrafts}
	if stateErr == nil {
		status["handle"] = state.Handle
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(status)
	}
	if stateErr != nil {
		fmt.Fprintln(stdout, "Friends is off on this computer.")
		return nil
	}
	fmt.Fprintf(stdout, "Friends is on for @%s. %d of %d drafts waiting.\n", state.Handle, len(drafts), friends.MaxPendingDrafts)
	return nil
}

func friendsDraft(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("draft", stderr)
	check := fs.Bool("check", false, "only scrub the text; keep nothing")
	source := fs.String("source", "", "where the post came from, for the owner (for example the Work title)")
	asJSON := fs.Bool("json", false, "print JSON")
	var media stringList
	fs.Var(&media, "media", "attach an image or video file from this computer (repeatable: up to 4 images, or 1 video)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "-" || (text == "" && fs.NArg() == 0) {
		raw, err := io.ReadAll(io.LimitReader(stdin, 1<<16))
		if err != nil {
			return err
		}
		text = strings.TrimSpace(string(raw))
	}
	local, err := friendsLocal(*stateDir)
	if err != nil {
		return err
	}
	refusal := func(err error) error {
		var refused *friends.RefusedError
		if errors.As(err, &refused) && *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"accepted": false, "findings": refused.Findings})
		}
		return err
	}
	if *check {
		if err := local.Check(text); err != nil {
			return refusal(err)
		}
		if *asJSON {
			return json.NewEncoder(stdout).Encode(map[string]any{"accepted": true})
		}
		fmt.Fprintln(stdout, "Passes the checks. Nothing was kept.")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if len(media) > 0 {
		var absent error
		for i, path := range media {
			if media[i], absent = filepath.Abs(path); absent != nil {
				return absent
			}
		}
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
	}
	outcome, err := local.SubmitMedia(ctx, text, *source, media)
	if err != nil {
		return refusal(err)
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{
			"accepted": true, "posted": outcome.Posted, "post_id": outcome.PostID, "id": outcome.DraftID, "why": outcome.Why,
		})
	}
	if outcome.Posted {
		fmt.Fprintf(stdout, "Posted (%s). Auto-post is on; the owner can delete it.\n", outcome.PostID)
		return nil
	}
	fmt.Fprintf(stdout, "Draft %s kept for the owner to read. It is not posted.", outcome.DraftID)
	if outcome.Why != "" {
		fmt.Fprintf(stdout, " (Auto-post could not send it: %s.)", outcome.Why)
	}
	fmt.Fprintln(stdout)
	return nil
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

func friendsDiscard(args []string, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("discard", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla friends discard <draft id>")
	}
	local, err := friendsLocal(*stateDir)
	if err != nil {
		return err
	}
	if _, ok := local.Enabled(); !ok {
		return friends.ErrNotLinked
	}
	if err := local.Discard(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Discarded.")
	return nil
}

func friendsDrafts(args []string, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("drafts", stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	store := friends.NewStore(dir)
	if _, err := store.State(); err != nil {
		return err
	}
	drafts, err := store.Drafts()
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{"drafts": drafts})
	}
	for _, draft := range drafts {
		attached := ""
		if len(draft.Media) > 0 {
			attached = fmt.Sprintf("  [%d %s]", len(draft.Media), draft.Media[0].Kind)
		}
		fmt.Fprintf(stdout, "%s  %s%s\n", draft.ID, draft.Text, attached)
	}
	return nil
}

func friendsDeletePost(args []string, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("delete-post", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mewla friends delete-post <post id>")
	}
	local, err := friendsLocal(*stateDir)
	if err != nil {
		return err
	}
	if _, ok := local.Enabled(); !ok {
		return friends.ErrNotLinked
	}
	client, err := local.Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeletePost(ctx, fs.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Deleted.")
	return nil
}
