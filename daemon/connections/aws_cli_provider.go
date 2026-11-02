package connections

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type awsCLI struct{}

func (awsCLI) binary() string { return "aws" }
func (awsCLI) environment() []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(key, "AWS_") || key == "BOTO_CONFIG" {
			continue
		}
		env = append(env, v)
	}
	// ~/.aws is authoritative. Inherited static credentials and endpoint overrides
	// must never silently replace the explicitly selected profile.
	return append(env, "AWS_PAGER=", "AWS_CLI_AUTO_PROMPT=off", "AWS_EC2_METADATA_DISABLED=true", "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=true", "PYTHONUNBUFFERED=1")
}
func (awsCLI) loginArgs(profile string) []string {
	return []string{"login", "--remote", "--profile", profile, "--region", "us-east-1", "--no-cli-auto-prompt"}
}

var cloudURL = regexp.MustCompile(`https://[^\s<>"']+`)

func (awsCLI) loginURL(output string) string {
	for _, s := range cloudURL.FindAllString(output, -1) {
		u, e := url.Parse(s)
		if e == nil && u.Host == "us-east-1.signin.aws.amazon.com" && u.Path == "/v1/authorize" && u.Query().Get("client_id") == "arn:aws:signin:::devtools/cross-device" && u.Query().Get("state") != "" {
			return s
		}
	}
	return ""
}
func (awsCLI) validateCode(address, code string) bool {
	if len(code) > 16384 || strings.ContainsAny(code, "\r\n") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(code)
	}
	if err != nil {
		return false
	}
	values, err := url.ParseQuery(string(raw))
	u, e := url.Parse(address)
	return err == nil && e == nil && len(values["code"]) == 1 && values.Get("code") != "" && len(values["state"]) == 1 && values.Get("state") == u.Query().Get("state")
}

// Vendor verbs cover new services without a hand-maintained read allowlist.
// Credential/secret readers and commands writing a local download stay gated.
var awsSensitiveReads = map[string]string{
	"sts":                 "get-session-token get-federation-token",
	"secretsmanager":      "get-secret-value batch-get-secret-value",
	"kms":                 "get-parameters-for-import",
	"ec2":                 "get-password-data",
	"ecr":                 "get-authorization-token",
	"ecr-public":          "get-authorization-token",
	"codeartifact":        "get-authorization-token",
	"eks":                 "get-token",
	"cognito-identity":    "get-credentials-for-identity get-open-id-token get-open-id-token-for-developer-identity",
	"cognito-idp":         "get-tokens-from-refresh-token describe-user-pool-client",
	"redshift":            "get-cluster-credentials get-cluster-credentials-with-iam",
	"redshift-serverless": "get-credentials",
	"lightsail":           "get-instance-access-details",
	"apigateway":          "get-api-key get-api-keys",
	"appsync":             "list-api-keys",
	"connect":             "get-federation-token",
	"gamelift":            "get-instance-access get-compute-auth-token get-compute-access",
	"grafana":             "list-workspace-service-account-tokens",
	"lambda":              "get-function get-function-configuration",
	"s3api":               "get-object get-object-torrent",
}
var awsCommandWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func awsSSMDecryption(args []string) bool {
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		if !strings.HasPrefix(key, "--") {
			continue
		}
		// AWS accepts unambiguous flag abbreviations. Structured inputs can also
		// request WithDecryption, so they require authorization for parameter reads.
		for _, sensitive := range []string{"--with-decryption", "--cli-input-json", "--cli-input-yaml"} {
			if strings.HasPrefix(sensitive, key) {
				return true
			}
		}
	}
	return false
}

func (awsCLI) classify(args []string) (bool, error) {
	if !cloudArgs(args) {
		return false, callRefused("Pass AWS command arguments as literal words")
	}
	if err := cloudForbidden(args); err != nil {
		return false, err
	}
	if !awsCommandWord.MatchString(args[0]) || !awsCommandWord.MatchString(args[1]) {
		return false, callRefused("Start with the AWS service and command")
	}
	switch args[0] {
	case "configure", "login", "logout", "sso", "sso-oidc", "signin", "history":
		return false, callRefused("Manage sign-in in Extensions")
	}
	for _, a := range args[2:] {
		if a == "--generate-cli-skeleton" || strings.HasPrefix(a, "--generate-cli-skeleton=") {
			return false, callRefused("Skeleton generation is not a cloud operation")
		}
	}
	service, operation := args[0], args[1]
	if strings.Contains(" "+awsSensitiveReads[service]+" ", " "+operation+" ") {
		return false, nil
	}
	if service == "ssm" && strings.Contains(" get-parameter get-parameters get-parameters-by-path get-parameter-history ", " "+operation+" ") && awsSSMDecryption(args[2:]) {
		return false, nil
	}
	if service == "s3" && operation == "ls" || service == "s3api" && (operation == "head-object" || operation == "head-bucket") {
		return true, nil
	}
	return strings.HasPrefix(operation, "get-") || strings.HasPrefix(operation, "list-") || strings.HasPrefix(operation, "describe-"), nil
}
func (awsCLI) install(ctx context.Context, dir string) error {
	return newAWSInstaller().install(ctx, dir)
}
func (awsCLI) installSupport() (string, string) {
	return awsInstallHint(runtime.GOOS, runtime.GOARCH), "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
}
func (awsCLI) validateExecutable(ctx context.Context, path string) error {
	return validateAWSExecutable(ctx, path)
}

func (awsCLI) commandArgs(profile string, args []string) []string {
	return append(awsOutputArgs(args), "--profile", profile, "--no-cli-pager", "--no-cli-auto-prompt")
}
func (awsCLI) syncProfiles(m *Manager, ctx context.Context, force bool) error {
	return m.syncAWSProfiles(ctx, force)
}
func (awsCLI) profileRecord(m *Manager, profile string) *record { return m.awsProfileRecord(profile) }
func (awsCLI) validProfile(profile string) bool                 { return validAWSProfile(profile) }

func (awsCLI) installPaths() []string {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("ProgramFiles"); base != "" {
			return []string{filepath.Join(base, "Amazon", "AWSCLIV2", "aws.exe")}
		}
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", "aws"), "/usr/local/bin/aws"}
}
