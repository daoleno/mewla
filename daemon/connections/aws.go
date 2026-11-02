package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const awsDefaultRegion = "us-east-1"

type callRefused string

func (e callRefused) Error() string { return string(e) }
func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return ""
}

type awsIdentity struct {
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
	UserID  string `json:"UserId"`
}

func awsTools() []Tool {
	props := map[string]any{"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2, "maxItems": 256, "description": "AWS CLI arguments as literal words, e.g. [ec2, describe-instances, --region, us-east-1]. No shell."}, "authorize": map[string]any{"type": "boolean", "description": "True only with explicit user authorization for this task."}}
	return []Tool{
		hlTool("whoami", "Show the AWS CLI profile's current account and identity.", true, map[string]any{}),
		hlTool("read", "Run a vetted read-only AWS CLI command. Prefer mewla aws <args>. Secret-value reads require execute.", true, props, "args"),
		hlTool("execute", "Run an AWS CLI command with explicit task-specific user authorization; pass authorize=true. Prefer mewla aws --mewla-authorize <args>. Never retried.", false, props, "args"),
	}
}
func discoverAWS(m *Manager, ctx context.Context, r *record, _ string) ([]Tool, error) {
	if r.CLI == nil {
		return nil, errAuth
	}
	out, err := m.cloudCommand(ctx, "aws", r.CLI.Profile, []string{"sts", "get-caller-identity", "--output", "json", "--region", awsDefaultRegion})
	if err != nil {
		return nil, err
	}
	var id awsIdentity
	if out.ExitCode != 0 || json.Unmarshal([]byte(out.Stdout), &id) != nil || id.Account == "" {
		return nil, errAuth
	}
	r.Account.Identity = id.Arn
	r.Account.AWS = &AWSDetails{AccountID: id.Account}
	r.Account.AWS.Who, r.Account.AWS.Session = parseAWSPrincipal(id.Arn)
	return awsTools(), nil
}
func invokeAWSDriver(m *Manager, ctx context.Context, r *record, _ string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	if r.CLI == nil {
		return nil, errAuth
	}
	var argv []string
	if t.Name == "whoami" {
		argv = []string{"sts", "get-caller-identity"}
	} else {
		values, _ := args["args"].([]any)
		for _, v := range values {
			s, ok := v.(string)
			if !ok {
				return nil, errors.New("CLI arguments must be strings")
			}
			argv = append(argv, s)
		}
	}
	read, err := awsCLI{}.classify(argv)
	if err != nil {
		return nil, err
	}
	if t.Name == "read" && !read {
		return nil, callRefused("This command requires execute and task-specific user authorization")
	}
	if !read && args["authorize"] != true {
		return nil, callRefused("Explicit user authorization is required; pass authorize=true only after it is given")
	}
	out, err := m.cloudCommand(ctx, "aws", r.CLI.Profile, awsOutputArgs(argv))
	if err != nil {
		return nil, err
	}
	if out.ExitCode != 0 && awsExpired(out.Stderr) {
		return nil, errAuth
	}
	// JSON envelope transports CLI text unchanged to remote callers.
	return json.Marshal(out)
}
