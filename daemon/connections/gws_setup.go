package connections

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// gws signs in with an OAuth client in the person's own Google Cloud project.
// Google offers no API for creating one, so Connect does what it can (a project
// with the APIs on, through the gcloud account already signed in) and leaves a
// single action: create a Desktop app client and paste its ID and secret.
const gwsConsoleAuth = "https://console.cloud.google.com/auth/overview"
const gwsConsoleNew = "https://console.cloud.google.com/projectcreate"
const gwsSetupMessage = "Set up the consent screen (External), choose Publish app, then create a Desktop app client. Paste its client ID and secret."
const gwsSetupMessageManual = "Create a Google Cloud project with the Gmail, Calendar and Drive APIs on, then set up the consent screen (External), choose Publish app and create a Desktop app client. Paste its client ID and secret."

var gwsAPIs = []string{"gmail.googleapis.com", "calendar-json.googleapis.com", "drive.googleapis.com"}

func gwsConsoleLink(project string) string {
	if project == "" {
		return gwsConsoleNew
	}
	return gwsConsoleAuth + "?project=" + project
}

// setupFlow reports whether Connect first needs the person's OAuth client.
func (gwsCLI) setupFlow(m *Manager, ctx context.Context, f *connectFlow) (bool, error) {
	st, err := m.gwsAuthStatus(ctx)
	if err != nil {
		return false, err
	}
	if st.ClientConfigExists && st.ClientConfigError == "" {
		return false, nil
	}
	f.SetupRequired = true
	f.Expires = time.Now().Add(30 * time.Minute)
	f.projectID = st.ProjectID
	f.CodeRequired, f.AuthorizationURL = true, gwsConsoleLink(f.projectID)
	f.Message = gwsSetupMessageManual
	if argv, account, ok := m.gwsProjectCommands(f); ok {
		f.CodeRequired, f.AuthorizationURL = false, ""
		f.Message = "Creating a Google project in " + account + "…"
		go m.runGWSProject(f, argv)
	}
	return true, nil
}

// gwsProjectCommands builds the gcloud calls for the one Google Cloud account
// that is signed in and turned on. With none, or with several, the person
// creates the project instead: Mewla never picks an account to change.
func (m *Manager) gwsProjectCommands(f *connectFlow) ([][]string, string, bool) {
	account := ""
	for _, r := range m.records {
		// Only an account the person turned on in Extensions: Mewla never changes
		// one that merely exists on this computer.
		if r.Account.Integration != "gcp" || r.CLI == nil || r.CLI.Account == "" || r.Account.Status != "connected" || !r.Account.Enabled {
			continue
		}
		if account != "" && account != r.CLI.Account {
			return nil, "", false
		}
		account = r.CLI.Account
	}
	if account == "" || m.cloud == nil || m.cloud.providers["gcp"] == nil {
		return nil, "", false
	}
	if _, err := m.cloud.executable("gcp"); err != nil {
		return nil, "", false
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	id := "mewla-ws-" + hex.EncodeToString(b)
	p := gcloudCLI{}
	profile := "account:" + account
	f.projectID = id
	return [][]string{
		p.accountArgs(m, profile, []string{"projects", "create", id, "--name=Mewla Workspace"}),
		p.accountArgs(m, profile, append(append([]string{"services", "enable"}, gwsAPIs...), "--project="+id)),
	}, account, true
}

// runGWSProject runs outside the manager lock; it reports through the flow.
func (m *Manager) runGWSProject(f *connectFlow, argv [][]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ok := false
	if binary, lease, err := m.cloud.acquireExecutable("gcp"); err == nil {
		ok = true
		for _, args := range argv {
			out, err := m.cloud.run(ctx, binary, args, gcloudCLI{}.environment())
			if err != nil || out.ExitCode != 0 {
				ok = false
				break
			}
		}
		closeCloudLease(lease)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.cacheListLocked()
	if m.connectFlows[f.ID] != f || f.Status != "waiting" || !f.SetupRequired {
		return
	}
	f.CodeRequired = true
	if ok {
		f.AuthorizationURL, f.Message = gwsConsoleLink(f.projectID), gwsSetupMessage
		return
	}
	f.projectID = ""
	f.AuthorizationURL, f.Message = gwsConsoleLink(""), "Couldn't create the project automatically. "+gwsSetupMessageManual
}

var gwsClientID = regexp.MustCompile(`^[0-9]{6,}-[a-z0-9]+\.apps\.googleusercontent\.com$`)
var gwsClientSecret = regexp.MustCompile(`^[A-Za-z0-9_-]{10,200}$`)
var gwsProjectID = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// parseGWSClient accepts the JSON the console downloads, or the client ID and
// secret pasted as two words.
func parseGWSClient(raw string) (id, secret, project string, err error) {
	raw = strings.TrimSpace(raw)
	bad := errors.New("Paste the client ID and secret. The ID ends in .apps.googleusercontent.com.")
	if raw == "" || len(raw) > 4096 {
		return "", "", "", bad
	}
	if strings.HasPrefix(raw, "{") {
		var file map[string]struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			ProjectID    string `json:"project_id"`
		}
		if json.Unmarshal([]byte(raw), &file) != nil {
			return "", "", "", bad
		}
		if _, web := file["web"]; web {
			return "", "", "", errors.New("That is a web client. Create a Desktop app client instead.")
		}
		c, ok := file["installed"]
		if !ok {
			return "", "", "", bad
		}
		id, secret, project = c.ClientID, c.ClientSecret, c.ProjectID
	} else {
		fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' || r == '\n' || r == '\r' || r == ':' })
		if len(fields) != 2 {
			return "", "", "", bad
		}
		id, secret = fields[0], fields[1]
		if !strings.HasSuffix(id, ".apps.googleusercontent.com") {
			id, secret = secret, id
		}
	}
	if !gwsClientID.MatchString(id) || !gwsClientSecret.MatchString(secret) || project != "" && !gwsProjectID.MatchString(project) {
		return "", "", "", bad
	}
	return id, secret, project, nil
}

// finishSetup stores the client where gws itself reads it. The secret goes only
// to gws's client_secret.json, never to Mewla's records.
func (gwsCLI) finishSetup(m *Manager, ctx context.Context, f *connectFlow, pasted string) error {
	id, secret, project, err := parseGWSClient(pasted)
	if err != nil {
		return err
	}
	st, err := m.gwsAuthStatus(ctx)
	if err != nil {
		return err
	}
	if st.ClientConfig == "" || !filepath.IsAbs(st.ClientConfig) {
		return errors.New("Could not find the Google Workspace CLI configuration.")
	}
	if project == "" {
		project = f.projectID
	}
	data, _ := json.MarshalIndent(map[string]any{"installed": map[string]any{
		"client_id": id, "client_secret": secret, "project_id": project,
		"auth_uri": "https://accounts.google.com/o/oauth2/auth", "token_uri": "https://oauth2.googleapis.com/token",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs", "redirect_uris": []string{"http://localhost"},
	}}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(st.ClientConfig), 0700); err != nil {
		return errors.New("Could not save the Google client.")
	}
	if err := atomicfile.Write(st.ClientConfig, data, 0600); err != nil {
		return errors.New("Could not save the Google client.")
	}
	return nil
}
