package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Git review actions: stage, discard, hunks, commit, push, pull and branches
// for the repository of a Session. Every command is an argument array run
// without a shell, with a deadline, no stdin prompt and a pinned locale.

const (
	gitLocalTimeout   = 30 * time.Second
	gitNetworkTimeout = 60 * time.Second
	// Commit and switch run the repository's hooks (and may sign), which can
	// legitimately take minutes.
	gitHookTimeout = 5 * time.Minute
	// A hunk's diff is read whole; past this it is too big to apply from a phone.
	gitHunkDiffMax = 8 << 20
	gitMessageMax  = 2000
)

var (
	gitObjectIDPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	gitShaPattern      = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	gitANSIPattern     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	gitRepoLocks       sync.Map // repo root -> *sync.Mutex
)

var errGitBusy = errors.New("Git is busy in this repository (another git command is running). Try again.")

// gitDiscardUndo is what a discard returns so the app can put the file back.
type gitDiscardUndo struct {
	Path   string `json:"path"`
	Blob   string `json:"blob"`
	After  string `json:"after"`
	Mode   string `json:"mode"`
	Staged bool   `json:"staged"`
}

type gitRunResult struct {
	stdout    string
	stderr    string
	exit      int
	truncated bool
}

type gitRunOptions struct {
	stdin     string
	timeout   time.Duration
	maxStdout int
	// network marks a command that talks to a remote.
	network bool
}

// limitedBuffer keeps draining a command's output past its cap, so git exits
// normally, while remembering only the first max bytes.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 || b.buf.Len() < b.max {
		room := len(p)
		if b.max > 0 && b.buf.Len()+room > b.max {
			room = b.max - b.buf.Len()
			b.truncated = true
		}
		b.buf.Write(p[:room])
	} else {
		b.truncated = true
	}
	return len(p), nil
}

func gitActionEnv(repoRoot string, network bool) []string {
	env := append(os.Environ(),
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=true",
		"GIT_MERGE_AUTOEDIT=no",
	)
	if network && !gitUsesOwnSSH(os.Getenv("GIT_SSH_COMMAND"), os.Getenv("GIT_SSH"), gitConfiguredSSHCommand(repoRoot)) {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=15")
	}
	return env
}

// gitUsesOwnSSH reports whether the person already chose how git reaches the
// remote (GIT_SSH_COMMAND, GIT_SSH or core.sshCommand). The daemon's
// non-interactive default would override such a choice, and with it a key.
func gitUsesOwnSSH(envCommand, envProgram, configured string) bool {
	return envCommand != "" || envProgram != "" || configured != ""
}

func gitConfiguredSSHCommand(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "config", "--get", "core.sshCommand").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitRun runs git in repoRoot. A non-zero exit is reported in the result, not
// as an error; only a failure to run (or a timeout) is an error.
func gitRun(repoRoot string, opts gitRunOptions, args ...string) (gitRunResult, error) {
	timeout := opts.timeout
	if timeout <= 0 {
		timeout = gitLocalTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = gitActionEnv(repoRoot, opts.network)
	gitOwnProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	if opts.stdin != "" {
		cmd.Stdin = strings.NewReader(opts.stdin)
	}
	stdout := &limitedBuffer{max: opts.maxStdout}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := gitRunResult{stdout: stdout.buf.String(), stderr: stderr.String(), truncated: stdout.truncated}
	if err == nil {
		return result, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		if opts.network {
			return result, fmt.Errorf("Git took too long and was stopped. Check the connection and try again.")
		}
		return result, fmt.Errorf("Git took too long and was stopped. A hook or signing step may be waiting; try again.")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exit = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("run git: %w", err)
}

// gitMust runs git and turns a non-zero exit into a plain message error.
func gitMust(repoRoot string, opts gitRunOptions, args ...string) (string, error) {
	result, err := gitRun(repoRoot, opts, args...)
	if err != nil {
		return "", err
	}
	if result.exit != 0 {
		return "", gitPlainError(result, 3)
	}
	return result.stdout, nil
}

func gitPlainError(result gitRunResult, maxLines int) error {
	text := result.stderr
	if strings.TrimSpace(text) == "" {
		text = result.stdout
	}
	if strings.Contains(text, "index.lock") {
		return errGitBusy
	}
	message := plainGitMessage(text, maxLines)
	if message == "" {
		message = "Git failed (exit " + strconv.Itoa(result.exit) + ")."
	}
	return errors.New(message)
}

// plainGitMessage reduces git's output to what a person needs: no color, no
// hint lines, no "error:" prefixes, at most maxLines lines.
func plainGitMessage(text string, maxLines int) string {
	text = gitANSIPattern.ReplaceAllString(text, "")
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		trimmed := strings.TrimRight(line, " \t")
		check := strings.TrimSpace(trimmed)
		switch {
		case check == "",
			strings.HasPrefix(check, "hint:"),
			strings.HasPrefix(check, "To "),
			strings.HasPrefix(check, "Please make sure you have the correct access rights"),
			check == "and the repository exists.":
			continue
		}
		for _, prefix := range []string{"error: ", "fatal: ", "! "} {
			if strings.HasPrefix(check, prefix) {
				trimmed = strings.TrimPrefix(check, prefix)
				break
			}
		}
		lines = append(lines, trimmed)
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
	}
	message := strings.Join(lines, "\n")
	if len(message) > gitMessageMax {
		message = message[:gitMessageMax] + "…"
	}
	return message
}

func lockGitRepo(repoRoot string) func() {
	value, _ := gitRepoLocks.LoadOrStore(repoRoot, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

// cleanGitPaths validates repo-relative paths from the client.
func cleanGitPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("No files were given.")
	}
	cleaned := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		clean, err := cleanGitPath(path)
		if err != nil {
			return nil, err
		}
		if !seen[clean] {
			seen[clean] = true
			cleaned = append(cleaned, clean)
		}
	}
	return cleaned, nil
}

func cleanGitPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("That file path is not valid.")
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("That file path is not valid.")
		}
	}
	slashed := filepath.ToSlash(path)
	if strings.HasPrefix(slashed, "/") || filepath.IsAbs(path) {
		return "", fmt.Errorf("%s is outside this repository.", path)
	}
	// One spelling per file: `a/./b` and `a//b` are `a/b`, so a path that
	// looks different cannot slip past a check made on another spelling.
	var parts []string
	for _, part := range strings.Split(slashed, "/") {
		switch {
		case part == "" || part == ".":
			continue
		case part == "..":
			return "", fmt.Errorf("%s is outside this repository.", path)
		case strings.EqualFold(part, ".git"):
			return "", fmt.Errorf("%s is inside .git and can't be changed here.", path)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", errors.New("That file path is not valid.")
	}
	return strings.Join(parts, "/"), nil
}

func gitPathspecs(paths []string) []string {
	specs := make([]string, len(paths))
	for i, path := range paths {
		specs[i] = gitLiteralPathspec(path)
	}
	return specs
}

// resolveGitActionRepo finds the repository and applies the repo_root guard.
func (s *Server) resolveGitActionRepo(raw clientMessage) (string, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(raw.TargetID, raw.Cwd)
	if err != nil {
		return "", err
	}
	switch reason {
	case gitDiffReasonNoCwd:
		return "", errors.New("Git is unavailable because this terminal has no folder yet.")
	case gitDiffReasonNotGitRepo:
		return "", errors.New("This folder is not a Git repository.")
	}
	if raw.GitRepoRoot != "" && filepath.Clean(raw.GitRepoRoot) != filepath.Clean(repoRoot) {
		return "", fmt.Errorf("This terminal is no longer in %s.", filepath.Base(raw.GitRepoRoot))
	}
	return repoRoot, nil
}

func (s *Server) handleGitMessage(conn *websocket.Conn, raw clientMessage) {
	// These run on their own goroutine: a panic here would take the daemon down.
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("git action %s panicked: %v", raw.Type, recovered)
			s.sendErrorWithRequestID(conn, raw.RequestID, raw.Type+"_failed", "Git action failed unexpectedly.")
		}
	}()
	name := strings.TrimPrefix(raw.Type, "git_")
	repoRoot, err := s.resolveGitActionRepo(raw)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, raw.Type+"_failed", err.Error())
		return
	}
	switch raw.Type {
	case "git_branches", "git_log", "git_show":
		var payload any
		var key string
		switch raw.Type {
		case "git_branches":
			key = "branches"
			payload, err = gitBranches(repoRoot)
		case "git_log":
			key = "log"
			payload, err = gitLog(repoRoot, raw.Limit, raw.GitSkip)
		default:
			key = "commit"
			payload, err = gitShow(repoRoot, raw.GitSHA)
		}
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, raw.Type+"_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": raw.Type, "request_id": raw.RequestID, key: payload})
		return
	}

	// A fetch only moves remote-tracking refs, so it does not wait for (or
	// hold up) the changes that touch the index and working tree.
	var result any
	if raw.Type == "git_fetch" {
		result, err = gitFetch(repoRoot)
	} else {
		unlock := lockGitRepo(repoRoot)
		result, err = runGitMutation(repoRoot, raw)
		unlock()
	}
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, raw.Type+"_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "git_action",
		"request_id": raw.RequestID,
		"action":     name,
		"result":     result,
	})
}

func runGitMutation(repoRoot string, raw clientMessage) (any, error) {
	switch raw.Type {
	case "git_stage":
		return gitStage(repoRoot, raw.GitPaths)
	case "git_unstage":
		return gitUnstage(repoRoot, raw.GitPaths)
	case "git_discard":
		return gitDiscard(repoRoot, raw.GitPaths, raw.Scope)
	case "git_discard_undo":
		return gitDiscardUndoFiles(repoRoot, raw.GitUndo)
	case "git_hunk":
		return gitHunk(repoRoot, raw.Path, raw.Scope, raw.Action, raw.GitPatch)
	case "git_commit":
		return gitCommit(repoRoot, raw.GitMessage)
	case "git_push":
		return gitPush(repoRoot)
	case "git_pull":
		return gitPull(repoRoot)
	case "git_switch":
		return gitSwitch(repoRoot, raw.GitBranch, raw.GitCreate)
	}
	return nil, fmt.Errorf("Unknown Git action: %s", raw.Type)
}

func gitHasCommits(repoRoot string) bool {
	result, err := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil && result.exit == 0
}

func gitStage(repoRoot string, rawPaths []string) (map[string]any, error) {
	paths, err := cleanGitPaths(rawPaths)
	if err != nil {
		return nil, err
	}
	args := append([]string{"add", "-A", "--"}, gitPathspecs(paths)...)
	if _, err := gitMust(repoRoot, gitRunOptions{}, args...); err != nil {
		return nil, err
	}
	return map[string]any{"staged": len(paths)}, nil
}

func gitUnstage(repoRoot string, rawPaths []string) (map[string]any, error) {
	paths, err := cleanGitPaths(rawPaths)
	if err != nil {
		return nil, err
	}
	specs := gitPathspecs(paths)
	var args []string
	if gitHasCommits(repoRoot) {
		args = append([]string{"restore", "--staged", "--"}, specs...)
	} else {
		args = append([]string{"rm", "--cached", "-r", "--ignore-unmatch", "-q", "--"}, specs...)
	}
	if _, err := gitMust(repoRoot, gitRunOptions{}, args...); err != nil {
		return nil, err
	}
	return map[string]any{"unstaged": len(paths)}, nil
}

// gitWorktreeFile resolves a repo-relative path to an absolute one. It refuses
// a path whose existing parents leave the repository through a symlink, and one
// that reaches into the git directory through one (`ln -s .git g` makes
// `g/HEAD` the repository's own HEAD).
func gitWorktreeFile(repoRoot, path string) (string, error) {
	full := filepath.Join(repoRoot, filepath.FromSlash(path))
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(full)
	for {
		if _, statErr := os.Lstat(parent); statErr == nil {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s is outside this repository.", path)
	}
	for _, dir := range gitControlDirs(repoRoot, root) {
		if resolved == dir || strings.HasPrefix(resolved, dir+string(os.PathSeparator)) {
			return "", fmt.Errorf("%s is inside .git and can't be changed here.", path)
		}
	}
	return full, nil
}

// gitControlDirs are the resolved locations of the repository's git directory
// (the common one too, for a linked worktree).
func gitControlDirs(repoRoot, resolvedRoot string) []string {
	dirs := []string{filepath.Join(resolvedRoot, ".git")}
	for _, flag := range []string{"--absolute-git-dir", "--git-common-dir"} {
		result, err := gitRun(repoRoot, gitRunOptions{}, "rev-parse", flag)
		if err != nil || result.exit != 0 {
			continue
		}
		dir := strings.TrimSpace(result.stdout)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(repoRoot, dir)
		}
		if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
			dirs = append(dirs, resolvedDir)
		}
	}
	return dirs
}

func gitHashFile(repoRoot, full string, write bool) (string, error) {
	args := []string{"hash-object", "--no-filters"}
	if write {
		args = append(args, "-w")
	}
	args = append(args, "--", full)
	out, err := gitMust(repoRoot, gitRunOptions{}, args...)
	return strings.TrimSpace(out), err
}

func gitDiscard(repoRoot string, rawPaths []string, scope string) (map[string]any, error) {
	paths, err := cleanGitPaths(rawPaths)
	if err != nil {
		return nil, err
	}
	if scope != "working" && scope != "all" {
		return nil, errors.New("Choose what to discard: the working changes or everything.")
	}
	hasHead := gitHasCommits(repoRoot)

	type plan struct {
		path    string
		full    string
		undo    gitDiscardUndo
		inIndex bool
		inHead  bool
		onDisk  bool
		untrack bool
	}
	plans := make([]*plan, 0, len(paths))
	// Plan and snapshot everything before changing anything.
	for _, path := range paths {
		spec := gitLiteralPathspec(path)
		full, err := gitWorktreeFile(repoRoot, path)
		if err != nil {
			return nil, err
		}
		p := &plan{path: path, full: full, undo: gitDiscardUndo{Path: path}}
		out, err := gitMust(repoRoot, gitRunOptions{}, "ls-files", "-z", "--", spec)
		if err != nil {
			return nil, err
		}
		p.inIndex = strings.Trim(out, "\x00") != ""
		if hasHead {
			result, err := gitRun(repoRoot, gitRunOptions{}, "cat-file", "-e", "HEAD:"+path)
			if err != nil {
				return nil, err
			}
			p.inHead = result.exit == 0
		}
		info, statErr := os.Lstat(full)
		switch {
		case statErr == nil:
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%s is a link or folder; discard it from a terminal.", path)
			}
			p.onDisk = true
			p.undo.Blob, err = gitHashFile(repoRoot, full, true)
			if err != nil {
				return nil, err
			}
			p.undo.Mode = "100644"
			if info.Mode()&0o111 != 0 {
				p.undo.Mode = "100755"
			}
		case os.IsNotExist(statErr):
		default:
			return nil, statErr
		}
		if !p.inIndex && !p.onDisk {
			return nil, fmt.Errorf("%s has nothing to discard.", path)
		}
		if scope == "all" {
			if hasHead {
				result, err := gitRun(repoRoot, gitRunOptions{}, "diff", "--cached", "--name-only", "--", spec)
				if err != nil {
					return nil, err
				}
				p.undo.Staged = strings.TrimSpace(result.stdout) != ""
			} else {
				p.undo.Staged = p.inIndex
			}
		}
		p.untrack = !p.inIndex
		plans = append(plans, p)
	}

	for _, p := range plans {
		spec := gitLiteralPathspec(p.path)
		switch {
		case p.untrack:
			if err := os.Remove(p.full); err != nil {
				return nil, err
			}
			removeEmptyGitParents(repoRoot, p.full)
		case scope == "working":
			if _, err := gitMust(repoRoot, gitRunOptions{}, "restore", "--worktree", "--", spec); err != nil {
				return nil, err
			}
		case p.inHead:
			if _, err := gitMust(repoRoot, gitRunOptions{}, "restore", "--source=HEAD", "--staged", "--worktree", "--", spec); err != nil {
				return nil, err
			}
		default: // staged addition, or unborn HEAD
			if _, err := gitMust(repoRoot, gitRunOptions{}, "rm", "-f", "-q", "--", spec); err != nil {
				return nil, err
			}
			removeEmptyGitParents(repoRoot, p.full)
		}
		if _, err := os.Lstat(p.full); err == nil {
			after, err := gitHashFile(repoRoot, p.full, false)
			if err != nil {
				return nil, err
			}
			p.undo.After = after
		}
	}
	undo := make([]gitDiscardUndo, 0, len(plans))
	for _, p := range plans {
		undo = append(undo, p.undo)
	}
	return map[string]any{"discarded": len(plans), "undo": undo}, nil
}

func removeEmptyGitParents(repoRoot, full string) {
	root := filepath.Clean(repoRoot)
	for dir := filepath.Dir(full); dir != root && strings.HasPrefix(dir, root+string(os.PathSeparator)); dir = filepath.Dir(dir) {
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

func gitDiscardUndoFiles(repoRoot string, undo []gitDiscardUndo) (map[string]any, error) {
	if len(undo) == 0 {
		return nil, errors.New("There is nothing to undo.")
	}
	type step struct {
		undo gitDiscardUndo
		full string
		data []byte
	}
	steps := make([]step, 0, len(undo))
	for _, item := range undo {
		path, err := cleanGitPath(item.Path)
		if err != nil {
			return nil, err
		}
		item.Path = path
		if (item.Blob != "" && !gitObjectIDPattern.MatchString(item.Blob)) || (item.After != "" && !gitObjectIDPattern.MatchString(item.After)) {
			return nil, errors.New("That undo is not valid.")
		}
		if item.Mode != "" && item.Mode != "100644" && item.Mode != "100755" {
			return nil, errors.New("That undo is not valid.")
		}
		full, err := gitWorktreeFile(repoRoot, path)
		if err != nil {
			return nil, err
		}
		info, statErr := os.Lstat(full)
		switch {
		case statErr == nil:
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%s changed since it was discarded.", path)
			}
			current, err := gitHashFile(repoRoot, full, false)
			if err != nil {
				return nil, err
			}
			if item.After == "" || current != item.After {
				return nil, fmt.Errorf("%s changed since it was discarded.", path)
			}
		case os.IsNotExist(statErr):
			if item.After != "" {
				return nil, fmt.Errorf("%s changed since it was discarded.", path)
			}
		default:
			return nil, statErr
		}
		st := step{undo: item, full: full}
		if item.Blob != "" {
			result, err := gitRun(repoRoot, gitRunOptions{}, "cat-file", "-t", item.Blob)
			if err != nil {
				return nil, err
			}
			if result.exit != 0 || strings.TrimSpace(result.stdout) != "blob" {
				return nil, fmt.Errorf("The discarded copy of %s is no longer available.", path)
			}
			out, err := gitMust(repoRoot, gitRunOptions{}, "cat-file", "blob", item.Blob)
			if err != nil {
				return nil, err
			}
			st.data = []byte(out)
		}
		steps = append(steps, st)
	}
	for _, st := range steps {
		if st.undo.Blob == "" {
			if err := os.Remove(st.full); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			removeEmptyGitParents(repoRoot, st.full)
		} else {
			if err := os.MkdirAll(filepath.Dir(st.full), 0o755); err != nil {
				return nil, err
			}
			mode := os.FileMode(0o644)
			if st.undo.Mode == "100755" {
				mode = 0o755
			}
			if err := os.WriteFile(st.full, st.data, mode); err != nil {
				return nil, err
			}
			if err := os.Chmod(st.full, mode); err != nil {
				return nil, err
			}
		}
		if st.undo.Staged {
			if _, err := gitMust(repoRoot, gitRunOptions{}, "add", "-A", "--", gitLiteralPathspec(st.undo.Path)); err != nil {
				return nil, err
			}
		}
	}
	return map[string]any{"restored": len(steps)}, nil
}

var gitHunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// validateGitHunks accepts only well-formed hunks, consuming exactly the line
// counts each header declares, so nothing after a hunk can be read by git
// apply as another file's header.
func validateGitHunks(patch string) (string, error) {
	// The bytes are kept as sent: a CR before a newline belongs to the file's
	// own line, and stripping it makes the hunk fail to apply to a CRLF file.
	if !strings.HasPrefix(patch, "@@ ") {
		return "", errors.New("That change is not a hunk.")
	}
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	for i := 0; i < len(lines); {
		match := gitHunkHeaderPattern.FindStringSubmatch(lines[i])
		if match == nil {
			return "", errors.New("That change is not a valid hunk.")
		}
		count := func(value string) int {
			if value == "" {
				return 1
			}
			n, _ := strconv.Atoi(value)
			return n
		}
		oldLeft, newLeft := count(match[1]), count(match[2])
		i++
		for (oldLeft > 0 || newLeft > 0) && i < len(lines) {
			line := lines[i]
			switch {
			case line == "" || line[0] == ' ':
				oldLeft--
				newLeft--
			case line[0] == '-':
				oldLeft--
			case line[0] == '+':
				newLeft--
			case line[0] == '\\':
			default:
				return "", errors.New("That change is not a valid hunk.")
			}
			i++
		}
		if oldLeft != 0 || newLeft != 0 {
			return "", errors.New("That change is not a valid hunk.")
		}
		for i < len(lines) && strings.HasPrefix(lines[i], "\\") {
			i++
		}
	}
	return patch, nil
}

func gitHunk(repoRoot, rawPath, scope, action, hunk string) (map[string]any, error) {
	path, err := cleanGitPath(rawPath)
	if err != nil {
		return nil, err
	}
	var staged, reverse, cached bool
	switch action {
	case "stage":
		if scope != "unstaged" {
			return nil, errors.New("Only unstaged changes can be staged.")
		}
		cached = true
	case "unstage":
		if scope != "staged" {
			return nil, errors.New("Only staged changes can be unstaged.")
		}
		staged, cached, reverse = true, true, true
	case "discard":
		if scope != "unstaged" {
			return nil, errors.New("Only unstaged changes can be discarded.")
		}
		reverse = true
	case "apply":
		if scope != "unstaged" {
			return nil, errors.New("Only unstaged changes can be restored.")
		}
	default:
		return nil, errors.New("Unknown hunk action.")
	}
	body, err := validateGitHunks(hunk)
	if err != nil {
		return nil, err
	}
	spec := gitLiteralPathspec(path)
	// `git apply` strips one path component, so the header must carry a/ and b/
	// whatever diff.noprefix or diff.mnemonicPrefix say.
	diffArgs := append(gitDiffPatchArgs(staged), "--src-prefix=a/", "--dst-prefix=b/", "--", spec)
	diff, err := gitRun(repoRoot, gitRunOptions{maxStdout: gitHunkDiffMax}, diffArgs...)
	if err != nil {
		return nil, err
	}
	if diff.exit != 0 {
		return nil, gitPlainError(diff, 2)
	}
	if diff.truncated {
		return nil, fmt.Errorf("The diff of %s is too large to change a hunk from here.", path)
	}
	current := diff.stdout
	var header strings.Builder
	for _, line := range strings.SplitAfter(current, "\n") {
		if strings.HasPrefix(line, "@@") {
			break
		}
		header.WriteString(line)
	}
	if header.Len() == 0 {
		if action != "apply" {
			return nil, fmt.Errorf("%s has no %s changes.", path, scope)
		}
		// Undoing the discard of a file's last hunk: no diff is left to take
		// a header from, so name the one tracked file ourselves.
		header.WriteString("diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n")
	}
	args := []string{"apply", "--whitespace=nowarn"}
	if cached {
		args = append(args, "--cached")
	}
	if reverse {
		args = append(args, "--reverse")
	}
	result, err := gitRun(repoRoot, gitRunOptions{stdin: header.String() + body}, args...)
	if err != nil {
		return nil, err
	}
	if result.exit != 0 {
		if strings.Contains(result.stderr, "index.lock") {
			return nil, errGitBusy
		}
		return nil, fmt.Errorf("This change no longer applies: %s", plainGitMessage(result.stderr, 2))
	}
	return map[string]any{"ok": true}, nil
}

func gitCommit(repoRoot, message string) (map[string]any, error) {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if message == "" {
		return nil, errors.New("Write a commit message.")
	}
	diff, err := gitRun(repoRoot, gitRunOptions{}, "diff", "--cached", "--quiet")
	if err != nil {
		return nil, err
	}
	if diff.exit == 0 {
		return nil, errors.New("Nothing is staged.")
	}
	result, err := gitRun(repoRoot, gitRunOptions{stdin: message + "\n", timeout: gitHookTimeout}, "commit", "-F", "-")
	if err != nil {
		return nil, err
	}
	if result.exit != 0 {
		text := plainGitMessage(result.stderr+"\n"+result.stdout, 6)
		if strings.Contains(result.stderr, "index.lock") {
			return nil, errGitBusy
		}
		if text == "" {
			text = "The commit failed."
		}
		return nil, errors.New(text)
	}
	sha, err := gitMust(repoRoot, gitRunOptions{}, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	return map[string]any{"sha": strings.TrimSpace(sha), "subject": strings.SplitN(message, "\n", 2)[0]}, nil
}

func gitCurrentBranch(repoRoot string) string {
	result, err := gitRun(repoRoot, gitRunOptions{}, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || result.exit != 0 {
		return ""
	}
	return strings.TrimSpace(result.stdout)
}

// gitUpstream is the upstream the branch is configured with
// (`branch.<name>.remote` and `.merge`), as `origin/main`, whether or not that
// remote branch still exists. It is empty only when none was ever set.
func gitUpstream(repoRoot string) string {
	branch := gitCurrentBranch(repoRoot)
	if branch == "" {
		return ""
	}
	value := func(key string) string {
		result, err := gitRun(repoRoot, gitRunOptions{}, "config", "--get", "branch."+branch+"."+key)
		if err != nil || result.exit != 0 {
			return ""
		}
		return strings.TrimSpace(result.stdout)
	}
	remote, merge := value("remote"), value("merge")
	if remote == "" || merge == "" {
		return ""
	}
	name := strings.TrimPrefix(merge, "refs/heads/")
	if remote == "." {
		return name
	}
	return remote + "/" + name
}

// gitUpstreamExists reports whether the configured upstream still resolves
// (a pruned remote branch leaves the configuration behind).
func gitUpstreamExists(repoRoot string) bool {
	result, err := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "--verify", "-q", "@{u}")
	return err == nil && result.exit == 0
}

func gitPush(repoRoot string) (map[string]any, error) {
	branch := gitCurrentBranch(repoRoot)
	if branch == "" {
		return nil, errors.New("You're on a detached HEAD. Switch to a branch to push.")
	}
	if !gitHasCommits(repoRoot) {
		return nil, errors.New("Commit something before pushing.")
	}
	args := []string{"push"}
	setUpstream := false
	if upstream := gitUpstream(repoRoot); upstream != "" {
		// Plain `git push` would publish the branch again under the same name,
		// bringing back one that was deleted on the remote (merged, say).
		if !gitUpstreamExists(repoRoot) {
			return nil, fmt.Errorf("%s no longer exists on the remote, so nothing was pushed.", upstream)
		}
	} else {
		remotes, err := gitMust(repoRoot, gitRunOptions{}, "remote")
		if err != nil {
			return nil, err
		}
		remote := ""
		for _, name := range strings.Fields(remotes) {
			if strings.HasPrefix(name, "-") {
				continue
			}
			if remote == "" || name == "origin" {
				remote = name
			}
		}
		if remote == "" {
			return nil, errors.New("This repository has no remote.")
		}
		args = []string{"push", "--set-upstream", remote, branch}
		setUpstream = true
	}
	result, err := gitRun(repoRoot, gitRunOptions{timeout: gitNetworkTimeout, network: true}, args...)
	if err != nil {
		return nil, err
	}
	if result.exit != 0 {
		return nil, gitPlainError(result, 2)
	}
	message := plainGitMessage(result.stderr+"\n"+result.stdout, 3)
	return map[string]any{"message": message, "branch": branch, "set_upstream": setUpstream}, nil
}

func gitPull(repoRoot string) (map[string]any, error) {
	if gitCurrentBranch(repoRoot) == "" || gitUpstream(repoRoot) == "" {
		return nil, errors.New("This branch has no upstream to pull from.")
	}
	if !gitUpstreamExists(repoRoot) {
		return nil, errors.New("The upstream branch no longer exists.")
	}
	before, _ := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "HEAD")
	result, err := gitRun(repoRoot, gitRunOptions{timeout: gitNetworkTimeout, network: true}, "pull", "--ff-only", "--no-rebase")
	if err != nil {
		return nil, err
	}
	if result.exit != 0 {
		return nil, gitPlainError(result, 3)
	}
	after, _ := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "HEAD")
	return map[string]any{
		"message": plainGitMessage(result.stdout+"\n"+result.stderr, 3),
		"updated": strings.TrimSpace(before.stdout) != strings.TrimSpace(after.stdout),
	}, nil
}

// gitFetch brings the remote-tracking branches up to date so ahead/behind is
// true. A repository with no remote has nothing to fetch.
func gitFetch(repoRoot string) (map[string]any, error) {
	remotes, err := gitRun(repoRoot, gitRunOptions{}, "remote")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(remotes.stdout) == "" {
		return map[string]any{"message": ""}, nil
	}
	result, err := gitRun(repoRoot, gitRunOptions{timeout: gitNetworkTimeout, network: true}, "fetch", "--prune")
	if err != nil {
		return nil, err
	}
	if result.exit != 0 {
		return nil, gitPlainError(result, 2)
	}
	return map[string]any{"message": plainGitMessage(result.stderr+"\n"+result.stdout, 3)}, nil
}

func validGitBranchName(repoRoot, name string) error {
	result, err := gitRun(repoRoot, gitRunOptions{}, "check-ref-format", "--branch", name)
	if err != nil {
		return err
	}
	if result.exit != 0 || strings.TrimSpace(result.stdout) != name || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q is not a valid branch name.", name)
	}
	return nil
}

func gitSwitch(repoRoot, branch string, create bool) (map[string]any, error) {
	if err := validGitBranchName(repoRoot, branch); err != nil {
		return nil, err
	}
	args := []string{"switch"}
	if create {
		args = append(args, "-c")
	}
	args = append(args, branch)
	if _, err := gitMust(repoRoot, gitRunOptions{timeout: gitHookTimeout}, args...); err != nil {
		return nil, err
	}
	return map[string]any{"branch": branch}, nil
}

// fillBranchState adds what the sheet shows beside the branch name: upstream,
// ahead/behind, detached HEAD, an unborn branch and an operation in progress.
func (p *gitDiffStatusPayload) fillBranchState(repoRoot string) {
	p.HasCommits = gitHasCommits(repoRoot)
	p.Detached = gitCurrentBranch(repoRoot) == ""
	if upstream := gitUpstream(repoRoot); upstream != "" && p.HasCommits {
		p.Upstream = upstream
		result, err := gitRun(repoRoot, gitRunOptions{}, "rev-list", "--left-right", "--count", "HEAD...@{u}")
		if err == nil && result.exit == 0 {
			fields := strings.Fields(result.stdout)
			if len(fields) == 2 {
				p.Ahead, _ = strconv.Atoi(fields[0])
				p.Behind, _ = strconv.Atoi(fields[1])
			}
		}
	}
	result, err := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "--absolute-git-dir")
	if err != nil || result.exit != 0 {
		return
	}
	dir := strings.TrimSpace(result.stdout)
	for _, marker := range []struct{ file, name string }{
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
	} {
		if _, err := os.Stat(filepath.Join(dir, marker.file)); err == nil {
			p.Operation = marker.name
			return
		}
	}
}

type gitBranchInfo struct {
	Name      string `json:"name"`
	Current   bool   `json:"current"`
	Remote    bool   `json:"remote"`
	RemoteRef string `json:"remote_ref,omitempty"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Subject   string `json:"subject"`
	Date      int64  `json:"date"`
}

type gitBranchesPayload struct {
	Current  string          `json:"current"`
	Detached bool            `json:"detached"`
	Branches []gitBranchInfo `json:"branches"`
}

var (
	gitAheadPattern  = regexp.MustCompile(`ahead (\d+)`)
	gitBehindPattern = regexp.MustCompile(`behind (\d+)`)
)

func gitBranches(repoRoot string) (gitBranchesPayload, error) {
	current := gitCurrentBranch(repoRoot)
	payload := gitBranchesPayload{Current: current, Detached: current == "", Branches: []gitBranchInfo{}}
	out, err := gitMust(repoRoot, gitRunOptions{}, "for-each-ref",
		"--format=%(refname)%00%(upstream:short)%00%(upstream:track)%00%(committerdate:unix)%00%(contents:subject)%1e",
		"refs/heads", "refs/remotes")
	if err != nil {
		return payload, err
	}
	local := map[string]bool{}
	var remotes []gitBranchInfo
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimLeft(record, "\n")
		fields := strings.Split(record, "\x00")
		if len(fields) < 5 {
			continue
		}
		ref := fields[0]
		date, _ := strconv.ParseInt(fields[3], 10, 64)
		info := gitBranchInfo{Upstream: fields[1], Subject: fields[4], Date: date}
		if m := gitAheadPattern.FindStringSubmatch(fields[2]); m != nil {
			info.Ahead, _ = strconv.Atoi(m[1])
		}
		if m := gitBehindPattern.FindStringSubmatch(fields[2]); m != nil {
			info.Behind, _ = strconv.Atoi(m[1])
		}
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			info.Name = strings.TrimPrefix(ref, "refs/heads/")
			info.Current = info.Name == current
			local[info.Name] = true
			payload.Branches = append(payload.Branches, info)
		case strings.HasPrefix(ref, "refs/remotes/"):
			short := strings.TrimPrefix(ref, "refs/remotes/")
			slash := strings.Index(short, "/")
			if slash < 0 || strings.HasSuffix(short, "/HEAD") {
				continue
			}
			info.Name = short[slash+1:]
			info.Remote = true
			info.RemoteRef = short
			info.Upstream = ""
			remotes = append(remotes, info)
		}
	}
	for _, info := range remotes {
		if !local[info.Name] {
			local[info.Name] = true
			payload.Branches = append(payload.Branches, info)
		}
	}
	if current != "" {
		found := false
		for _, b := range payload.Branches {
			if b.Current && !b.Remote {
				found = true
			}
		}
		if !found { // an unborn branch has no ref yet
			payload.Branches = append(payload.Branches, gitBranchInfo{Name: current, Current: true})
		}
	}
	sortGitBranches(payload.Branches)
	return payload, nil
}

func sortGitBranches(branches []gitBranchInfo) {
	// insertion sort: current first, then newest commit first; stable.
	for i := 1; i < len(branches); i++ {
		for j := i; j > 0 && gitBranchBefore(branches[j], branches[j-1]); j-- {
			branches[j], branches[j-1] = branches[j-1], branches[j]
		}
	}
}

func gitBranchBefore(a, b gitBranchInfo) bool {
	if a.Current != b.Current {
		return a.Current
	}
	return a.Date > b.Date
}

type gitCommitSummary struct {
	SHA     string   `json:"sha"`
	Short   string   `json:"short"`
	Subject string   `json:"subject"`
	Author  string   `json:"author"`
	Date    int64    `json:"date"`
	Refs    []string `json:"refs"`
	Parents int      `json:"parents"`
}

type gitLogPayload struct {
	Commits []gitCommitSummary `json:"commits"`
	HasMore bool               `json:"has_more"`
}

func gitLog(repoRoot string, limit, skip int) (gitLogPayload, error) {
	payload := gitLogPayload{Commits: []gitCommitSummary{}}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if skip < 0 {
		skip = 0
	}
	if !gitHasCommits(repoRoot) {
		return payload, nil
	}
	out, err := gitMust(repoRoot, gitRunOptions{}, "log", "--no-color",
		"-n", strconv.Itoa(limit+1), "--skip="+strconv.Itoa(skip),
		"--format=%H%x00%h%x00%an%x00%at%x00%P%x00%D%x00%s%x1e", "HEAD", "--")
	if err != nil {
		return payload, err
	}
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimLeft(record, "\n")
		fields := strings.Split(record, "\x00")
		if len(fields) < 7 {
			continue
		}
		date, _ := strconv.ParseInt(fields[3], 10, 64)
		commit := gitCommitSummary{
			SHA: fields[0], Short: fields[1], Author: fields[2], Date: date,
			Parents: len(strings.Fields(fields[4])), Subject: fields[6], Refs: []string{},
		}
		if fields[5] != "" {
			commit.Refs = strings.Split(fields[5], ", ")
		}
		payload.Commits = append(payload.Commits, commit)
	}
	if len(payload.Commits) > limit {
		payload.Commits = payload.Commits[:limit]
		payload.HasMore = true
	}
	return payload, nil
}

type gitCommitFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

type gitShowPayload struct {
	SHA       string          `json:"sha"`
	Short     string          `json:"short"`
	Subject   string          `json:"subject"`
	Body      string          `json:"body"`
	Author    string          `json:"author"`
	Date      int64           `json:"date"`
	Files     []gitCommitFile `json:"files"`
	Patch     string          `json:"patch"`
	Truncated bool            `json:"truncated"`
}

const gitShowPatchMax = 5 * 1024 * 1024

func gitShow(repoRoot, sha string) (gitShowPayload, error) {
	var payload gitShowPayload
	if !gitShaPattern.MatchString(sha) {
		return payload, errors.New("That commit id is not valid.")
	}
	resolved, err := gitRun(repoRoot, gitRunOptions{}, "rev-parse", "--verify", "--end-of-options", sha+"^{commit}")
	if err != nil {
		return payload, err
	}
	if resolved.exit != 0 {
		return payload, errors.New("That commit was not found.")
	}
	full := strings.TrimSpace(resolved.stdout)
	meta, err := gitMust(repoRoot, gitRunOptions{}, "show", "-s", "--no-color",
		"--format=%H%x00%h%x00%an%x00%at%x00%s%x00%b", "--end-of-options", full)
	if err != nil {
		return payload, err
	}
	fields := strings.SplitN(meta, "\x00", 6)
	if len(fields) < 6 {
		return payload, errors.New("Could not read that commit.")
	}
	payload.SHA, payload.Short, payload.Author = fields[0], fields[1], fields[2]
	payload.Date, _ = strconv.ParseInt(fields[3], 10, 64)
	payload.Subject = fields[4]
	payload.Body = strings.TrimSpace(fields[5])
	payload.Files = []gitCommitFile{}

	base := []string{"show", "--format=", "--no-ext-diff", "--no-color", "--no-textconv", "--find-renames", "-m", "--first-parent"}
	statusOut, err := gitMust(repoRoot, gitRunOptions{}, append(append([]string{}, base...), "--name-status", "-z", "--end-of-options", full)...)
	if err != nil {
		return payload, err
	}
	numstatOut, err := gitMust(repoRoot, gitRunOptions{}, append(append([]string{}, base...), "--numstat", "-z", "--end-of-options", full)...)
	if err != nil {
		return payload, err
	}
	payload.Files = parseGitCommitFiles(statusOut, numstatOut)

	patch, err := gitRun(repoRoot, gitRunOptions{maxStdout: gitShowPatchMax + 1}, append(append([]string{}, base...), "--end-of-options", full)...)
	if err != nil {
		return payload, err
	}
	if patch.exit != 0 {
		return payload, gitPlainError(patch, 3)
	}
	payload.Patch = patch.stdout
	if patch.truncated || len(payload.Patch) > gitShowPatchMax {
		payload.Truncated = true
		cut := payload.Patch
		if len(cut) > gitShowPatchMax {
			cut = cut[:gitShowPatchMax]
		}
		if index := strings.LastIndex(cut, "\ndiff --git "); index > 0 {
			payload.Patch = cut[:index+1]
		} else {
			payload.Patch = ""
		}
	}
	return payload, nil
}

func parseGitCommitFiles(nameStatus, numstat string) []gitCommitFile {
	files := []gitCommitFile{}
	index := map[string]int{}
	records := strings.Split(nameStatus, "\x00")
	for i := 0; i < len(records); i++ {
		code := records[i]
		if code == "" {
			continue
		}
		file := gitCommitFile{}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(records) {
				return files
			}
			file.OldPath, file.Path = records[i+1], records[i+2]
			file.Status = map[byte]string{'R': "renamed", 'C': "copied"}[code[0]]
			i += 2
		default:
			if i+1 >= len(records) {
				return files
			}
			file.Path = records[i+1]
			file.Status = map[byte]string{'A': "added", 'D': "deleted", 'M': "modified"}[code[0]]
			if file.Status == "" {
				file.Status = "changed"
			}
			i++
		}
		index[file.Path] = len(files)
		files = append(files, file)
	}
	records = strings.Split(numstat, "\x00")
	for i := 0; i < len(records); i++ {
		fields := strings.SplitN(records[i], "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		if path == "" {
			if i+2 >= len(records) {
				break
			}
			path = records[i+2]
			i += 2
		}
		position, ok := index[path]
		if !ok {
			continue
		}
		files[position].Additions, _ = parseNumstatField(fields[0])
		files[position].Deletions, _ = parseNumstatField(fields[1])
		files[position].Binary = fields[0] == "-"
	}
	return files
}
