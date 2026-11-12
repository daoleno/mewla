package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	restartDebounce = 250 * time.Millisecond
	stopTimeout     = 4 * time.Second
)

type runningProcess struct {
	cmd  *exec.Cmd
	done chan error
}

type watchTree struct {
	root    string
	watcher *fsnotify.Watcher
	watched map[string]struct{}
}

type devRunner struct {
	root       string
	binary     string
	daemonArgs []string
	stdout     io.Writer
	stderr     io.Writer
	child      *runningProcess
	// pprofAddr is the loopback address the daemon child serves Go's profiler
	// on (MEWLA_PPROF_ADDR); empty leaves the daemon's own setting alone.
	pprofAddr string
	webDirty  bool
	// goBuild runs `go build`; a test seam.
	goBuild func(dir string, env, args []string) ([]byte, error)
}

// engineLibDir holds the session host's terminal engine libraries
// (scripts/build-libghostty-desktop.sh). While it exists the daemon is built
// with them (-tags ghosttyvt), so Settings → Labs → Session host can work.
const engineLibDir = "ghostty-libs"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "mewla-dev: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	tmpDir := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create tmp dir: %w", err)
	}

	runner := &devRunner{
		root:       root,
		binary:     filepath.Join(tmpDir, "mewla-dev"),
		daemonArgs: args,
		stdout:     stdout,
		stderr:     stderr,
		webDirty:   true,
		goBuild:    runGoBuild,
		pprofAddr:  devPprofAddr(),
	}

	if err := runner.rebuild(); err != nil {
		return err
	}
	if err := runner.start(); err != nil {
		return err
	}
	childDone := runner.child.done
	defer func() {
		_ = runner.stop(syscall.SIGINT)
	}()

	tree, err := newWatchTree(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer tree.Close()

	fmt.Fprintf(stderr, "  Dev     Watching %s\n", tree.root)
	if runner.pprofAddr != "" {
		fmt.Fprintf(stderr, "  Dev     Profiler http://%s/debug/pprof/ (MEWLA_PPROF_ADDR= turns it off)\n", runner.pprofAddr)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	pending := map[string]struct{}{}
	var debounce *time.Timer
	var debounceC <-chan time.Time

	for {
		select {
		case <-signals:
			fmt.Fprintln(stderr, "\nmewla-dev shutting down...")
			return nil
		case err := <-childDone:
			// The daemon is the service; the watcher must never remain alive as
			// a false-positive runtime owner after its child exits. Surface the
			// failure so supervisors and developers can restart or diagnose it.
			runner.child = nil
			return unexpectedDaemonExit(err)
		case event, ok := <-tree.watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if event.Op == 0 {
				continue
			}

			if err := tree.handleEvent(event); err != nil {
				fmt.Fprintf(stderr, "mewla-dev watch error: %v\n", err)
			}

			rel, ok := tree.relevantPath(event.Name)
			if !ok {
				continue
			}

			pending[rel] = struct{}{}
			if webSourcePath(rel) {
				runner.webDirty = true
			}
			debounce = resetTimer(debounce)
			debounceC = debounce.C
		case err, ok := <-tree.watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			fmt.Fprintf(stderr, "mewla-dev watch error: %v\n", err)
		case <-debounceC:
			debounceC = nil

			changedFiles := mapKeys(pending)
			pending = map[string]struct{}{}

			fmt.Fprintf(stderr, "\nmewla-dev detected changes: %s\n", strings.Join(changedFiles, ", "))
			if err := runner.rebuild(); err != nil {
				fmt.Fprintf(stderr, "mewla-dev build failed:\n%v\n", err)
				continue
			}
			if err := runner.restart(); err != nil {
				return err
			}
			childDone = runner.child.done
		}
	}
}

// devPprofAddr is where a dev daemon serves its profiler: a free loopback
// port, the same across restarts of this runner's child, so "what is it
// spending CPU on" can always be answered with `go tool pprof` and no restart.
// A MEWLA_PPROF_ADDR already in the environment wins, empty included (off).
// Release daemons never get this; only mewla-dev sets it.
func devPprofAddr() string {
	if _, set := os.LookupEnv("MEWLA_PPROF_ADDR"); set {
		return ""
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ""
	}
	defer listener.Close()
	return listener.Addr().String()
}

func unexpectedDaemonExit(err error) error {
	if err == nil {
		return errors.New("daemon exited unexpectedly")
	}
	return fmt.Errorf("daemon exited unexpectedly: %w", err)
}

func newWatchTree(root string) (*watchTree, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	tree := &watchTree{
		root:    root,
		watcher: watcher,
		watched: make(map[string]struct{}),
	}
	if err := tree.addTree(root); err != nil {
		watcher.Close()
		return nil, err
	}
	return tree, nil
}

func (t *watchTree) Close() error {
	return t.watcher.Close()
}

func (t *watchTree) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		// Only source trees that contribute to the daemon or embedded app.
		if filepath.Dir(path) == t.root && d.Name() != "daemon" && d.Name() != "client" && d.Name() != "scripts" {
			return filepath.SkipDir
		}
		// The web UI is client/ui and client/core; the native hosts never reach it.
		if filepath.Dir(path) == filepath.Join(t.root, "client") && d.Name() != "ui" && d.Name() != "core" {
			return filepath.SkipDir
		}
		if ignoredDirName(d.Name()) {
			return filepath.SkipDir
		}

		clean := filepath.Clean(path)
		if _, ok := t.watched[clean]; ok {
			return nil
		}
		if err := t.watcher.Add(clean); err != nil {
			return fmt.Errorf("watch %s: %w", clean, err)
		}
		t.watched[clean] = struct{}{}
		return nil
	})
}

func (t *watchTree) handleEvent(event fsnotify.Event) error {
	path := filepath.Clean(event.Name)

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		t.removeTree(path)
	}
	if event.Op&fsnotify.Create == 0 {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() || ignoredDirName(info.Name()) {
		return nil
	}
	return t.addTree(path)
}

func (t *watchTree) removeTree(root string) {
	clean := filepath.Clean(root)
	prefix := clean + string(os.PathSeparator)
	toRemove := make([]string, 0)

	for watched := range t.watched {
		if watched == clean || strings.HasPrefix(watched, prefix) {
			toRemove = append(toRemove, watched)
		}
	}

	for _, watched := range toRemove {
		delete(t.watched, watched)
		_ = t.watcher.Remove(watched)
	}
}

func (t *watchTree) relevantPath(path string) (string, bool) {
	rel, err := filepath.Rel(t.root, path)
	if err != nil {
		return "", false
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}

	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if ignoredDirName(part) {
			return "", false
		}
	}

	if webSourcePath(filepath.ToSlash(rel)) {
		return filepath.ToSlash(rel), true
	}
	if !strings.HasPrefix(filepath.ToSlash(rel), "daemon/") {
		return "", false
	}
	base := filepath.Base(rel)
	if base == "go.mod" || base == "go.sum" {
		return filepath.ToSlash(rel), true
	}
	if strings.HasSuffix(base, "_test.go") {
		return "", false
	}
	switch filepath.Ext(base) {
	case ".go":
		return filepath.ToSlash(rel), true
	default:
		return "", false
	}
}

// Web source and daemon code must be packaged together; an old package can
// otherwise keep calling operations the current daemon has removed.
func webSourcePath(rel string) bool {
	if strings.HasPrefix(rel, "client/ui/") || strings.HasPrefix(rel, "client/core/") {
		return !strings.Contains(rel, ".test.") && !strings.Contains(rel, ".spec.") && !strings.Contains(rel, "/testing/")
	}
	return rel == "scripts/build-web-ui.sh"
}

func (r *devRunner) rebuild() error {
	if r.webDirty {
		cmd := exec.Command("bash", filepath.Join(r.root, "..", "scripts", "build-web-ui.sh"))
		cmd.Dir = filepath.Dir(r.root)
		cmd.Stdout, cmd.Stderr = r.stdout, r.stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("web UI build failed: %w", err)
		}
		r.webDirty = false
	}
	flags, err := publisherFlags(r.root)
	if err != nil {
		return err
	}
	return r.buildDaemon(flags)
}

// buildDaemon builds cmd/mewla into the runner's binary, with the session
// host's terminal engine when its libraries are present.
func (r *devRunner) buildDaemon(flags string) error {
	built := r.binary + ".building"
	args := func(extra ...string) []string {
		return append([]string{"build", "-ldflags=" + flags, "-o", built}, append(extra, "./cmd/mewla")...)
	}
	if r.engineLibsPresent() {
		// A reload must never leave the daemon down: a tagged build that fails
		// (cgo, the libraries) is followed by one untagged build, and the
		// daemon runs on without the terminal engine.
		output, err := r.goBuild(r.root, []string{"CGO_ENABLED=1"}, args("-tags", "ghosttyvt"))
		if err == nil {
			return commitBuiltBinary(built, r.binary)
		}
		_ = os.Remove(built)
		logPath := r.binary + "-tagged-build.log"
		_ = os.WriteFile(logPath, output, 0o644)
		fmt.Fprintf(r.stderr, "mewla-dev: building with the session host's terminal engine (%s) failed: %v; built without it, so the Session host Labs switch stays off; details in %s\n", engineLibDir, firstLine(output, err), logPath)
	}
	output, err := r.goBuild(r.root, []string{"CGO_ENABLED=0"}, args())
	if len(output) > 0 {
		_, _ = r.stderr.Write(output)
	}
	if err != nil {
		_ = os.Remove(built)
		return fmt.Errorf("go build failed: %w", err)
	}
	if err := commitBuiltBinary(built, r.binary); err != nil {
		_ = os.Remove(built)
		return err
	}
	return nil
}

func (r *devRunner) engineLibsPresent() bool {
	info, err := os.Stat(filepath.Join(r.root, engineLibDir))
	return err == nil && info.IsDir()
}

func runGoBuild(dir string, env, args []string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd.CombinedOutput()
}

// firstLine is the first line of a failed build's output, or its error.
func firstLine(output []byte, err error) string {
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return err.Error()
}

func commitBuiltBinary(built, dest string) error {
	if err := os.Rename(built, dest); err != nil {
		return fmt.Errorf("install built mewla: %w", err)
	}
	return nil
}

func (r *devRunner) restart() error {
	if err := r.stop(syscall.SIGINT); err != nil {
		return err
	}
	return r.start()
}

func (r *devRunner) start() error {
	cmd := exec.Command(r.binary, r.daemonArgs...)
	cmd.Dir = r.root
	if r.pprofAddr != "" {
		cmd.Env = append(os.Environ(), "MEWLA_PPROF_ADDR="+r.pprofAddr)
	}
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	cmd.Stdin = os.Stdin
	// Bind the daemon child to this watcher so an abrupt watcher death cannot
	// leave an orphan holding the single-owner state lock (see pdeathsig_*.go).
	cmd.SysProcAttr = parentDeathSignal()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	r.child = &runningProcess{
		cmd:  cmd,
		done: make(chan error, 1),
	}

	go func(child *runningProcess) {
		child.done <- child.cmd.Wait()
	}(r.child)

	return nil
}

func (r *devRunner) stop(sig os.Signal) error {
	if r.child == nil {
		return nil
	}

	child := r.child
	r.child = nil

	select {
	case err := <-child.done:
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("daemon exited unexpectedly: %w", err)
		}
		return nil
	default:
	}

	if err := child.cmd.Process.Signal(sig); err != nil && !isDoneProcess(err) {
		return fmt.Errorf("signal daemon: %w", err)
	}

	select {
	case err := <-child.done:
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("wait for daemon stop: %w", err)
		}
		return nil
	case <-time.After(stopTimeout):
		if err := child.cmd.Process.Kill(); err != nil && !isDoneProcess(err) {
			return fmt.Errorf("kill daemon: %w", err)
		}
		err := <-child.done
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("wait for daemon kill: %w", err)
		}
		return nil
	}
}

func resetTimer(timer *time.Timer) *time.Timer {
	if timer == nil {
		return time.NewTimer(restartDebounce)
	}

	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(restartDebounce)
	return timer
}

func ignoredDirName(name string) bool {
	switch name {
	case ".git", "bin", "tmp", "node_modules", "dist", ".expo", "build", ".gradle", "Pods", ".idea":
		return true
	default:
		return false
	}
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isExpectedExit(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func isDoneProcess(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrProcessDone)
}

// Use the same validated public identities as release and local build scripts.
// Resolve from the source root, never from the daemon's runtime state.
func publisherFlags(daemonRoot string) (string, error) {
	cmd := exec.Command("python3", filepath.Join(daemonRoot, "..", "scripts", "plugin-publisher-flags.py"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("publisher configuration: %w: %s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
