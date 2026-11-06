package connections

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGWSCLIClassification(t *testing.T) {
	p := gwsCLI{}
	reads := [][]string{
		{"gmail", "users", "messages", "list", "--params", `{"userId":"me","q":"is:unread"}`},
		{"gmail", "users", "messages", "get", "--params", `{"userId":"me","id":"1"}`},
		{"gmail", "users", "getProfile", "--params", `{"userId":"me"}`},
		{"gmail", "users", "settings", "getVacation"},
		{"gmail", "+triage"}, {"gmail", "+read", "--id", "1"}, {"calendar", "+agenda"},
		{"calendar", "events", "list", "--params", `{"calendarId":"primary"}`},
		{"calendar", "freebusy", "query", "--json", `{"items":[]}`},
		{"drive", "files", "list"}, {"drive", "files", "get", "--params", `{"fileId":"1","alt":"media"}`},
		{"schema", "gmail.users.messages.list"}, {"--help"}, {"gmail"}, {"drive", "files", "list", "--page-all", "--format", "table"},
	}
	for _, args := range reads {
		if read, err := p.classify(args); err != nil || !read {
			t.Fatalf("read=%v err=%v: %v", read, err, args)
		}
	}
	changes := [][]string{
		{"gmail", "+send", "--to", "a@example.com", "--subject", "x", "--body", "y"},
		{"gmail", "+reply", "--message-id", "1"}, {"gmail", "+forward"}, {"calendar", "+insert"},
		{"gmail", "users", "messages", "send", "--json", "{}"},
		{"gmail", "users", "messages", "trash", "--params", `{"id":"1"}`},
		{"gmail", "users", "drafts", "create"}, {"gmail", "users", "messages", "batchDelete"},
		{"calendar", "events", "insert"}, {"calendar", "events", "delete"}, {"calendar", "events", "quickAdd"},
		{"drive", "files", "create"}, {"drive", "files", "delete"}, {"drive", "permissions", "create"},
		{"drive", "files", "list", "extra", "update"}, // the last word of the path decides
		{"gmail", "users", "messages", "list", "extra"},
		{"drive", "files", "emptyTrash"}, {"drive", "files", "copy"},
	}
	for _, args := range changes {
		read, err := p.classify(args)
		if err != nil || read {
			t.Fatalf("read=%v err=%v; expected a change: %v", read, err, args)
		}
	}
	blocked := [][]string{
		{}, {"auth", "login"}, {"auth", "export", "--unmasked"}, {"mcp"}, {"gmail", "+watch"}, {"gmail", "users", "watch"}, {"drive", "changes", "watch"},
		{"drive", "+upload", "/etc/passwd"}, {"drive", "files", "create", "--upload", "/etc/passwd"}, {"drive", "files", "get", "--output", "/tmp/x"},
		{"drive", "files", "get", "-o", "/tmp/x"}, {"gmail", "+send", "--attach", "/etc/passwd"}, {"gmail", "+watch", "--output-dir", "/tmp"},
		{"gmail", "users", "messages", "list", "--json", "@/etc/passwd"}, {"gmail", "users", "messages", "list", "--params", "file:///etc/passwd"},
		{"gmail", "users", "messages", "list", "--token", "x"}, {"gmail", "users", "messages", "list", "--credentials-file=/x"}, {"gmail", "users", "messages", "list", "--account", "other@example.com"},
		{"gmail", "users", "messages", "list", "--sanitize", "projects/p/locations/l/templates/t"},
		{"sheets", "spreadsheets", "get"}, {"chat", "spaces", "list"}, {"gmail\nusers", "messages"},
		{"calendar", "events", "list", "--config-dir", "/x"},
	}
	for _, args := range blocked {
		if read, err := p.classify(args); err == nil || read {
			t.Fatalf("read=%v err=%v; expected refusal: %v", read, err, args)
		}
	}
}

func TestGWSEnvironmentKeepsOnlyTheSignedInAccount(t *testing.T) {
	t.Setenv("GOOGLE_WORKSPACE_CLI_TOKEN", "evil-token")
	t.Setenv("GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE", "/evil.json")
	t.Setenv("GOOGLE_WORKSPACE_CLI_CLIENT_ID", "evil-client")
	t.Setenv("GOOGLE_WORKSPACE_CLI_CLIENT_SECRET", "evil-secret")
	t.Setenv("GOOGLE_WORKSPACE_PROJECT_ID", "evil-project")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/evil-adc.json")
	t.Setenv("GOOGLE_WORKSPACE_CLI_CONFIG_DIR", "/canonical/config")
	env := strings.Join((gwsCLI{}).environment(), "\n")
	if strings.Contains(env, "evil") || !strings.Contains(env, "GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/canonical/config") {
		t.Fatalf("credential routing overridden:\n%s", env)
	}
	// The CLI runs in a private empty directory, so a .env file cannot supply them.
	if dir := cloudWorkDir("/x/bin/gws"); dir == "" || cloudWorkDir("/x/bin/aws") != "" {
		t.Fatal("working directory", dir)
	}
	if entries, _ := os.ReadDir(cloudWorkDir("/x/bin/gws")); len(entries) != 0 {
		t.Fatal("working directory is not empty")
	}
}

const gwsTestLogin = "https://accounts.google.com/o/oauth2/auth?scope=https://www.googleapis.com/auth/drive&access_type=offline&redirect_uri=http://localhost:%s&response_type=code&client_id=123456789012-abc.apps.googleusercontent.com&prompt=select_account+consent"

func TestGWSLoopbackPasteBack(t *testing.T) {
	p := gwsCLI{}
	address := fmt.Sprintf(gwsTestLogin, "42503")
	if p.loginURL("Open this URL:\n\n  "+address+"\n") != address {
		t.Fatal("URL not parsed")
	}
	for _, bad := range []string{
		"https://accounts.google.com.evil.test/o/oauth2/auth?redirect_uri=http://localhost:42503&response_type=code&client_id=x",
		"https://accounts.google.com/o/oauth2/auth?redirect_uri=http://evil.test:42503&response_type=code&client_id=x",
		"https://accounts.google.com/o/oauth2/auth?redirect_uri=http://localhost:42503/steal&response_type=code&client_id=x",
		"https://accounts.google.com/o/oauth2/auth?redirect_uri=https://localhost:42503&response_type=code&client_id=x",
	} {
		if p.loginURL(bad) != "" {
			t.Fatalf("foreign sign-in URL accepted: %s", bad)
		}
	}
	good := "http://localhost:42503/?code=4/0AUn6Hd-XClk_abc&scope=email%20https://www.googleapis.com/auth/drive"
	if !p.validateCode(address, good) || !p.validateCode(address, "http://127.0.0.1:42503/?code=4/0AUn6Hd-XClk_abc") {
		t.Fatal("own address refused")
	}
	for _, bad := range []string{
		"http://localhost:9999/?code=4/0AUn6Hd-XClk_abc",              // not the port gws listens on
		"http://evil.test:42503/?code=4/0AUn6Hd-XClk_abc",             // other host
		"http://localhost.evil.test:42503/?code=4/0AUn6Hd-XClk_abc",   // look-alike host
		"http://user@localhost:42503/?code=4/0AUn6Hd-XClk_abc",        // userinfo
		"https://localhost:42503/?code=4/0AUn6Hd-XClk_abc",            // wrong scheme
		"http://localhost:42503/?error=access_denied",                 // declined
		"http://localhost:42503/?code=",                               // empty
		"http://localhost:42503/?code=4/short",                        // not a code
		"http://localhost:42503/?code=4/0AUn6Hd-XClk_abc\nGET /admin", // injected line
		"http://localhost:42503/?code=4/0AUn6Hd-XClk_abc extra",       // injected text
		"4/0AUn6Hd-XClk_abc",                                          // a bare code is not an address
		"",
	} {
		if p.validateCode(address, bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
	if p.validateCode("https://accounts.google.com/o/oauth2/auth?client_id=vendor&response_type=code", good) {
		t.Fatal("accepted without a loopback redirect")
	}
}

func TestGWSDeliverCodeReachesOnlyTheCLIListener(t *testing.T) {
	var got []url.Values
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query())
		paths = append(paths, r.URL.Path+" "+r.Host)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	address := fmt.Sprintf(gwsTestLogin, port)
	pasted := "http://localhost:" + port + "/anything/else?code=4/0AUn6Hd-XClk_abc&scope=email&state=ignored&extra=1"
	if err := (gwsCLI{}).deliverCode(address, pasted); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Get("code") != "4/0AUn6Hd-XClk_abc" || got[0].Get("scope") != "email" || len(got[0]) != 2 || paths[0] != "/ 127.0.0.1:"+port {
		t.Fatalf("only code and scope go to the listener's root: %v %v", got, paths)
	}
	// Other ports, other hosts and a stopped listener never reach anything.
	if err := (gwsCLI{}).deliverCode(address, "http://localhost:1/?code=4/0AUn6Hd-XClk_abc"); err == nil {
		t.Fatal("wrong port delivered")
	}
	if err := (gwsCLI{}).deliverCode(address, "http://evil.test:"+port+"/?code=4/0AUn6Hd-XClk_abc"); err == nil || len(got) != 1 {
		t.Fatal("wrong host delivered")
	}
	srv.Close()
	if err := (gwsCLI{}).deliverCode(address, pasted); err == nil || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("ended sign-in not reported: %v", err)
	}
}

func TestParseGWSClient(t *testing.T) {
	const id, secret = "123456789012-abcdef0123456789.apps.googleusercontent.com", "GOCSPX-abcDEF_123-xyz"
	for name, raw := range map[string]string{
		"two words": id + " " + secret, "reversed": secret + "\n" + id, "comma": id + "," + secret,
		"console json": `{"installed":{"client_id":"` + id + `","project_id":"mewla-ws-0a1b2c3d","client_secret":"` + secret + `","redirect_uris":["http://localhost"]}}`,
	} {
		gotID, gotSecret, _, err := parseGWSClient(raw)
		if err != nil || gotID != id || gotSecret != secret {
			t.Fatalf("%s: %v %q %q", name, err, gotID, gotSecret)
		}
	}
	if _, _, project, _ := parseGWSClient(`{"installed":{"client_id":"` + id + `","project_id":"mewla-ws-0a1b2c3d","client_secret":"` + secret + `"}}`); project != "mewla-ws-0a1b2c3d" {
		t.Fatal("project not read")
	}
	for name, raw := range map[string]string{
		"empty": "", "one word": id, "web client": `{"web":{"client_id":"` + id + `","client_secret":"` + secret + `"}}`,
		"bad id": "not-an-id " + secret, "bad secret": id + " short", "three": id + " " + secret + " more", "bad json": `{"installed":`,
		"bad project": `{"installed":{"client_id":"` + id + `","project_id":"Bad Project","client_secret":"` + secret + `"}}`,
	} {
		if _, _, _, err := parseGWSClient(raw); err == nil {
			t.Fatalf("%s accepted", name)
		} else if strings.Contains(err.Error(), secret) {
			t.Fatal("error repeats the secret")
		}
	}
}

func gwsTestArchive(t *testing.T, script string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range []struct{ name, body string }{{"README.md", "docs"}, {"../escape", "x"}, {"gws", script}} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0755, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(f.body))
	}
	tw.Close()
	zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestGWSInstallVerifiesDigestAndVersion(t *testing.T) {
	archive, digest := gwsTestArchive(t, "#!/bin/sh\necho 'gws "+gwsVersion+"'\n")
	served := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		if !strings.HasSuffix(r.URL.Path, "google-workspace-cli-test.tar.gz") {
			http.NotFound(w, r)
			return
		}
		w.Write(archive)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := installGWSArchive(context.Background(), dir, gwsArchive{"test", "0000"}, srv.URL+"/", srv.Client(), runCloudProcess); err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("bad digest accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "gws")); err == nil {
		t.Fatal("unverified executable published")
	}
	if err := installGWSArchive(context.Background(), dir, gwsArchive{"test", digest}, srv.URL+"/", srv.Client(), runCloudProcess); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "bin", "gws")
	if info, err := os.Stat(launcher); err != nil || info.Mode()&0111 == 0 {
		t.Fatal("launcher missing", err)
	}
	if err := (gwsCLI{}).validateExecutable(context.Background(), launcher); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); err == nil {
		t.Fatal("archive escaped")
	}
	// A second install finds the pinned version and downloads nothing.
	before := served
	if err := installGWSArchive(context.Background(), dir, gwsArchive{"test", digest}, srv.URL+"/", srv.Client(), runCloudProcess); err != nil || served != before {
		t.Fatalf("reinstalled: %v", err)
	}
	// A binary that reports another version is not published.
	wrong, wrongDigest := gwsTestArchive(t, "#!/bin/sh\necho 'gws 0.1.0'\n")
	archive = wrong
	other := t.TempDir()
	if err := installGWSArchive(context.Background(), other, gwsArchive{"test", wrongDigest}, srv.URL+"/", srv.Client(), runCloudProcess); err == nil {
		t.Fatal("wrong version installed")
	}
	if _, err := os.Stat(filepath.Join(other, "bin", "gws")); err == nil {
		t.Fatal("wrong version published")
	}
	if msg, link := gwsInstallSupport("plan9/arm"); msg == "" || link == "" {
		t.Fatal("unsupported platform has no manual path")
	}
	for platform := range gwsArchives {
		if msg, _ := gwsInstallSupport(platform); msg != "" {
			t.Fatal(platform)
		}
	}
}

// TestGWSHelperProcess is a stand-in for the vendor CLI, driven by the flow test.
func TestGWSHelperProcess(t *testing.T) {
	if os.Getenv("GWS_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	dir := os.Getenv("GOOGLE_WORKSPACE_CLI_CONFIG_DIR")
	creds := filepath.Join(dir, "credentials.enc")
	client := filepath.Join(dir, "client_secret.json")
	switch strings.Join(args, " ") {
	case "--version":
		fmt.Println("gws " + gwsVersion)
	case "auth status":
		out := map[string]any{"auth_method": "none", "client_config": client, "client_config_exists": false}
		if _, err := os.Stat(client); err == nil {
			out["client_config_exists"] = true
			out["project_id"] = "mewla-ws-0a1b2c3d"
		}
		if user, err := os.ReadFile(creds); err == nil {
			out["auth_method"], out["user"], out["token_valid"] = "oauth2", strings.TrimSpace(string(user)), true
		}
		json.NewEncoder(os.Stdout).Encode(out)
	case "auth logout":
		os.Remove(creds)
		fmt.Println(`{"status":"success"}`)
	case "auth login -s " + gwsServices:
		if _, err := os.Stat(client); err != nil {
			fmt.Fprintln(os.Stderr, "No OAuth client configured.")
			os.Exit(2)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(3)
		}
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		fmt.Fprintf(os.Stderr, "Open this URL in your browser to authenticate:\n\n  "+gwsTestLogin+"\n\n", port)
		conn, err := ln.Accept()
		if err != nil {
			os.Exit(3)
		}
		line, _ := bufio.NewReader(conn).ReadString('\n')
		conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\nok"))
		conn.Close()
		if !strings.Contains(line, "code=4%2F0AUn6Hd-good_code") {
			fmt.Fprintln(os.Stderr, "invalid_grant")
			os.Exit(1)
		}
		os.WriteFile(creds, []byte("person@example.com\n"), 0600)
		fmt.Println(`{"status":"success","account":"person@example.com"}`)
	default:
		json.NewEncoder(os.Stdout).Encode(map[string]any{"stub": true, "args": args})
	}
	os.Exit(0)
}

type testGWSCLI struct {
	gwsCLI
	path string
}

func (p testGWSCLI) binary() string                                   { return p.path }
func (p testGWSCLI) validateExecutable(context.Context, string) error { return nil }

func testGWSManager(t *testing.T) (*Manager, string) {
	t.Helper()
	m, _, _, _ := testCloudManager(t)
	config := t.TempDir()
	t.Setenv("GOOGLE_WORKSPACE_CLI_CONFIG_DIR", config)
	t.Setenv("GWS_HELPER", "1")
	t.Setenv("GWS_TEST_BIN", os.Args[0])
	script := filepath.Join(t.TempDir(), "gws")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec \"$GWS_TEST_BIN\" -test.run=TestGWSHelperProcess -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m.cloud.providers["gws"] = testGWSCLI{path: script}
	m.cloud.run = runCloudProcess
	return m, config
}

func TestGWSConnectFlowEndToEnd(t *testing.T) {
	m, config := testGWSManager(t)
	const id, secret = "123456789012-abcdef0123456789.apps.googleusercontent.com", "GOCSPX-abcDEF_123-xyz"

	// No client yet: Connect asks for the one thing Google gives no API for.
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gws"}}).Flow
	if !f.SetupRequired || !f.CodeRequired || f.AddressRequired || !strings.HasPrefix(f.AuthorizationURL, "https://console.cloud.google.com/") || f.Message == "" {
		t.Fatalf("setup step: %+v", f)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: "nonsense"}); err == nil {
		t.Fatal("bad client accepted")
	}
	if _, err := os.Stat(filepath.Join(config, "client_secret.json")); err == nil {
		t.Fatal("client written for a bad paste")
	}
	f = mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: id + " " + secret}).Flow
	if f.SetupRequired || f.CodeRequired || !f.AddressRequired || f.AuthorizationURL == "" {
		t.Fatalf("sign-in step: %+v", f)
	}
	saved, err := os.ReadFile(filepath.Join(config, "client_secret.json"))
	if err != nil || !strings.Contains(string(saved), id) {
		t.Fatalf("client not saved for gws: %v", err)
	}
	if info, _ := os.Stat(filepath.Join(config, "client_secret.json")); info.Mode().Perm() != 0600 {
		t.Fatal("client file is not private")
	}
	for _, r := range m.records {
		if raw, _ := json.Marshal(r); strings.Contains(string(raw), secret) {
			t.Fatal("secret in Mewla records")
		}
	}
	// The browser could not reach this computer: paste the address it ended on.
	u, _ := url.Parse(f.AuthorizationURL)
	port := strings.TrimPrefix(u.Query().Get("redirect_uri"), "http://localhost:")
	login := m.connectFlows[f.ID].cli
	for _, bad := range []string{"http://localhost:1/?code=4/0AUn6Hd-good_code", "http://evil.test:" + port + "/?code=4/0AUn6Hd-good_code", "4/0AUn6Hd-good_code"} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: bad}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: "http://localhost:" + port + "/?code=4/0AUn6Hd-good_code&scope=email"})
	select {
	case <-login.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sign-in did not finish")
	}
	result := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
	if result.Account == nil || result.Account.Integration != "gws" || result.Account.Identity != "person@example.com" || !result.Account.Enabled || len(m.records) != 1 {
		t.Fatalf("not connected: %+v", result.Account)
	}

	// Agent calls: reads run, changes need enabled writes and task authorization.
	r := m.gwsProfileRecord("")
	q := Request{Action: "cloud_run", Integration: "gws", CLIArgs: []string{"gmail", "users", "messages", "list", "--params", `{"userId":"me"}`}}
	out := mustHandle(t, m, q)
	if !strings.Contains(out.CLIOutput.Stdout, `"stub":true`) {
		t.Fatalf("read did not run: %+v", out.CLIOutput)
	}
	send := Request{Action: "cloud_run", Integration: "gws", CLIArgs: []string{"gmail", "+send", "--to", "a@example.com", "--subject", "s", "--body", "b"}}
	if _, err := m.Handle(context.Background(), send); err == nil {
		t.Fatal("send ran with writes disabled")
	}
	m.setGroup(r, "write", true)
	if _, err := m.Handle(context.Background(), send); err == nil {
		t.Fatal("send ran without task authorization")
	}
	send.Authorize = true
	if out := mustHandle(t, m, send); !strings.Contains(out.CLIOutput.Stdout, `"+send"`) {
		t.Fatal("authorized change did not reach the CLI")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "gws", Authorize: true, CLIArgs: []string{"drive", "files", "create", "--upload", "/etc/passwd"}}); err == nil {
		t.Fatal("local file upload ran even with authorization")
	}

	// Signing out of gws outside Mewla removes the card; deleting here signs out.
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_delete", ID: r.Account.ID, ConfirmProfile: "wrong@example.com"}); err == nil {
		t.Fatal("deleted without confirmation")
	}
	mustHandle(t, m, Request{Action: "cloud_delete", ID: r.Account.ID, ConfirmProfile: "person@example.com"})
	if _, err := os.Stat(filepath.Join(config, "credentials.enc")); err == nil || len(m.records) != 0 {
		t.Fatal("sign-out incomplete")
	}
	// The client stays: connecting again goes straight to sign-in.
	f = mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gws"}}).Flow
	if f.SetupRequired || !f.AddressRequired {
		t.Fatalf("reconnect asked for the client again: %+v", f)
	}
	mustHandle(t, m, Request{Action: "connect_cancel", FlowID: f.ID})
	select {
	case <-m.connectFlows[f.ID].cli.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel leaked the child")
	}
}

func TestGWSSetupCreatesTheProjectInTheOnlyGoogleCloudAccount(t *testing.T) {
	m, _ := testGWSManager(t)
	var calls []string
	m.cloud.providers["gcp"] = testGcloudCLI{path: "/bin/true"}
	m.records["gcp-one"] = &record{CLI: &cloudAccount{Profile: "account:me@example.com", Account: "me@example.com"}, Account: Account{ID: "gcp-one", Integration: "gcp", Status: "connected"}}
	run := m.cloud.run
	m.cloud.run = func(ctx context.Context, binary string, args, env []string) (cloudOutput, error) {
		if filepath.Base(binary) == "true" {
			calls = append(calls, strings.Join(args, " "))
			return cloudOutput{Stdout: "{}"}, nil
		}
		return run(ctx, binary, args, env)
	}
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gws"}}).Flow
	if !f.SetupRequired || f.CodeRequired || !strings.Contains(f.Message, "me@example.com") {
		t.Fatalf("project not announced: %+v", f)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.CodeRequired == false && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		f = mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID}).Flow
	}
	if !f.CodeRequired || !strings.Contains(f.AuthorizationURL, "?project=mewla-ws-") {
		t.Fatalf("console link lacks the project: %+v", f)
	}
	joined := strings.Join(calls, "\n")
	if len(calls) != 2 || !strings.Contains(calls[0], "projects create mewla-ws-") || !strings.Contains(calls[0], "--account me@example.com") ||
		!strings.Contains(calls[1], "services enable gmail.googleapis.com calendar-json.googleapis.com drive.googleapis.com") || !strings.Contains(joined, "--project=mewla-ws-") {
		t.Fatalf("gcloud calls:\n%s", joined)
	}

	// Two Google Cloud accounts: Mewla never picks one to change.
	m2, _ := testGWSManager(t)
	m2.records["a"] = &record{CLI: &cloudAccount{Profile: "account:a@example.com", Account: "a@example.com"}, Account: Account{ID: "a", Integration: "gcp", Status: "connected"}}
	m2.records["b"] = &record{CLI: &cloudAccount{Profile: "account:b@example.com", Account: "b@example.com"}, Account: Account{ID: "b", Integration: "gcp", Status: "connected"}}
	m2.cloud.providers["gcp"] = testGcloudCLI{path: "/bin/true"}
	f = mustHandle(t, m2, Request{Action: "connect_start", Input: &Input{Integration: "gws"}}).Flow
	if !f.CodeRequired || f.AuthorizationURL != gwsConsoleNew || !strings.Contains(f.Message, "Create a Google Cloud project") {
		t.Fatalf("ambiguous accounts: %+v", f)
	}
}

func TestGWSProfileFollowsTheCLIsOwnState(t *testing.T) {
	m, config := testGWSManager(t)
	if err := m.syncGWSProfiles(context.Background(), true); err != nil || len(m.records) != 0 {
		t.Fatal("card without a sign-in", err)
	}
	os.WriteFile(filepath.Join(config, "credentials.enc"), []byte("person@example.com\n"), 0600)
	if err := m.syncGWSProfiles(context.Background(), true); err != nil || len(m.records) != 1 {
		t.Fatal("sign-in not discovered", err)
	}
	r := m.gwsProfileRecord("")
	if r.Account.Status != "connected" || r.Account.Identity != "person@example.com" || r.CLI.Profile != gwsProfile || r.Account.Enabled {
		t.Fatalf("%+v", r.Account)
	}
	os.Remove(filepath.Join(config, "credentials.enc"))
	if err := m.syncGWSProfiles(context.Background(), true); err != nil || len(m.records) != 0 {
		t.Fatal("sign-out elsewhere left a card")
	}
}
