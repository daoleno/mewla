package connections

import (
	"regexp"
	"strings"
	"time"
)

// AWSDetails is what a person recognizes about the connected AWS account: its
// name and who is signed in. Nothing in it is secret. It is stored on the
// account when the account is connected or refreshed, and read again lazily
// (see awsDetailsStale), never on every render.
type AWSDetails struct {
	AccountID string `json:"account_id"`
	// AccountName is the name of the AWS account (Account Management), else its
	// alias; empty when the sign-in may read neither (an SCP often denies them).
	AccountName  string `json:"account_name,omitempty"`
	AccountState string `json:"account_state,omitempty"`
	// Who is the signed-in identity in plain words: "Root user", a role's name or
	// "IAM user NAME". Session is the role session's name, if any.
	Who       string    `json:"who"`
	Session   string    `json:"session,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

var awsSSORoleName = regexp.MustCompile(`^AWSReservedSSO_(.+)_[0-9a-f]{16}$`)

// parseAWSPrincipal reads an identity ARN into plain words. who is never the ARN.
func parseAWSPrincipal(arn string) (who, session string) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 {
		return "AWS identity", ""
	}
	resource := parts[5]
	kind, rest, _ := strings.Cut(resource, "/")
	switch {
	case resource == "root":
		return "Root user", ""
	case kind == "assumed-role":
		role, sess, _ := strings.Cut(rest, "/")
		if m := awsSSORoleName.FindStringSubmatch(role); m != nil {
			role = m[1]
		}
		return role, sess
	case kind == "user" && rest != "":
		return "IAM user " + rest[strings.LastIndex(rest, "/")+1:], ""
	case kind == "federated-user" && rest != "":
		return "Federated user " + rest, ""
	case kind == "role" && rest != "":
		return rest[strings.LastIndex(rest, "/")+1:], ""
	}
	return "AWS identity", ""
}
