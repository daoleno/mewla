package connections

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWSRenamePreservesOtherProfilesAndPermissions(t *testing.T) {
	m, config, creds, _ := testCloudManager(t)
	os.WriteFile(config, []byte("# keep\n[profile old]\nregion = us-east-1\nlogin_session = shared-identity\n[profile other]\nsource_profile = old\nrole_arn = role\n[sso-session test]\nsso_region = us-east-1\n"), 0600)
	os.WriteFile(creds, []byte("[old]\naws_access_key_id = fake\n[other]\naws_access_key_id = other-fake\n"), 0600)
	if err := m.syncAWSProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	r := m.awsProfileRecord("old")
	r.Account.Enabled = true
	m.setGroup(r, "write", true)
	result, err := m.Handle(context.Background(), Request{Action: "cloud_rename", ID: r.Account.ID, Profile: "team-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.Profile != "team-admin" || !result.Account.Enabled {
		t.Fatal("metadata lost")
	}
	if result.Account.Access.Write != "allowed" {
		t.Fatal("write permission lost")
	}
	raw, _ := os.ReadFile(config)
	if !strings.Contains(string(raw), "source_profile = team-admin") || !strings.Contains(string(raw), "login_session = shared-identity") || !strings.Contains(string(raw), "[sso-session test]\nsso_region = us-east-1") {
		t.Fatal("related/other config damaged")
	}
	raw, _ = os.ReadFile(creds)
	if !strings.Contains(string(raw), "[team-admin]\naws_access_key_id = fake") || !strings.Contains(string(raw), "[other]\naws_access_key_id = other-fake") {
		t.Fatal("credentials rename failed")
	}
	backups, _ := filepath.Glob(filepath.Join(m.cloud.dir, "backups", "rename-*", "config"))
	if len(backups) != 1 {
		t.Fatal("backup absent")
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup not private")
	}
	for _, name := range []string{"other", "../escape", "x\ny", "-flag", "has space"} {
		if _, err = m.Handle(context.Background(), Request{Action: "cloud_rename", ID: result.Account.ID, Profile: name}); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	raw, _ = os.ReadFile(config)
	raw = renameAWSSections(raw, "team-admin", "from-terminal", true)
	os.WriteFile(config, raw, 0600)
	raw, _ = os.ReadFile(creds)
	raw = renameAWSSections(raw, "team-admin", "from-terminal", false)
	os.WriteFile(creds, raw, 0600)
	if err = m.syncAWSProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.awsProfileRecord("team-admin") != nil || m.awsProfileRecord("from-terminal") == nil {
		t.Fatal("terminal rename not reflected")
	}
}
