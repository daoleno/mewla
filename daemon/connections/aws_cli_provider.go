package connections

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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
	if runtime.GOOS == "darwin" {
		return installAWSMac(ctx, dir)
	}
	if runtime.GOOS != "linux" {
		return errors.New("Automatic AWS CLI installation currently requires Linux; install AWS CLI v2 on this computer")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://awscli.amazonaws.com/v2/install.sh", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("AWS installer download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("AWS installer download failed")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(raw) >= 1<<20 {
		return errors.New("AWS installer download was incomplete")
	}
	temp, err := os.CreateTemp(dir, "installer-*.sh")
	if err != nil {
		return err
	}
	path := temp.Name()
	defer os.Remove(path)
	if _, err = temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	temp.Close()
	// Fixed official installer, never caller-supplied shell text. It verifies the
	// vendor package and supports a per-user install without sudo.
	cmd := exec.CommandContext(ctx, "bash", path)
	ownCloudProcess(cmd)
	cmd.Env = append(awsCLI{}.environment(), "XDG_DATA_HOME="+dir, "XDG_BIN_HOME="+filepath.Join(dir, "bin"))
	cmd.WaitDelay = 1e9
	ownCloudProcess(cmd)
	if err = cmd.Run(); err != nil {
		return errors.New("AWS CLI installation failed")
	}
	return nil
}

// Apple's official per-user installer supports a custom location without sudo.
func installAWSMac(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(dir, "install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://awscli.amazonaws.com/AWSCLIV2.pkg", nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("AWS installer download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("AWS installer download failed")
	}
	pkg := filepath.Join(scratch, "AWSCLIV2.pkg")
	file, err := os.OpenFile(pkg, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	size, err := io.Copy(file, io.LimitReader(response.Body, 256<<20))
	file.Close()
	if err != nil || size >= 256<<20 {
		return errors.New("AWS installer download was incomplete")
	}
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(dir))
	choices := filepath.Join(scratch, "choices.xml")
	raw := `<?xml version="1.0"?><plist version="1.0"><array><dict><key>choiceAttribute</key><string>customLocation</string><key>attributeSetting</key><string>` + escaped.String() + `</string><key>choiceIdentifier</key><string>default</string></dict></array></plist>`
	if err = os.WriteFile(choices, []byte(raw), 0600); err != nil {
		return err
	}
	// Verify the signed package with the OS before executing its installer.
	verify := exec.CommandContext(ctx, "/usr/sbin/pkgutil", "--check-signature", pkg)
	ownCloudProcess(verify)
	signature, err := verify.Output()
	if err != nil || !bytes.Contains(signature, []byte("Amazon")) {
		return errors.New("AWS installer signature could not be verified")
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/installer", "-pkg", pkg, "-target", "CurrentUserHomeDirectory", "-applyChoiceChangesXML", choices)
	ownCloudProcess(cmd)
	if err = cmd.Run(); err != nil {
		return errors.New("AWS CLI installation failed")
	}
	if err = os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		return err
	}
	return os.Symlink(filepath.Join(dir, "aws-cli", "aws"), filepath.Join(dir, "bin", "aws"))
}

func (awsCLI) commandArgs(profile string, args []string) []string {
	return append(awsOutputArgs(args), "--profile", profile, "--no-cli-pager", "--no-cli-auto-prompt")
}
func (awsCLI) syncProfiles(m *Manager, ctx context.Context, force bool) error {
	return m.syncAWSProfiles(ctx, force)
}
func (awsCLI) profileRecord(m *Manager, profile string) *record { return m.awsProfileRecord(profile) }
func (awsCLI) validProfile(profile string) bool                 { return validAWSProfile(profile) }
