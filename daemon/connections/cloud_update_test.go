package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type updatingAWS struct {
	awsCLI
	calls     atomic.Int32
	installFn func(context.Context, string) error
}

func (p *updatingAWS) install(ctx context.Context, dir string) error {
	p.calls.Add(1)
	return p.installFn(ctx, dir)
}
func (p *updatingAWS) installPaths() []string { return nil }

func fixtureAWS(t *testing.T, dir, version string) string {
	t.Helper()
	path := filepath.Join(dir, "aws")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' 'aws-cli/"+version+" Python/test'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func activateFixture(t *testing.T, dir, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "next")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(link, filepath.Join(dir, "bin", "aws")); err != nil {
		t.Fatal(err)
	}
}
func updateRuntime(t *testing.T, p *updatingAWS, dir string) *cloudRuntime {
	t.Helper()
	c := newCloudRuntime(dir)
	c.providers = map[string]cloudProvider{"aws": p}
	c.enableUpdates()
	t.Cleanup(func() { c.updates.cancel(); c.updates.wg.Wait() })
	return c
}
func writeUpdateState(t *testing.T, c *cloudRuntime, age time.Duration) {
	t.Helper()
	state := cloudUpdateState{Attempted: time.Now().Add(-age), Succeeded: time.Now().Add(-age)}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(c.dir, "aws", "update.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestManagedCloudUpgradeRetainsRunningGenerationAndDailyBudget(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "aws")
	old := fixtureAWS(t, filepath.Join(root, "versions", "old"), "2.36.20")
	activateFixture(t, root, old)
	started, release := make(chan struct{}), make(chan struct{})
	p := &updatingAWS{installFn: func(ctx context.Context, dir string) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		next := fixtureAWS(t, filepath.Join(dir, "versions", "new"), "2.39.3")
		activateFixture(t, dir, next)
		return nil
	}}
	c := updateRuntime(t, p, dir)
	writeUpdateState(t, c, 25*time.Hour)
	selected, err := c.executable("aws")
	if err != nil || selected != old {
		t.Fatalf("old generation unavailable while updating: %s %v", selected, err)
	}
	<-started
	for i := 0; i < 10; i++ {
		c.refresh("aws", false)
	}
	done := c.refresh("aws", false)
	close(release)
	<-done
	next, err := c.executable("aws")
	if err != nil || next == old {
		t.Fatal("upgrade did not activate", next, err)
	}
	out, err := runCloudProcess(context.Background(), selected, []string{"--version"}, os.Environ())
	if err != nil || !strings.Contains(out.Stdout, "2.36.20") {
		t.Fatal("running command lost its generation")
	}
	if p.calls.Load() != 1 {
		t.Fatal("parallel readers downloaded repeatedly")
	}
	info := c.info("aws")
	if info == nil || !info.Managed || info.Version != "2.39.3" {
		t.Fatalf("wrong details: %+v", info)
	}
	c.updates.cancel()
	c.updates.wg.Wait()
	restarted := updateRuntime(t, p, dir)
	if restarted.refresh("aws", false) != nil || p.calls.Load() != 1 {
		t.Fatal("restart bypassed daily budget")
	}
}
func TestCloudFirstUseAndLongGapWaitForUpdate(t *testing.T) {
	for _, age := range []time.Duration{0, 8 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			dir := t.TempDir()
			started, release := make(chan struct{}), make(chan struct{})
			p := &updatingAWS{installFn: func(ctx context.Context, dir string) error {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				activateFixture(t, dir, fixtureAWS(t, filepath.Join(dir, "versions", "new"), "2.39.3"))
				return nil
			}}
			c := updateRuntime(t, p, dir)
			if age != 0 {
				root := filepath.Join(dir, "aws")
				activateFixture(t, root, fixtureAWS(t, filepath.Join(root, "versions", "old"), "2.36.20"))
				writeUpdateState(t, c, age)
			}
			finished := make(chan error, 1)
			go func() { finished <- c.prepare(context.Background(), "aws") }()
			<-started
			select {
			case <-finished:
				t.Fatal("first use did not wait")
			default:
			}
			close(release)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if c.info("aws").Version != "2.39.3" {
				t.Fatal("first use selected old binary")
			}
		})
	}
}
func TestCloudFailedInstallFallsBackToMinimumPATH(t *testing.T) {
	for _, version := range []string{"2.36.20", "2.31.9"} {
		t.Run(version, func(t *testing.T) {
			pathDir := t.TempDir()
			path := fixtureAWS(t, pathDir, version)
			t.Setenv("PATH", pathDir)
			p := &updatingAWS{installFn: func(context.Context, string) error { return errors.New("injected offline") }}
			c := updateRuntime(t, p, t.TempDir())
			err := c.prepare(context.Background(), "aws")
			if version == "2.31.9" {
				if err == nil {
					t.Fatal("obsolete fallback accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("working fallback blocked", err)
			}
			selected, err := c.executable("aws")
			if err != nil || selected != path {
				t.Fatal("PATH fallback not selected")
			}
			info := c.info("aws")
			if info == nil || info.Managed || info.Version != version {
				t.Fatalf("fallback details missing: %+v", info)
			}
			if c.refresh("aws", false) != nil || p.calls.Load() != 1 {
				t.Fatal("failed update retried within a day")
			}
		})
	}
}
func TestCloudBlockedStateDirectoryStillUsesPATH(t *testing.T) {
	pathDir := t.TempDir()
	fixtureAWS(t, pathDir, "2.36.20")
	t.Setenv("PATH", pathDir)
	blocked := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocked, []byte("blocked"), 0600)
	p := &updatingAWS{installFn: func(context.Context, string) error {
		t.Error("installer reached despite blocked directory")
		return nil
	}}
	c := updateRuntime(t, p, blocked)
	if err := c.prepare(context.Background(), "aws"); err != nil {
		t.Fatal(err)
	}
	if c.refresh("aws", false) != nil {
		t.Fatal("unwritable state caused retry loop")
	}
}
func TestCloudVersionRequiresNoEnabledProfile(t *testing.T) {
	m, _, _, _ := testCloudManager(t)
	m.cloud.run = func(context.Context, string, []string, []string) (cloudOutput, error) {
		return cloudOutput{Stdout: "aws-cli/2.39.3"}, nil
	}
	out := mustHandle(t, m, Request{Action: "cloud_run", Integration: "aws", CLIArgs: []string{"--version"}})
	if out.CLIOutput == nil || out.CLIOutput.Stdout != "aws-cli/2.39.3" {
		t.Fatal("version probe incorrectly gated")
	}
}

func TestCloudVersionComparison(t *testing.T) {
	for _, pair := range [][2]string{{"2.9.0", "2.39.3"}, {"591.0.0", "592.0.0"}, {"2.39.2", "2.39.3"}} {
		if !cloudVersionLess(pair[0], pair[1]) || cloudVersionLess(pair[1], pair[0]) || cloudVersionLess(pair[0], pair[0]) {
			t.Fatal(pair)
		}
	}
}
