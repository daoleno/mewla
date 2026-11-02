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

// Reviewed service/operation pairs, not a prefix heuristic: GetSessionToken,
// GetObject, GetParameter and Lambda code/configuration can reveal secrets.
var awsReads = map[string]string{
	"sts":            "get-caller-identity",
	"ec2":            "describe-instances describe-instance-status describe-volumes describe-vpcs describe-subnets describe-security-groups describe-images describe-regions describe-availability-zones describe-tags",
	"rds":            "describe-db-instances describe-db-clusters describe-db-snapshots describe-events",
	"s3api":          "list-buckets list-objects list-objects-v2 head-bucket get-bucket-location get-bucket-tagging",
	"s3":             "ls",
	"cloudwatch":     "list-metrics get-metric-data get-metric-statistics describe-alarms",
	"logs":           "describe-log-groups describe-log-streams",
	"ecs":            "list-clusters list-services list-tasks describe-clusters describe-services",
	"eks":            "list-clusters describe-cluster",
	"lambda":         "list-functions",
	"iam":            "list-users list-roles list-policies list-account-aliases get-account-summary",
	"organizations":  "list-accounts describe-organization describe-account list-roots",
	"account":        "get-account-information",
	"cloudformation": "list-stacks describe-stacks list-stack-resources",
	"route53":        "list-hosted-zones list-resource-record-sets",
	"dynamodb":       "list-tables describe-table",
	"tagging":        "get-resources get-tag-keys get-tag-values",
	"ce":             "get-cost-and-usage get-cost-forecast",
}

func (awsCLI) classify(args []string) (bool, error) {
	if !cloudArgs(args) {
		return false, callRefused("Pass AWS command arguments as literal words")
	}
	if err := cloudForbidden(args); err != nil {
		return false, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(args[0]) || !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(args[1]) {
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
	return strings.Contains(" "+awsReads[args[0]]+" ", " "+args[1]+" "), nil
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
