package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// An update never replaces the installed binary until the new one has
// answered as a running daemon. The old process stays alive and in charge:
//
//  1. Stage writes the new binary beside the installed one (mewla.next) and
//     checks that it executes and reports the expected version.
//  2. The old daemon shuts down (locks and ports released). The same process
//     then backs up the databases and runs mewla.next as its child.
//  3. Once the child's /health answers as the new version, the old process
//     stops it, moves mewla.next over the installed path (keeping the old one
//     as mewla.prev) and execs the installed path: now the new version.
//  4. If the child exits, hangs or never answers, the old process kills it,
//     puts the databases back, records the version as bad and execs the
//     installed path again: still the old version.
//
// Until step 3's rename the installed file is untouched, so a crash, a power
// cut or a reboot at any point leaves the old, working binary in place.
const (
	StagedSuffix = ".next"
	PrevSuffix   = ".prev"
	// ChildEnv marks the staged daemon while it is on trial: it must not
	// recover, back up or update anything itself.
	ChildEnv    = "MEWLA_UPDATE_TRIAL_CHILD"
	failedFile  = "update-failed.json"
	backupDir   = "update-backup"
	backupIndex = "index.json"
)

// Stage writes binary beside installed (so adopting it is one rename on one
// filesystem) and proves it runs and reports version. A failure here, such as
// a full disk or a binary for another platform, changes nothing.
func Stage(installed string, binary []byte, version string) (string, error) {
	target, err := filepath.EvalSymlinks(installed)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("stat executable: %w", err)
	}
	staged := target + StagedSuffix
	temp, err := os.CreateTemp(filepath.Dir(target), ".mewla-update-*")
	if err != nil {
		return "", fmt.Errorf("create update beside executable: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(info.Mode().Perm() | 0o100); err != nil {
		temp.Close()
		return "", fmt.Errorf("set update permissions: %w", err)
	}
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return "", fmt.Errorf("write update: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", fmt.Errorf("sync update: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close update: %w", err)
	}
	if err := os.Rename(tempPath, staged); err != nil {
		return "", fmt.Errorf("stage update: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, staged, "version", "--json").Output()
	var reported struct {
		Version string `json:"version"`
	}
	if err != nil || json.Unmarshal(output, &reported) != nil || reported.Version != version {
		os.Remove(staged)
		if err != nil {
			return "", fmt.Errorf("the new binary does not run here: %w", err)
		}
		return "", fmt.Errorf("the new binary reports version %q, not %s", reported.Version, version)
	}
	return staged, nil
}

// Handover is one attempt to move a stopped daemon onto a staged binary.
type Handover struct {
	Installed string   // the installed binary; replaced only on success
	Staged    string   // the binary from Stage
	Version   string   // the version Staged must report
	Args      []string // the daemon's own arguments, repeated for the child
	Env       []string
	StateDir  string
	BrainRoot string
	// Gate is how long the child has to answer before it is given up on.
	Gate time.Duration
	// Healthy returns nil once the child answers as Version.
	Healthy        func() error
	StopGrace      time.Duration
	Stdout, Stderr io.Writer
}

// Outcome says whether the staged binary was adopted. When it was not, the
// installed binary and the databases are as they were.
type Outcome struct {
	Adopted bool
	Reason  string
	// Bad: the new version itself failed. It is recorded and not retried.
	// Stopping, a full disk and the like leave it false.
	Bad bool
}

// Run starts the staged binary as a child and adopts it only after it answers.
// ctx ends the attempt early (the service is being stopped).
func (h Handover) Run(ctx context.Context) Outcome {
	if h.Gate <= 0 {
		h.Gate = 60 * time.Second
	}
	if h.StopGrace <= 0 {
		h.StopGrace = 30 * time.Second
	}
	if err := backupDatabases(h.StateDir, h.BrainRoot); err != nil {
		os.Remove(h.Staged)
		return Outcome{Reason: "back up databases: " + err.Error()}
	}
	fail := func(bad bool, reason string) Outcome {
		restoreErr := RestoreDatabases(h.StateDir)
		os.Remove(h.Staged)
		if restoreErr != nil {
			reason += "; restoring databases: " + restoreErr.Error()
		}
		return Outcome{Reason: reason, Bad: bad}
	}
	child := exec.Command(h.Staged, h.Args...)
	child.Env = append(append([]string(nil), h.Env...), ChildEnv+"=1")
	child.Stdout, child.Stderr = h.Stdout, h.Stderr
	if err := child.Start(); err != nil {
		return fail(true, "the new version could not start: "+err.Error())
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	stop := func() {
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(h.StopGrace):
			_ = child.Process.Kill()
			<-exited
		}
	}

	deadline := time.NewTimer(h.Gate)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for answered := false; !answered; {
		select {
		case <-ctx.Done():
			stop()
			return fail(false, "stopped while updating")
		case err := <-exited:
			return fail(true, fmt.Sprintf("the new version exited before it answered: %v", err))
		case <-deadline.C:
			_ = child.Process.Kill()
			<-exited
			return fail(true, fmt.Sprintf("the new version did not answer within %s", h.Gate))
		case <-tick.C:
			answered = h.Healthy() == nil
		}
	}
	stop()
	if err := promote(h.Installed, h.Staged); err != nil {
		return fail(false, "install the new version: "+err.Error())
	}
	discardBackup(h.StateDir)
	return Outcome{Adopted: true}
}

// promote makes staged the installed binary and keeps the old one as .prev.
// The installed path never goes missing: the old file is linked to .prev
// first, then staged is renamed over it in one step.
func promote(installed, staged string) error {
	target, err := filepath.EvalSymlinks(installed)
	if err != nil {
		return err
	}
	prev := target + PrevSuffix
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Link(target, prev); err != nil {
		return fmt.Errorf("keep the previous version: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Failure is the last update that was rolled back.
type Failure struct {
	Version string    `json:"version"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

// RecordFailure remembers a rolled-back version so it is not tried again.
func RecordFailure(stateDir string, failure Failure) error {
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, failedFile), append(raw, '\n'), 0o600)
}

// LastFailure returns the recorded rollback, if any.
func LastFailure(stateDir string) (Failure, bool) {
	raw, err := os.ReadFile(filepath.Join(stateDir, failedFile))
	if err != nil {
		return Failure{}, false
	}
	var failure Failure
	if json.Unmarshal(raw, &failure) != nil || failure.Version == "" {
		return Failure{}, false
	}
	return failure, true
}

// A newer schema is something an older binary refuses to open, so the
// databases are copied before the new version first touches them and put back
// if it is not adopted. The index exists only while a handover is in flight; if
// the machine stops in that window, the next start restores them
// (RecoverInterrupted) before opening anything.

type backupEntry struct {
	Original string `json:"original"`
	Copy     string `json:"copy"`
}

var backupSkipDirs = map[string]bool{
	"t": true, "tmp": true, "cache": true, "build": true, "build-tmp": true, "artifacts": true,
	"browsers": true, "downloads": true, "workspace": true, "worktrees": true, "node_modules": true,
	backupDir: true,
}

func databaseFiles(roots []string) []string {
	seen := map[string]bool{}
	var files []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if entry.IsDir() {
				if path != root && (backupSkipDirs[entry.Name()] || depth > 3 || strings.HasPrefix(entry.Name(), "legacy-")) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && strings.HasSuffix(path, ".db") && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			return nil
		})
	}
	return files
}

func sidecars(database string) []string {
	return []string{database, database + "-wal", database + "-shm", database + "-journal"}
}

func backupDatabases(stateDir, brainRoot string) error {
	dir := filepath.Join(stateDir, backupDir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var entries []backupEntry
	for number, database := range databaseFiles([]string{stateDir, brainRoot}) {
		for _, file := range sidecars(database) {
			if _, err := os.Stat(file); err != nil {
				continue
			}
			copyPath := filepath.Join(dir, fmt.Sprintf("%d-%s", number, filepath.Base(file)))
			if err := copyFile(file, copyPath); err != nil {
				_ = os.RemoveAll(dir)
				return err
			}
			entries = append(entries, backupEntry{Original: file, Copy: copyPath})
		}
		// A database that does not exist yet must not exist after a restore.
		entries = append(entries, backupEntry{Original: database})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	// The index is written last: no index, no backup to trust.
	if err := atomicfile.Write(filepath.Join(dir, backupIndex), raw, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	return nil
}

// RestoreDatabases puts the backed-up databases back and removes the sidecars
// (-wal, -shm) the new version left, which would otherwise replay a newer log
// onto an older file. It is a no-op when no handover is in flight.
func RestoreDatabases(stateDir string) error {
	dir := filepath.Join(stateDir, backupDir)
	raw, err := os.ReadFile(filepath.Join(dir, backupIndex))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = os.RemoveAll(dir)
			return nil
		}
		return err
	}
	var entries []backupEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Copy != "" {
			continue
		}
		for _, file := range sidecars(entry.Original) {
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, entry := range entries {
		if entry.Copy == "" {
			continue
		}
		if err := copyFile(entry.Copy, entry.Original); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

func discardBackup(stateDir string) { _ = os.RemoveAll(filepath.Join(stateDir, backupDir)) }

// RecoverInterrupted restores the databases when a previous handover was cut
// short (power loss, reboot, SIGKILL) and removes its staged binary. Call it
// with the state lock held, before any database is opened.
func RecoverInterrupted(stateDir, installed string) error {
	if os.Getenv(ChildEnv) != "" {
		return nil // the parent's handover is the one in flight
	}
	if _, err := os.Stat(filepath.Join(stateDir, backupDir)); err != nil {
		return nil
	}
	err := RestoreDatabases(stateDir)
	if installed != "" {
		if target, evalErr := filepath.EvalSymlinks(installed); evalErr == nil {
			_ = os.Remove(target + StagedSuffix)
		}
	}
	return err
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(to), ".mewla-copy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, in); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, to)
}
