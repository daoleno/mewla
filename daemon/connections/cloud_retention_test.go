//go:build linux || darwin

package connections

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installRetentionFixture(t *testing.T, root, binary string) string {
	t.Helper()
	generation, lease, err := newCloudGeneration(root, "install-")
	if err != nil {
		t.Fatal(err)
	}
	defer closeCloudLease(lease)
	path := filepath.Join(generation, "bin", binary)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo aws-cli/2.39.3; exit; fi\necho ready\nread code\ncat \"$0.asset\"\n"
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".asset", []byte("lazy resource survives\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = activateCloudGeneration(root, binary, path); err != nil {
		t.Fatal(err)
	}
	pruneCloudGenerations(root, binary)
	return path
}
func waitCloudFixture(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("fixture did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestCloudGenerationRetentionAfterManyUpdates(t *testing.T) {
	for _, binary := range []string{"aws", "gcloud"} {
		t.Run(binary, func(t *testing.T) {
			root := t.TempDir()
			previous := ""
			for i := 0; i < 12; i++ {
				active := installRetentionFixture(t, root, binary)
				dirs := cloudDirectories(filepath.Join(root, "versions"))
				want := 2
				if i == 0 {
					want = 1
				}
				if len(dirs) != want {
					t.Fatalf("update %d retained %d generations, want %d", i, len(dirs), want)
				}
				if previous != "" {
					rollback, err := filepath.EvalSymlinks(filepath.Join(cloudGeneration(active), "previous"))
					if err != nil || rollback != cloudGeneration(previous) {
						t.Fatal("previous generation not preserved", rollback, err)
					}
					if _, err = os.Stat(previous); err != nil {
						t.Fatal("rollback executable deleted")
					}
				}
				previous = active
			}
		})
	}
}
func TestCloudRetentionKeepsRunningLoginUntilExit(t *testing.T) {
	root := t.TempDir()
	old := installRetentionFixture(t, root, "aws")
	login, err := startCloudLogin(old, nil, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	defer login.cancel()
	waitCloudFixture(t, func() bool { return strings.Contains(login.output.String(), "ready") })
	for i := 0; i < 6; i++ {
		installRetentionFixture(t, root, "aws")
	}
	if _, err = os.Stat(old); err != nil {
		t.Fatal("running login lost its SDK")
	}
	if n := len(cloudDirectories(filepath.Join(root, "versions"))); n != 3 {
		t.Fatal("expected current + previous + leased generation", n)
	}
	login.stdin.Write([]byte("continue\n"))
	<-login.done
	if login.err != nil || !strings.Contains(login.output.String(), "lazy resource survives") {
		t.Fatal("running command lost a lazily loaded file", login.err, login.output.String())
	}
	pruneCloudGenerations(root, "aws")
	if n := len(cloudDirectories(filepath.Join(root, "versions"))); n != 2 {
		t.Fatal("released generation was not pruned", n)
	}
}
func TestCloudLeaseSurvivesParentDescriptorClose(t *testing.T) {
	root := t.TempDir()
	old := installRetentionFixture(t, root, "aws")
	binary, lease, err := holdCloudBinary(old)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary)
	inheritCloudLease(cmd, lease)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output := &boundedBuffer{limit: 1024}
	cmd.Stdout = output
	cmd.Stderr = output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	// This models the daemon disappearing: only the child now owns the lease.
	closeCloudLease(lease)
	waitCloudFixture(t, func() bool { return strings.Contains(output.String(), "ready") })
	for i := 0; i < 5; i++ {
		installRetentionFixture(t, root, "aws")
	}
	if _, err = os.Stat(old); err != nil {
		t.Fatal("child's inherited lease was ignored")
	}
	input.Write([]byte("continue\n"))
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "lazy resource survives") {
		t.Fatal(output.String())
	}
	pruneCloudGenerations(root, "aws")
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("exited child still pins old SDK")
	}
}
func TestCloudSelectionLeaseCoversCommandLaunchGap(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "aws")
	old := installRetentionFixture(t, root, "aws")
	c := newCloudRuntime(dir)
	binary, lease, err := c.acquireExecutable("aws")
	if err != nil {
		t.Fatal(err)
	}
	if binary != old {
		t.Fatal("wrong selected generation")
	}
	for i := 0; i < 5; i++ {
		installRetentionFixture(t, root, "aws")
	}
	out, err := runCloudProcess(context.Background(), binary, []string{"--version"}, os.Environ())
	if err != nil || !strings.Contains(out.Stdout, "2.39.3") {
		t.Fatal("selected executable disappeared before launch", err)
	}
	closeCloudLease(lease)
	pruneCloudGenerations(root, "aws")
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("released selection still retained")
	}
}
func TestCloudRetentionProtectsStagingAndFailedActivationRollback(t *testing.T) {
	root := t.TempDir()
	old := installRetentionFixture(t, root, "aws")
	current := installRetentionFixture(t, root, "aws")
	staged, lease, err := newCloudGeneration(root, "install-")
	if err != nil {
		t.Fatal(err)
	}
	pruneCloudGenerations(root, "aws")
	if _, err = os.Stat(staged); err != nil {
		t.Fatal("active installer staging deleted")
	}
	// An invalid target cannot change the current/previous pair.
	if err = activateCloudGeneration(root, "aws", filepath.Join(root, "outside", "aws")); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	active, _ := filepath.EvalSymlinks(filepath.Join(root, "bin", "aws"))
	previous, _ := filepath.EvalSymlinks(filepath.Join(cloudGeneration(current), "previous"))
	if active != current || previous != cloudGeneration(old) {
		t.Fatal("failed activation changed rollback owners")
	}
	closeCloudLease(lease)
	pruneCloudGenerations(root, "aws")
	if _, err = os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("abandoned staging not reclaimed")
	}
}
func TestCloudBackupsBoundedAndHeldBackupSurvives(t *testing.T) {
	root := t.TempDir()
	held, release, err := newCloudBackup(root, "first")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		path, done, err := newCloudBackup(root, fmt.Sprintf("backup-%02d", i))
		if err != nil {
			t.Fatal(err)
		}
		// Stable ordering independent of filesystem timestamp resolution.
		at := time.Now().Add(time.Duration(i+1) * time.Second)
		os.Chtimes(path, at, at)
		done()
	}
	if _, err = os.Stat(held); err != nil {
		t.Fatal("backup in active mutation deleted")
	}
	if n := len(cloudDirectories(filepath.Join(root, "backups"))); n != 3 {
		t.Fatal("expected newest two plus held backup", n)
	}
	release()
	if n := len(cloudDirectories(filepath.Join(root, "backups"))); n != 2 {
		t.Fatal("backup retention is unbounded", n)
	}
	for _, name := range []string{"backup-10", "backup-11"} {
		if _, err = os.Stat(filepath.Join(root, "backups", name)); err != nil {
			t.Fatal("newest backup deleted", name)
		}
	}
}

func TestCloudRetentionReclaimsAbandonedFirstInstallStaging(t *testing.T) {
	root := t.TempDir()
	installRetentionFixture(t, root, "aws")
	stage, lease, err := newCloudGeneration(root, "install-")
	if err != nil {
		t.Fatal(err)
	}
	closeCloudLease(lease)
	pruneCloudGenerations(root, "aws")
	if _, err = os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("abandoned staging mistaken for rollback generation")
	}
}

func TestCloudLeaseNeverWritesToSystemInstallation(t *testing.T) {
	root := t.TempDir()
	binary := fixtureAWS(t, filepath.Join(root, "versions", "vendor"), "2.39.3")
	out, err := runCloudProcess(context.Background(), binary, []string{"--version"}, os.Environ())
	if err != nil || out.ExitCode != 0 {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "generations.lock"), filepath.Join(root, ".mewla-managed"), filepath.Join(filepath.Dir(binary), ".lease")} {
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("vendor-owned installation was modified", path)
		}
	}
}
