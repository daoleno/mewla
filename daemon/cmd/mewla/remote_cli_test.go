package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/remote"
)

func TestRemoteLoginSendsNoHostnameAndLoginNameRenames(t *testing.T) {
	machine := func(label string) remote.Machine {
		return remote.Machine{
			MachineID: "mach_1", AccountLogin: "octo", Hostname: label + ".mewla.test", Label: label,
			RouteID: strings.Repeat("ab", 16), Grant: "grant", GrantExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
			Relays: []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}},
		}
	}
	var mu sync.Mutex
	var startNames, renames []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/login/start":
			startNames = append(startNames, body["machine_name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"login_id": "l1", "user_code": "ABCD-EFGH", "verification_uri": "https://example.test/device", "interval_seconds": 1})
		case "POST /v1/login/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete", "machine": machine("quiet-otter-1a2b")})
		case "PUT /v1/machine/name":
			renames = append(renames, body["name"])
			_ = json.NewEncoder(w).Encode(machine("studio-c0ffee"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	stateDir := t.TempDir()
	var out bytes.Buffer
	config, err := remoteLogin(context.Background(), &out, stateDir, remoteLoginOptions{serviceURL: service.URL})
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out.String())
	}
	if len(startNames) != 1 || startNames[0] != "" {
		t.Fatalf("login sent machine names %q; want one empty name so the service picks random words", startNames)
	}
	book, err := addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Add(config.URL(), addressbook.SourceRemote); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := runLoginCommand([]string{"-state-dir", stateDir, "-name", "Studio", "-no-browser"}, &out); err != nil {
		t.Fatalf("rename: %v\n%s", err, out.String())
	}
	if len(renames) != 1 || renames[0] != "Studio" || len(startNames) != 1 {
		t.Fatalf("renames=%q starts=%q; want one rename and no second sign-in", renames, startNames)
	}
	saved, ok, err := remote.LoadRemote(stateDir)
	if err != nil || !ok || saved.Machine.Label != "studio-c0ffee" || saved.ServiceURL != service.URL {
		t.Fatalf("saved config = %#v ok=%v err=%v", saved, ok, err)
	}
	book, err = addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if book.Contains(config.URL()) || !book.Contains(saved.URL()) {
		t.Fatalf("address book still lists %s or misses %s", config.URL(), saved.URL())
	}
	if !strings.Contains(out.String(), saved.URL()) || !strings.Contains(out.String(), "browsers pair again") {
		t.Fatalf("rename output:\n%s", out.String())
	}
}

func TestLogoutDeleteAccountConfirmsThenDeletes(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method+" "+r.URL.Path == "DELETE /v1/account" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer service.Close()
	signIn := func(t *testing.T) string {
		stateDir := t.TempDir()
		config := remote.Config{ServiceURL: service.URL, RemoteAccess: true, Machine: remote.Machine{
			MachineID: "mach_1", AccountLogin: "octo", Provider: "github", Hostname: "quiet-otter-1a2b.mewla.test",
			Label: "quiet-otter-1a2b", RouteID: strings.Repeat("ab", 16), Grant: "grant",
			GrantExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
			Relays:           []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}},
		}}
		if err := remote.SaveConfig(stateDir, config); err != nil {
			t.Fatal(err)
		}
		return stateDir
	}
	signedIn := func(stateDir string) bool {
		_, ok, err := remote.LoadAccount(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	stateDir := signIn(t)
	var out bytes.Buffer
	if err := runLogout([]string{"-state-dir", stateDir, "-delete-account"}, strings.NewReader("someone-else\n"), true, &out); err == nil {
		t.Fatal("a wrong name deleted the account")
	}
	if err := runLogout([]string{"-state-dir", stateDir, "-delete-account"}, strings.NewReader(""), false, &out); err == nil {
		t.Fatal("deleted without a terminal or -yes")
	}
	if len(calls) != 0 || !signedIn(stateDir) {
		t.Fatalf("unconfirmed deletion reached the service (%q) or signed out", calls)
	}

	out.Reset()
	if err := runLogout([]string{"-state-dir", stateDir, "-delete-account"}, strings.NewReader("Octo\n"), true, &out); err != nil {
		t.Fatalf("confirmed deletion: %v\n%s", err, out.String())
	}
	// Friends data goes first, while the computer can still speak for the
	// account; a service without Friends (404) has nothing to delete.
	if len(calls) != 2 || calls[0] != "DELETE /friends/v1/me" || calls[1] != "DELETE /v1/account" || signedIn(stateDir) {
		t.Fatalf("calls=%q signedIn=%v; want Friends data, then the account, and a local sign-out", calls, signedIn(stateDir))
	}
	if !strings.Contains(out.String(), "Deleted the Mewla account octo (github)") {
		t.Fatalf("output:\n%s", out.String())
	}

	stateDir = signIn(t)
	if err := runLogout([]string{"-state-dir", stateDir, "-delete-account", "-yes"}, strings.NewReader(""), false, &out); err != nil {
		t.Fatalf("-yes deletion: %v", err)
	}
	if len(calls) != 4 || signedIn(stateDir) {
		t.Fatalf("calls=%q after -yes", calls)
	}
	if err := runLogout([]string{"-state-dir", t.TempDir(), "-delete-account", "-yes"}, nil, false, &out); err == nil {
		t.Fatal("deleting from a signed-out computer did not explain it must sign in first")
	}
}

func friendsOnlyAccount(t *testing.T, serviceURL string) string {
	t.Helper()
	stateDir := t.TempDir()
	config := remote.Config{ServiceURL: serviceURL, Machine: remote.Machine{
		MachineID: "mach_1", AccountLogin: "octo", Provider: "github", Hostname: "quiet-otter-1a2b.mewla.test",
		Label: "quiet-otter-1a2b", RouteID: strings.Repeat("ab", 16), Grant: "grant",
		GrantExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
		Relays:           []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}},
	}}
	if err := remote.SaveConfig(stateDir, config); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

// A computer signed in only for Friends turns remote access on with mewla
// login, without a second sign-in; mewla logout turns it off again and, while
// Friends is on, keeps the account so Friends keeps working.
func TestLoginReusesTheFriendsAccountAndLogoutKeepsItWhileFriendsIsOn(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/machine":
			_ = json.NewEncoder(w).Encode(remote.Machine{
				MachineID: "mach_1", AccountLogin: "octo", Provider: "github", Hostname: "quiet-otter-1a2b.mewla.test",
				Label: "quiet-otter-1a2b", RouteID: strings.Repeat("ab", 16), Grant: "fresh",
				Relays: []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}},
			})
		case "DELETE /v1/machine":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	stateDir := friendsOnlyAccount(t, service.URL)
	if _, on, _ := remote.LoadRemote(stateDir); on {
		t.Fatal("a Friends-only account counts as remote access")
	}
	if err := friends.NewStore(stateDir).SaveState(friends.State{Handle: "octo", EnabledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runLoginCommand([]string{"-state-dir", stateDir, "-no-browser"}, &out); err != nil {
		t.Fatalf("login: %v\n%s", err, out.String())
	}
	if strings.Join(calls, ",") != "GET /v1/machine" {
		t.Fatalf("service calls = %q; want the existing account reused with no second sign-in", calls)
	}
	config, on, err := remote.LoadRemote(stateDir)
	if err != nil || !on || config.Machine.Grant != "fresh" {
		t.Fatalf("after login: %#v on=%v err=%v", config, on, err)
	}

	out.Reset()
	if err := runLogout([]string{"-state-dir", stateDir}, strings.NewReader(""), false, &out); err != nil {
		t.Fatalf("logout: %v\n%s", err, out.String())
	}
	if strings.Join(calls, ",") != "GET /v1/machine" {
		t.Fatalf("service calls = %q; logout while Friends is on must not sign the computer out at the service", calls)
	}
	if _, on, _ := remote.LoadRemote(stateDir); on {
		t.Fatal("remote access stayed on after logout")
	}
	account, ok, err := remote.LoadAccount(stateDir)
	if err != nil || !ok || account.Machine.AccountLogin != "octo" {
		t.Fatalf("logout lost the Friends account: %#v ok=%v err=%v", account, ok, err)
	}

	// With Friends turned off (its account kept) nothing needs the sign-in,
	// so logout signs out fully.
	if err := (&friends.Local{StateDir: stateDir}).Disable(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runLogout([]string{"-state-dir", stateDir}, strings.NewReader(""), false, &out); err != nil {
		t.Fatalf("second logout: %v\n%s", err, out.String())
	}
	if last := calls[len(calls)-1]; last != "DELETE /v1/machine" {
		t.Fatalf("service calls = %q; want the machine deleted", calls)
	}
	if _, ok, _ := remote.LoadAccount(stateDir); ok {
		t.Fatal("still signed in after a full logout")
	}
}

// Deleting the account works from a computer signed in only for Friends.
func TestDeleteAccountFromAFriendsOnlyComputer(t *testing.T) {
	var calls []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer service.Close()
	stateDir := friendsOnlyAccount(t, service.URL)
	if err := friends.NewStore(stateDir).SaveState(friends.State{Handle: "octo", EnabledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runLogout([]string{"-state-dir", stateDir, "-delete-account", "-yes"}, strings.NewReader(""), false, &out); err != nil {
		t.Fatalf("delete: %v\n%s", err, out.String())
	}
	if strings.Join(calls, ",") != "DELETE /friends/v1/me,DELETE /v1/account" {
		t.Fatalf("calls = %q; want Friends data, then the account", calls)
	}
	if _, ok, _ := remote.LoadAccount(stateDir); ok {
		t.Fatal("still signed in after deleting the account")
	}
	if strings.Contains(out.String(), "no longer reaches this computer") {
		t.Fatalf("output names an address a Friends-only computer never had:\n%s", out.String())
	}
}
