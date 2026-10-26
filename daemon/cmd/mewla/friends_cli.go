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

  status [-json]                        is Friends set up on this computer
  link -code <code> [-name <name>]      link this computer to the owner's Friends account
  draft [-check] [-source <text>] <text|->   scrub a post and keep it for the owner
  drafts [-json]                        posts waiting for the owner

Friends is off unless the owner turned it on in the app (Settings, Labs).
There are no commands to read, like, follow or comment: an agent only writes
drafts.`

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
	case "link":
		return friendsLink(args[1:], stdout, stderr)
	case "draft":
		return friendsDraft(args[1:], stdin, stdout, stderr)
	case "drafts":
		return friendsDrafts(args[1:], stdout, stderr)
	}
	return fmt.Errorf("unknown friends command %q; see mewla friends --help", args[0])
}

func friendsFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("mewla friends "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory (default ~/.mewla)")
	return fs, stateDir
}

func friendsServiceURL() string {
	if value := strings.TrimSpace(os.Getenv("MEWLA_FRIENDS_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return friends.DefaultServiceURL
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
	link, linkErr := store.Link()
	drafts, _ := store.Drafts()
	status := map[string]any{"linked": linkErr == nil, "drafts": len(drafts), "max_drafts": friends.MaxPendingDrafts}
	if linkErr == nil {
		status["handle"] = link.Handle
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(status)
	}
	if linkErr != nil {
		fmt.Fprintln(stdout, "Friends is off on this computer.")
		return nil
	}
	fmt.Fprintf(stdout, "Friends is on for @%s. %d of %d drafts waiting.\n", link.Handle, len(drafts), friends.MaxPendingDrafts)
	return nil
}

func friendsLink(args []string, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("link", stderr)
	code := fs.String("code", "", "link code from the app")
	name := fs.String("name", "", "the agent's display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		return errors.New("-code is required")
	}
	dir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	manager, err := auth.NewManager(dir)
	if err != nil {
		return err
	}
	service := friendsServiceURL()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := (&friends.Client{BaseURL: service, Auth: manager}).Link(ctx, strings.TrimSpace(*code), *name)
	if err != nil {
		return err
	}
	store := friends.NewStore(dir)
	if result.Replaced {
		// A new computer took over: drafts written for the old link stay here
		// no longer.
		_ = store.Clear()
	}
	if err := store.SaveLink(friends.Link{Handle: result.Handle, ServiceURL: service, LinkedAt: time.Now().UTC()}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Linked to @%s.\n", result.Handle)
	return nil
}

// projectRoots are the places where this person keeps projects.
func projectRoots(home string) []string {
	roots := []string{}
	if extra := strings.TrimSpace(os.Getenv("MEWLA_FRIENDS_PROJECT_ROOTS")); extra != "" {
		roots = append(roots, filepath.SplitList(extra)...)
	}
	for _, name := range []string{"workspace", "src", "code", "projects", "dev", "work", "repos"} {
		roots = append(roots, filepath.Join(home, name))
	}
	return roots
}

func friendsVocabulary(stateDir, owner string) *friends.Vocabulary {
	home, _ := os.UserHomeDir()
	return friends.CollectVocabulary(friends.Machine{
		StateDir: stateDir, HomeDir: home, ProjectRoots: projectRoots(home), Owner: owner,
	})
}

func friendsDraft(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs, stateDir := friendsFlags("draft", stderr)
	check := fs.Bool("check", false, "only scrub the text; keep nothing")
	source := fs.String("source", "", "where the post came from, for the owner (for example the Work title)")
	asJSON := fs.Bool("json", false, "print JSON")
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
	dir, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	store := friends.NewStore(dir)
	owner := ""
	if link, err := store.Link(); err == nil {
		owner = link.Handle
	} else if !*check {
		return err
	}
	scrubber := &friends.Scrubber{Vocabulary: friendsVocabulary(dir, owner)}
	findings := scrubber.Check(text)
	if len(findings) > 0 {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"accepted": false, "findings": findings})
		}
		reasons := make([]string, len(findings))
		for i, finding := range findings {
			reasons[i] = finding.Reason
		}
		return fmt.Errorf("draft refused, rewrite it without these: %s", strings.Join(reasons, "; "))
	}
	if *check {
		if *asJSON {
			return json.NewEncoder(stdout).Encode(map[string]any{"accepted": true})
		}
		fmt.Fprintln(stdout, "Passes the checks. Nothing was kept.")
		return nil
	}
	draft, err := store.AddDraft(text, *source)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{"accepted": true, "id": draft.ID})
	}
	fmt.Fprintf(stdout, "Draft %s kept for the owner to read. It is not posted.\n", draft.ID)
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
	drafts, err := friends.NewStore(dir).Drafts()
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{"drafts": drafts})
	}
	for _, draft := range drafts {
		fmt.Fprintf(stdout, "%s  %s\n", draft.ID, draft.Text)
	}
	return nil
}
