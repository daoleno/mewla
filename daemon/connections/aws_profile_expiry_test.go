package connections

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLoginTimingCache(t *testing.T, config, session, arn, expiry string) {
	t.Helper()
	sum := sha256.Sum256([]byte(session))
	dir := filepath.Join(filepath.Dir(config), "login", "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"session_arn": arn, "iat": time.Now().Unix(), "exp": time.Now().Add(15 * time.Minute).Unix()})
	raw, _ := json.Marshal(map[string]any{"idToken": "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature", "accessToken": map[string]string{"expiresAt": expiry}})
	if err := os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAWSLoginTimingIgnoresRotatingExpiryAndUnknownStart(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	fields := map[string]string{"login_session": "identity"}
	writeLoginTimingCache(t, config, "identity", "session-one", "2026-11-02T09:00:00Z")
	if at, _ := awsProfileExpiry(config, fields, &cloudAccount{}); at != nil {
		t.Fatal("invented start from rotating credentials")
	}
	start := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	cli := &cloudAccount{LoginAt: &start, LoginSession: awsLoginSession(config, fields)}
	for _, expires := range []string{"2026-11-02T08:15:00Z", "2026-11-02T09:00:00Z"} {
		writeLoginTimingCache(t, config, "identity", "session-one", expires)
		at, kind := awsProfileExpiry(config, fields, cli)
		if at == nil || !at.Equal(start.Add(12*time.Hour)) || kind != "maximum" {
			t.Fatal("refresh changed upper bound")
		}
	}
	writeLoginTimingCache(t, config, "identity", "terminal-new-session", "2026-11-02T09:15:00Z")
	if at, _ := awsProfileExpiry(config, fields, cli); at != nil {
		t.Fatal("reused timing for terminal sign-in")
	}
	if at, _ := awsProfileExpiry(config, map[string]string{"aws_access_key_id": "static"}, cli); at != nil {
		t.Fatal("invented static expiry")
	}
}

func TestAWSLegacySSOTimingOnly(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	key := "https://work.awsapps.com/start"
	sum := sha1.Sum([]byte(key))
	dir := filepath.Join(filepath.Dir(config), "sso", "cache")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), []byte(`{"expiresAt":"2026-11-03T09:00:00UTC","accessToken":"opaque"}`), 0600)
	fields := map[string]string{"sso_start_url": key}
	at, kind := awsProfileExpiry(config, fields, nil)
	if at == nil || at.Format(time.RFC3339) != "2026-11-03T09:00:00Z" || kind != "sso" {
		t.Fatal("legacy SSO session end missing")
	}
	fields["sso_session"] = "modern"
	if at, _ = awsProfileExpiry(config, fields, nil); at != nil {
		t.Fatal("modern SSO session without a cache got a session end")
	}
	// Neither the hourly access token nor the 90-day client registration is
	// when the sign-in ends: only its start and the organization's length are.
	modern := sha1.Sum([]byte("modern"))
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(modern[:])+".json"), []byte(`{"accessToken":"a","expiresAt":"2026-11-03T10:00:00Z","refreshToken":"first","registrationExpiresAt":"2027-02-01T09:00:00Z"}`), 0600)
	if at, _ = awsProfileExpiry(config, fields, &cloudAccount{}); at != nil {
		t.Fatalf("a token or registration date shown as the sign-in's end: %v", at)
	}
	start := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	cli := &cloudAccount{SignInAt: &start, SignInBinding: awsSSOBinding(config, "modern")}
	if at, _ = awsProfileExpiry(config, fields, cli); at != nil {
		t.Fatal("an end without the organization's length")
	}
	cli.SessionLength = int64(90 * 24 * time.Hour / time.Second)
	if at, kind = awsProfileExpiry(config, fields, cli); at == nil || !at.Equal(start.Add(90*24*time.Hour)) || kind != "session" {
		t.Fatalf("sign-in end = %v %q", at, kind)
	}
	// Signing in again elsewhere (a terminal) is another sign-in: its start is unknown.
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(modern[:])+".json"), []byte(`{"accessToken":"a","expiresAt":"2026-11-03T10:00:00Z","refreshToken":"second","registrationExpiresAt":"2027-02-01T09:00:00Z"}`), 0600)
	if at, _ = awsProfileExpiry(config, fields, cli); at != nil {
		t.Fatal("another sign-in's start reused")
	}
}

func TestAWSLoginTimingPersistsThroughRenameAndOnlyAuthFailureExpires(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[profile work]\nlogin_session=identity\n"), 0600)
	writeLoginTimingCache(t, config, "identity", "session", "2026-11-02T09:00:00Z")
	mustHandle(t, m, Request{Action: "list"})
	r := m.awsProfileRecord("work")
	// Unknown preexisting terminal login has no session timing.
	if r.Account.ProfileSignInBy != nil {
		t.Fatal("unknown start has countdown")
	}
	(awsCLI{}).loginCompleted(m, r)
	if r.CLI.LoginAt == nil || r.Account.ProfileSignInBy == nil {
		t.Fatal("Mewla completion not recorded")
	}
	at := *r.Account.ProfileSignInBy
	result := mustHandle(t, m, Request{Action: "cloud_rename", ID: r.Account.ID, Profile: "renamed"})
	if result.Account.ProfileSignInBy == nil || !result.Account.ProfileSignInBy.Equal(at) {
		t.Fatal("rename lost sign-in timing")
	}
	r = m.awsProfileRecord("renamed")
	// Simulate persistence/reload without running any external provider.
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored record
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	m.records[r.Account.ID] = &restored
	r = &restored
	old := time.Now().Add(-13 * time.Hour)
	r.CLI.LoginAt = &old
	if err = m.syncAWSProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if r.Account.ProfileStatus != "valid" {
		t.Fatal("clock alone expired working profile")
	}
	m.cloud.run = func(context.Context, string, []string, []string) (cloudOutput, error) {
		return cloudOutput{ExitCode: 1, Stderr: "Please reauthenticate using aws login"}, nil
	}
	if err = m.syncAWSProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if r.Account.ProfileStatus != "expired" {
		t.Fatal("actual refresh failure did not expire profile")
	}
}
