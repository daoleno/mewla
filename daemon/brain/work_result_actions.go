package brain

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// A Work result's next steps are only what the daemon can show is there:
//
//   - pull_request: a pull or merge request URL written in the result (its
//     summary or details), opened in the browser.
//   - git_review: the Worker's project has uncommitted changes right now,
//     opened in Git review. Only on a Work's current result: the changes are
//     the project's state now, not what an older result left.
//   - file: a file the result names that exists inside the Worker's project,
//     opened as a repository file.
//   - brain_file / brain_folder: a file the result names, or the Work's own
//     context (its worklog), that exists in the Brain workspace.
//
// A result that names none of these, whose Worker left no known project or
// left it clean, gets none. Work.NextAction is Brain's own note and is never
// an action. Labels are the client's.
const (
	ResultActionPullRequest = "pull_request"
	ResultActionGitReview   = "git_review"
	ResultActionFile        = "file"
	ResultActionBrainFile   = "brain_file"
	ResultActionBrainFolder = "brain_folder"

	resultActionFileLimit = 4
	resultGitProbeTTL     = 15 * time.Second
	resultGitProbeTimeout = 3 * time.Second
)

var pullRequestURL = regexp.MustCompile(`https://(?:github\.com/[\w.-]+/[\w.-]+/pull/\d+|[\w.-]+(?::\d+)?(?:/[\w.-]+)+/-/merge_requests/\d+)`)

// workResultSource is what one result says and where its Worker worked.
type workResultSource struct {
	sessionID  string
	summary    string
	details    string
	contextRef string
	current    bool
	cwd        string
}

type resultGitProbe struct {
	root  string
	dirty bool
	at    time.Time
}

// resultGitProbes caches, per Worker cwd, its repository root and whether it
// has changes, so a thread tick runs git at most once per project per TTL.
type resultGitProbes struct {
	mu     sync.Mutex
	byCwd  map[string]resultGitProbe
	now    func() time.Time
	runGit func(dir string, args ...string) (string, error)
}

func (p *resultGitProbes) probe(cwd string) resultGitProbe {
	now := time.Now
	if p.now != nil {
		now = p.now
	}
	p.mu.Lock()
	cached, ok := p.byCwd[cwd]
	run := p.runGit
	p.mu.Unlock()
	if ok && now().Sub(cached.at) < resultGitProbeTTL {
		return cached
	}
	if run == nil {
		run = runResultGit
	}
	// git runs outside the lock: one slow project never holds another's
	// thread. Two ticks may probe the same project at once; both answers
	// are true.
	probe := resultGitProbe{at: now()}
	if root, err := run(cwd, "rev-parse", "--show-toplevel"); err == nil {
		probe.root = strings.TrimSpace(root)
		if status, err := run(probe.root, "status", "--porcelain", "--untracked-files=normal"); err == nil {
			probe.dirty = strings.TrimSpace(status) != ""
		}
	}
	p.mu.Lock()
	if p.byCwd == nil {
		p.byCwd = map[string]resultGitProbe{}
	}
	p.byCwd[cwd] = probe
	p.mu.Unlock()
	return probe
}

func runResultGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resultGitProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// A read must never take the index lock from the person's own git.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	return string(out), err
}

// annotateWorkResultActions sets each Work result row's typed next steps.
// currentByWork names each Work's current result row.
func (s *Service) annotateWorkResultActions(events []work.CodexConversationEvent, currentByWork map[string]string) {
	works := map[string]Work{}
	for index := range events {
		event := &events[index]
		if event.Source != workResultConversationSource || strings.TrimSpace(event.WorkID) == "" {
			continue
		}
		item, ok := works[event.WorkID]
		if !ok {
			// A Work that is gone, or a read that fails, leaves its rows
			// without the Work's own context; the thread still renders.
			found, err := s.store.Work(event.WorkID)
			if err != nil && !errors.Is(err, ErrWorkNotFound) {
				log.Printf("brain Work %s result actions: %v", event.WorkID, err)
			}
			item = found
			works[event.WorkID] = item
		}
		event.WorkActions = s.workResultActions(workResultSource{
			sessionID: strings.TrimSpace(event.WorkSession), summary: event.Body, details: event.WorkDetailsJSON,
			contextRef: item.ContextRef, current: currentByWork[event.WorkID] == event.ID,
			cwd: s.workWorkerCwd(event.WorkID, event.WorkSession),
		})
	}
}

// workWorkerCwd is where a Work's Worker Session worked: the cwd it joined
// with, else its live cwd.
func (s *Service) workWorkerCwd(workID, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || sessionID == "work" {
		return ""
	}
	if records, err := s.store.workWorkerRecords(); err == nil {
		if record, ok := records[workWorkerKey(strings.TrimSpace(workID), sessionID)]; ok && record.Cwd != "" {
			return record.Cwd
		}
	}
	if s.watcher != nil {
		if worker := s.watcher.GetWorker(sessionID); worker != nil {
			return strings.TrimSpace(worker.Cwd)
		}
	}
	return ""
}

func (s *Service) workResultActions(source workResultSource) []work.ResultAction {
	var out []work.ResultAction
	seen := map[string]bool{}
	add := func(action work.ResultAction) {
		key := action.Kind + "\x00" + action.URL + "\x00" + action.Path
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, action)
	}
	texts := append([]string{source.summary}, resultDetailStrings(source.details)...)
	for _, text := range texts {
		for _, url := range pullRequestURL.FindAllString(text, -1) {
			add(work.ResultAction{Kind: ResultActionPullRequest, URL: strings.TrimRight(url, ".")})
		}
	}

	// The project is probed only for a Work's current result: an older
	// result offers neither its changes (they are now, not then) nor its
	// project's files, and a long thread runs no git for its history.
	var git resultGitProbe
	if source.current && source.cwd != "" && filepath.IsAbs(source.cwd) {
		git = s.resultGit.probe(source.cwd)
	}
	if source.current && git.root != "" && git.dirty {
		add(work.ResultAction{Kind: ResultActionGitReview, SessionID: source.sessionID, Cwd: git.root})
	}

	workspace := ""
	if s.store != nil {
		workspace = filepath.Clean(s.store.WorkspacePath())
	}
	files := 0
	for _, candidate := range resultPathCandidates(texts) {
		if files == resultActionFileLimit {
			break
		}
		for _, path := range resolveResultPath(candidate, source.cwd, git.root, workspace) {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if rel, ok := within(workspace, path); ok {
				add(work.ResultAction{Kind: ResultActionBrainFile, Path: rel})
				files++
				break
			}
			if rel, ok := within(git.root, path); ok {
				add(work.ResultAction{Kind: ResultActionFile, SessionID: source.sessionID, Cwd: git.root, Path: rel})
				files++
				break
			}
		}
	}

	if ref := strings.TrimSpace(source.contextRef); ref != "" && workspace != "" &&
		!strings.HasPrefix(ref, "calendar:") && !strings.HasPrefix(ref, "watch:") {
		path := ref
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		if rel, ok := within(workspace, filepath.Clean(path)); ok {
			if info, err := os.Stat(filepath.Join(workspace, rel)); err == nil {
				switch {
				case info.Mode().IsRegular():
					add(work.ResultAction{Kind: ResultActionBrainFile, Path: rel})
				case info.IsDir():
					add(work.ResultAction{Kind: ResultActionBrainFolder, Path: rel})
				}
			}
		}
	}
	return out
}

// resultDetailStrings is every string value in a result's details JSON.
func resultDetailStrings(details string) []string {
	details = strings.TrimSpace(details)
	if details == "" {
		return nil
	}
	var value any
	if json.Unmarshal([]byte(details), &value) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			out = append(out, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return out
}

// resultPathCandidates are the words of a result that could name a file: a
// path with a slash, or a name with an extension. Only the ones that exist
// become actions.
func resultPathCandidates(texts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, text := range texts {
		for _, word := range strings.Fields(text) {
			word = strings.TrimLeft(word, "`'\"([{<")
			word = strings.TrimRight(word, "`'\")]}>,;:.!?")
			if word == "" || strings.Contains(word, "://") || seen[word] {
				continue
			}
			if !strings.Contains(word, "/") && filepath.Ext(word) == "" {
				continue
			}
			seen[word] = true
			out = append(out, word)
		}
	}
	return out
}

// resolveResultPath is where a named path could be: itself when absolute,
// else under each base in turn (the Worker's cwd, its repository root, the
// Brain workspace).
func resolveResultPath(candidate string, bases ...string) []string {
	if filepath.IsAbs(candidate) {
		return []string{filepath.Clean(candidate)}
	}
	var out []string
	for _, base := range bases {
		if base != "" && filepath.IsAbs(base) {
			out = append(out, filepath.Join(base, candidate))
		}
	}
	return out
}

// within reports path relative to root when path is inside it.
func within(root, path string) (string, bool) {
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
