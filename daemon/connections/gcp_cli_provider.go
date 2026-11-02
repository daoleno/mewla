package connections

import (
	"context"
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type gcloudCLI struct{}

func (gcloudCLI) binary() string { return "gcloud" }
func (gcloudCLI) environment() []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		// Keep the vendor's configured directory, but never inherit credential,
		// active-account or endpoint overrides in place of the selected profile.
		if strings.HasPrefix(key, "CLOUDSDK_") && key != "CLOUDSDK_CONFIG" || strings.HasPrefix(key, "GOOGLE_") || key == "PYTHONPATH" || key == "PYTHONHOME" {
			continue
		}
		env = append(env, v)
	}
	return append(env, "CLOUDSDK_CORE_DISABLE_USAGE_REPORTING=true", "CLOUDSDK_COMPONENT_MANAGER_DISABLE_UPDATE_CHECK=true", "CLOUDSDK_CORE_LOG_HTTP=false", "PYTHONUNBUFFERED=1")
}
func (gcloudCLI) validProfile(p string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9-]{0,127}$`).MatchString(p) || strings.HasPrefix(p, "account:") && validGCPAccount(strings.TrimPrefix(p, "account:"))
}
func validGCPAccount(s string) bool {
	return len(s) < 320 && regexp.MustCompile(`^[A-Za-z0-9._%+@-]+$`).MatchString(s) && strings.Contains(s, "@") && !strings.HasPrefix(s, "-")
}
func (gcloudCLI) commandArgs(profile string, args []string) []string {
	out := append([]string{}, args...)
	if profile != "" {
		if strings.HasPrefix(profile, "account:") {
			out = append(out, "--account", strings.TrimPrefix(profile, "account:"))
		} else {
			out = append(out, "--configuration", profile)
		}
	}
	format := false
	for _, a := range args {
		if a == "--format" || strings.HasPrefix(a, "--format=") {
			format = true
		}
	}
	if !format {
		out = append(out, "--format=json")
	}
	return append(out, "--quiet")
}
func (p gcloudCLI) accountArgs(m *Manager, profile string, args []string) []string {
	account := strings.TrimPrefix(profile, "account:")
	configuration := ""
	if r := m.gcpProfileRecord(profile); r != nil {
		account = r.CLI.Account
		configuration = r.CLI.Configuration
		for _, name := range r.CLI.Configurations {
			if name == profile {
				configuration = name
			}
		}
	} else if !strings.HasPrefix(profile, "account:") {
		return p.commandArgs(profile, args)
	}
	out := append([]string{}, args...)
	out = append(out, "--account", account)
	if configuration == "" {
		// An unbound account must not inherit another account's default project.
		project := false
		for _, a := range args {
			if a == "--project" || strings.HasPrefix(a, "--project=") {
				project = true
			}
		}
		if !project {
			out = append(out, "--project=")
		}
	}
	return p.commandArgs(configuration, out)
}

func (gcloudCLI) loginArgs(profile string) []string {
	args := []string{"auth", "login", "--no-launch-browser", "--force"}
	if strings.HasPrefix(profile, "account:") {
		return append(args, strings.TrimPrefix(profile, "account:"), "--no-activate")
	}
	return append(args, "--configuration", profile)
}
func (gcloudCLI) loginURL(output string) string {
	for _, s := range cloudURL.FindAllString(output, -1) {
		u, err := url.Parse(s)
		if err == nil && u.Scheme == "https" && u.Host == "accounts.google.com" && (u.Path == "/o/oauth2/auth" || u.Path == "/o/oauth2/v2/auth") && u.Query().Get("client_id") != "" && u.Query().Get("response_type") == "code" {
			return s
		}
	}
	return ""
}
func (p gcloudCLI) validateCode(address, code string) bool {
	// The vendor child owns PKCE and the exchange. Only one code reaches stdin.
	return p.loginURL(address) != "" && len(code) > 8 && len(code) < 4096 && regexp.MustCompile(`^[A-Za-z0-9/_-]+$`).MatchString(code)
}
func (gcloudCLI) syncProfiles(m *Manager, ctx context.Context, force bool) error {
	return m.syncGCPProfiles(ctx, force)
}
func (gcloudCLI) profileRecord(m *Manager, p string) *record { return m.gcpProfileRecord(p) }
func (gcloudCLI) prepareLogin(m *Manager, ctx context.Context, p string) error {
	if strings.HasPrefix(p, "account:") {
		return nil
	}
	configs, _, err := m.gcpLocalProfiles(ctx)
	if err != nil {
		return err
	}
	for _, c := range configs {
		if c.Name == p {
			return nil
		}
	}
	out, err := m.cloudCommand(ctx, "gcp", "", []string{"config", "configurations", "create", p, "--no-activate"})
	if err != nil || out.ExitCode != 0 {
		return errors.New("Could not create the gcloud configuration")
	}
	return nil
}

// Read classification is independent of the command group. Credential-returning
// operations stay outside reads even when their names begin with get/list.
var gcpSensitiveReadVerbs = map[string]bool{
	"access": true, "decrypt": true, "asymmetric-decrypt": true,
	"print-access-token": true, "print-identity-token": true,
	"generate-access-token": true, "generate-identity-token": true, "generate-login-token": true,
	"get-auth-string": true, "get-key-string": true, "get-password": true,
	"get-login-password": true, "get-client-secret": true, "get-credentials": true,
	"get-token": true, "get-access-token": true, "get-identity-token": true,
	"get-secret": true, "get-secret-value": true,
}

// Stop at a non-read operation before considering its positional arguments:
// `projects delete list` must not become a read because the project is "list".
// These are operation verbs, not an allowlist of supported service groups.
var gcpNonReadVerbs = strings.Fields("abandon abort accept ack acknowledge activate add adopt advance allow apply approve archive assign asymmetric attach authorize batch build cancel change clean clear clone close commit compose configure convert copy create deactivate deauthorize debug delete deny deploy deprecate destroy detach disable dismiss download drain drop edit enable encrypt enroll execute expand export failover force generate grant import ingest init insert install instantiate invalidate issue kill lease link load lock login logout mark merge migrate modify mount move mv mutate patch pause perform post predict print promote provision publish pull purge push query reboot recover recreate redact redeploy reencrypt register reimage reject release relocate remove rm rename renew repair replace replay replicate request reschedule reset resize resolve restart restore resume retry revert revoke rollback rotate run save scan seed seek send set setup show sign simulate snapshot start stop stream submit subscribe suspend switch sync terminate test train transfer trigger undelete undeploy unenroll uninstall unlink unlock unmount unpause unpublish unregister unset update upgrade upload upsert write lookup fetch find read search validate verify wait watch")

func gcpReadVerb(verb string) bool {
	return verb == "list" || verb == "describe" || verb == "get" || strings.HasPrefix(verb, "list-") || strings.HasPrefix(verb, "describe-") || strings.HasPrefix(verb, "get-")
}

func (gcloudCLI) classify(args []string) (bool, error) {
	if !cloudArgs(args) || strings.HasPrefix(args[0], "-") {
		return false, callRefused("Pass gcloud command arguments as literal words")
	}
	for _, a := range args {
		key, _, _ := strings.Cut(a, "=")
		if strings.HasPrefix(key, "--") && (strings.Contains(key, "file") || strings.HasSuffix(key, "-directory") || strings.HasSuffix(key, "-dir")) {
			return false, callRefused("Local file arguments are unavailable through this connection")
		}
		for _, blocked := range []string{"--account", "--configuration", "--impersonate-service-account", "--access-token-file", "--credential-file-override", "--flags-file", "--log-http", "--trace-token", "--verbosity", "--billing-project", "--endpoint", "--universe-domain", "--source", "--destination", "--output", "--command", "--script", "--shell-command"} {
			if strings.HasPrefix(key, "--") && strings.HasPrefix(blocked, key) {
				return false, callRefused("This option cannot override the connected account or load local files")
			}
		}
		if strings.Contains(a, "file://") {
			return false, callRefused("Local file loading is unavailable")
		}
	}
	group := args[0]
	if (group == "alpha" || group == "beta") && len(args) > 1 {
		group = args[1]
	}
	switch group {
	case "config", "components", "init", "info", "interactive", "topic", "meta":
		return false, callRefused("Manage local CLI configuration and sign-in in Extensions")
	}
	// Commands that execute local programs, start tunnels or export credentials
	// are not remote resource operations, even with an execute grant.
	for _, a := range args {
		switch a {
		case "ssh", "scp", "connect", "start-iap-tunnel", "get-credentials", "kubectl", "generate-access-token", "export", "import", "deploy", "submit", "cp", "rsync", "cat", "edit", "open-console":
			return false, callRefused("Local execution, file transfer and credential export are unavailable through this connection")
		}
	}
	command := args
	if command[0] == "alpha" || command[0] == "beta" {
		command = command[1:]
	}
	if len(command) < 2 {
		return false, nil
	}
	// Local auth management and credential export remain unavailable. Listing
	// the signed-in account identities is a read, including in preview groups.
	if command[0] == "auth" && command[1] != "list" {
		return false, callRefused("Manage local CLI configuration and sign-in in Extensions")
	}
	for _, verb := range command[1:] {
		if strings.HasPrefix(verb, "-") {
			// Flags and their values cannot supply the operation name. Put the
			// command path before its options, as in `projects list --project P`.
			return false, nil
		}
		if gcpSensitiveReadVerbs[verb] {
			return false, nil
		}
		if gcpReadVerb(verb) {
			return true, nil
		}
		base, _, _ := strings.Cut(verb, "-")
		for _, action := range gcpNonReadVerbs {
			if base == action {
				return false, nil
			}
		}
	}
	return false, nil
}
