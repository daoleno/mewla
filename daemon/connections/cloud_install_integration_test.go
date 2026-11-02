package connections

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Explicit opt-in: downloads and verifies the real official package in test
// state. It neither starts a daemon nor reads/writes cloud credentials.
func TestOfficialAWSManagedUpgrade(t *testing.T) {
	if os.Getenv("MEWLA_CLOUD_INSTALL_SMOKE") != "1" {
		t.Skip("set MEWLA_CLOUD_INSTALL_SMOKE=1 for the official download test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("real package proof currently runs on Linux")
	}
	dir := t.TempDir()
	c := newCloudRuntime(dir)
	delete(c.providers, "gcp")
	root := filepath.Join(dir, "aws")
	old := fixtureAWS(t, filepath.Join(root, "versions", "old"), "2.36.20")
	activateFixture(t, root, old)
	writeUpdateState(t, c, 8*24*time.Hour)
	c.enableUpdates()
	defer func() { c.updates.cancel(); c.updates.wg.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := c.prepare(ctx, "aws"); err != nil {
		t.Fatal(err)
	}
	info := c.info("aws")
	if info == nil || !info.Managed || !cloudVersionLess("2.36.20", info.Version) {
		t.Fatalf("old managed generation did not update: %+v", info)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("running old generation removed")
	}
	t.Logf("Official signed package automatically upgraded managed AWS CLI 2.36.20 to %s; old generation retained", info.Version)
}
