package connections

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What cf 1.0.0-beta.16 prints to stderr for `cf auth login --no-browser`.
const cloudflareLoginFixture = `Attempting to login via OAuth Device Authorization Grant...
To authorize cf, please visit:

  https://dash.cloudflare.com/oauth2/device/verify

and enter the code:

  V3vsUaLb

You have 5 minutes to approve this request.
`

func TestParseCloudflareLogin(t *testing.T) {
	link, code, ok := parseCloudflareLogin(cloudflareLoginFixture)
	if !ok || link != "https://dash.cloudflare.com/oauth2/device/verify" || code != "V3vsUaLb" {
		t.Fatalf("parsed %q %q %v", link, code, ok)
	}
	if _, _, ok := parseCloudflareLogin("To authorize cf, please visit:\n  https://dash.cloudflare.com/oauth2/device/verify\n"); ok {
		t.Fatal("a link without a code was offered")
	}
	foreign := strings.Replace(cloudflareLoginFixture, "https://dash.cloudflare.com/", "https://dash.cloudflare.com.example/", 1)
	if _, _, ok := parseCloudflareLogin(foreign); ok {
		t.Fatal("a page off Cloudflare's dashboard was offered")
	}
}

func TestParseCloudflareWhoami(t *testing.T) {
	w, err := parseCloudflareWhoami([]byte(`{"authenticated":false,"error":"Not logged in"}`))
	if err != nil || w.signedIn || w.expired {
		t.Fatalf("signed out: %+v %v", w, err)
	}
	w, _ = parseCloudflareWhoami([]byte(`{"authenticated":true,"authSource":"OAuth token from /home/u/.config/cloudflare","tokenValid":false,"accounts":[]}`))
	if w.signedIn || !w.expired {
		t.Fatalf("rejected sign-in counted as signed in: %+v", w)
	}
	w, _ = parseCloudflareWhoami([]byte(`{"authenticated":true,"authSource":"OAuth token from /home/u/.config/cloudflare","tokenValid":true,"email":"a@example.com","accounts":[{"id":"acc1","name":"Main"},"junk"],"scopes":["user:read"]}`))
	if !w.signedIn || w.token || w.email != "a@example.com" || len(w.accounts) != 1 || w.accounts[0] != (CloudflareAccount{ID: "acc1", Name: "Main"}) {
		t.Fatalf("signed in: %+v", w)
	}
	w, _ = parseCloudflareWhoami([]byte(`{"authenticated":true,"authSource":"CLOUDFLARE_API_TOKEN environment variable","tokenValid":true,"accounts":[]}`))
	if !w.signedIn || !w.token {
		t.Fatalf("token sign-in: %+v", w)
	}
	if _, err := parseCloudflareWhoami([]byte("Logged in as someone")); err == nil {
		t.Fatal("text output was accepted")
	}
}

func TestVersionAtLeast(t *testing.T) {
	for have, want := range map[string]bool{"22.18.0": true, "24.1.0": true, "22.17.9": false, "20.0.0": false, "22.18.0-rc.1": true, "garbage": false} {
		if versionAtLeast(have, cloudflareNodeMinimum) != want {
			t.Fatalf("%s >= %s should be %v", have, cloudflareNodeMinimum, want)
		}
	}
}

// fakeCloudflareTools puts scripted cf, npm and node first on PATH. cf keeps
// its sign-in in a file; a sign-in waits until the test approves it.
func fakeCloudflareTools(t *testing.T, onPath bool) (bin, state string) {
	t.Helper()
	root := t.TempDir()
	bin = filepath.Join(root, "bin")
	global := filepath.Join(root, "global")
	state = filepath.Join(root, "state")
	for _, dir := range []string{bin, filepath.Join(global, "bin"), state} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cf := `#!/bin/sh
state="` + state + `"
case "$1 $2" in
"--version ") echo 1.0.0-beta.16 ;;
"auth whoami")
  if [ -f "$state/signed-in" ]; then echo '{"authenticated":true,"authSource":"OAuth token from x","tokenValid":true,"email":"me@example.com","accounts":[{"id":"acc1","name":"Main"}]}'
  else echo '{"authenticated":false,"error":"Not logged in"}'; fi ;;
"auth login")
  echo "$*" > "$state/login-args"
  printf '%s' "$LOGIN_OUTPUT" >&2
  while [ ! -f "$state/approve" ]; do sleep 0.05; done
  touch "$state/signed-in" ;;
"auth logout") rm -f "$state/signed-in"; echo "Logging out..." ;;
*) exit 2 ;;
esac
`
	npm := `#!/bin/sh
case "$1" in
prefix) echo "` + global + `" ;;
install) echo "$*" > "` + state + `/install-args"; cp "` + bin + `/cf.real" "` + global + `/bin/cf" ;;
esac
`
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bin, "cf.real"), cf)
	if onPath {
		write(filepath.Join(bin, "cf"), cf)
	}
	write(filepath.Join(bin, "npm"), npm)
	write(filepath.Join(bin, "node"), "#!/bin/sh\necho v24.18.0\n")
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("LOGIN_OUTPUT", cloudflareLoginFixture)
	return bin, state
}

func cloudflareRequest(t *testing.T, m *Manager, ctx context.Context, action string) CloudflareStatus {
	t.Helper()
	response, err := m.Handle(ctx, Request{Action: action, Refresh: true})
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	if response.Cloudflare == nil {
		t.Fatalf("%s returned no Cloudflare status", action)
	}
	return *response.Cloudflare
}

func waitCloudflare(t *testing.T, m *Manager, done func(CloudflareStatus) bool) CloudflareStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status := cloudflareRequest(t, m, context.Background(), "cloudflare_status")
		if done(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("Cloudflare status never settled: %+v", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A device sign-in shows Cloudflare's link and code, keeps waiting after the
// request that started it ends, and signs in once approved. Sign-out ends it.
func TestCloudflareSignInOutlivesItsRequest(t *testing.T) {
	_, state := fakeCloudflareTools(t, true)
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	status := cloudflareRequest(t, m, context.Background(), "cloudflare_status")
	if !status.Installed || !status.OnPath || status.Version != CloudflareVersion || status.SignedIn || status.Login != nil {
		t.Fatalf("before sign-in: %+v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	status = cloudflareRequest(t, m, ctx, "cloudflare_login")
	cancel()
	if status.Login == nil || status.Login.URL != "https://dash.cloudflare.com/oauth2/device/verify" || status.Login.Code != "V3vsUaLb" || status.SignedIn {
		t.Fatalf("sign-in: %+v", status)
	}
	if args, _ := os.ReadFile(filepath.Join(state, "login-args")); strings.TrimSpace(string(args)) != "auth login --no-browser --force" {
		t.Fatalf("cf ran with %q", args)
	}
	if err := os.WriteFile(filepath.Join(state, "approve"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	status = waitCloudflare(t, m, func(s CloudflareStatus) bool { return s.Login == nil })
	if !status.SignedIn || status.Email != "me@example.com" || len(status.Accounts) != 1 || status.Message != "Signed in to Cloudflare." {
		t.Fatalf("after approval: %+v", status)
	}
	status = cloudflareRequest(t, m, context.Background(), "cloudflare_logout")
	if status.SignedIn || status.Message != "Signed out of Cloudflare." {
		t.Fatalf("after sign-out: %+v", status)
	}
}

func TestCloudflareCancelStopsTheSignIn(t *testing.T) {
	_, state := fakeCloudflareTools(t, true)
	m, _ := New(t.TempDir())
	if status := cloudflareRequest(t, m, context.Background(), "cloudflare_login"); status.Login == nil {
		t.Fatalf("no code: %+v", status)
	}
	status := cloudflareRequest(t, m, context.Background(), "cloudflare_cancel")
	if status.Login != nil || status.SignedIn || status.Message != "Cloudflare sign-in cancelled." {
		t.Fatalf("after cancel: %+v", status)
	}
	// Approval after cancel reaches no cf.
	_ = os.WriteFile(filepath.Join(state, "approve"), nil, 0o644)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(state, "signed-in")); err == nil {
		t.Fatal("a cancelled sign-in still finished")
	}
}

// npm's global bin may be missing from the daemon's PATH: cf is found there
// and reported as off PATH, so agents' shells may not find it by name.
func TestCloudflareInstallsThePinnedRelease(t *testing.T) {
	_, state := fakeCloudflareTools(t, false)
	m, _ := New(t.TempDir())
	status := cloudflareRequest(t, m, context.Background(), "cloudflare_status")
	if status.Installed || status.Problem != "" || status.Pinned != CloudflareVersion {
		t.Fatalf("before install: %+v", status)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "cloudflare_login"}); err == nil {
		t.Fatal("sign-in started without cf")
	}
	cloudflareRequest(t, m, context.Background(), "cloudflare_install")
	status = waitCloudflare(t, m, func(s CloudflareStatus) bool { return !s.Installing })
	if !status.Installed || status.OnPath || status.Version != CloudflareVersion || status.Message != "Installed cf "+CloudflareVersion+"." {
		t.Fatalf("after install: %+v", status)
	}
	if args, _ := os.ReadFile(filepath.Join(state, "install-args")); strings.TrimSpace(string(args)) != "install --global --no-fund --no-audit cf@"+CloudflareVersion {
		t.Fatalf("npm ran with %q", args)
	}
}

func TestCloudflareIgnoresAnotherCfAndOldNode(t *testing.T) {
	bin, _ := fakeCloudflareTools(t, false)
	if err := os.WriteFile(filepath.Join(bin, "cf"), []byte("#!/bin/sh\necho 'cf version 8.7.0+0b8e4e2.2023-06-28'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho v20.11.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _ := New(t.TempDir())
	status := cloudflareRequest(t, m, context.Background(), "cloudflare_status")
	if status.Installed || !strings.Contains(status.Problem, "Node.js 22.18") {
		t.Fatalf("Cloud Foundry's cf or old Node: %+v", status)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "cloudflare_install"}); err == nil {
		t.Fatal("install ran on Node 20")
	}
}
