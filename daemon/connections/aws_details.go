package connections

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AWSDetails is what a person recognizes about the connected AWS account: its
// name, who is signed in and how. Nothing in it is secret. It is stored on the
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
	Who     string `json:"who"`
	Session string `json:"session,omitempty"`
	// SignIn is login, sso, keys or profile (any other kind of profile).
	SignIn string `json:"sign_in"`
	// ExpiresAt is when the CLI's IAM Identity Center sign-in ends. A login
	// profile has none: its cached credential lasts minutes and the CLI renews it.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

const awsDetailsMaxAge = 6 * time.Hour

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

func awsSignIn(r *record) string {
	if r.Account.AuthMethod == awsMethodKeys {
		return "keys"
	}
	switch p, _ := awsProfile(r.Account.Profile); p.Kind {
	case "login", "sso":
		return p.Kind
	case "keys":
		return "keys"
	}
	return "profile"
}

// awsExpiry is when the CLI's IAM Identity Center sign-in for a profile ends,
// from the CLI's own cache; nil when there is none to read.
func awsExpiry(profile string) *time.Time {
	settings := awsProfileSettings(profile)
	name := settings["sso_session"]
	if name == "" {
		name = settings["sso_start_url"]
	}
	if name == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	sum := sha1.Sum([]byte(name))
	raw, err := os.ReadFile(filepath.Join(home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json"))
	if err != nil {
		return nil
	}
	var cache struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if json.Unmarshal(raw, &cache) != nil {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05UTC"} {
		if t, err := time.Parse(layout, cache.ExpiresAt); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// awsAccountNames asks for the account's name, then its alias. A denial is
// ordinary (an organization's policy can deny both) and is not an error.
func (m *Manager) awsAccountNames(ctx context.Context, r *record, secret string) (name, state string) {
	out, err := m.awsRun(ctx, r, secret, "account", "get-account-information", map[string]any{}, "us-east-1", "")
	if err == nil {
		var info struct {
			AccountName  string `json:"AccountName"`
			AccountState string `json:"AccountState"`
		}
		if json.Unmarshal(out, &info) == nil && strings.TrimSpace(info.AccountName) != "" {
			return strings.TrimSpace(info.AccountName), info.AccountState
		}
	}
	out, err = m.awsRun(ctx, r, secret, "iam", "list-account-aliases", map[string]any{}, "us-east-1", "")
	if err == nil {
		var aliases struct {
			AccountAliases []string `json:"AccountAliases"`
		}
		if json.Unmarshal(out, &aliases) == nil && len(aliases.AccountAliases) > 0 {
			return aliases.AccountAliases[0], ""
		}
	}
	return "", ""
}

// awsDetails builds the details for an identity. The calls for the account's
// name are best effort and bounded; who and how are known without them.
func (m *Manager) awsDetails(ctx context.Context, r *record, secret string, id awsIdentity) *AWSDetails {
	d := &AWSDetails{AccountID: id.Account, CheckedAt: time.Now().UTC()}
	d.Who, d.Session = parseAWSPrincipal(id.Arn)
	d.SignIn = awsSignIn(r)
	if d.SignIn == "sso" {
		d.ExpiresAt = awsExpiry(r.Account.Profile)
	}
	named, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d.AccountName, d.AccountState = m.awsAccountNames(named, r, secret)
	return d
}

// awsDetailsStale: details never read, or read long ago, are read again when
// the account is opened.
func awsDetailsStale(r *record, now time.Time) bool {
	a := r.Account
	if a.Integration != "aws" || !a.Enabled || a.Status == "disconnected" || a.Status == "authorization_required" {
		return false
	}
	return a.AWS == nil || now.Sub(a.AWS.CheckedAt) > awsDetailsMaxAge
}

// awsRefreshDetails reads the details of an account that is opened and keeps
// them. A failure leaves the account as it was: the next call or refresh says
// what is wrong with the sign-in. The attempt is dated either way so a failing
// read is not repeated on every open.
func (m *Manager) awsRefreshDetails(ctx context.Context, r *record) {
	secret, err := m.accountToken(ctx, r)
	if err != nil {
		return
	}
	waiting, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := m.awsWhoami(waiting, r, secret)
	if err != nil {
		if errors.Is(err, errAuth) {
			r.Account.Status = "authorization_required"
		}
		if r.Account.AWS == nil {
			r.Account.AWS = &AWSDetails{AccountID: awsAccountID(r)}
			r.Account.AWS.Who, r.Account.AWS.Session = parseAWSPrincipal(r.Account.Identity)
			r.Account.AWS.SignIn = awsSignIn(r)
		}
		r.Account.AWS.CheckedAt = time.Now().UTC()
		_ = m.save()
		return
	}
	previous := r.Account.AWS
	r.Account.AWS = m.awsDetails(waiting, r, secret, id)
	awsAdoptName(r, previous)
	_ = m.save()
}

// awsAccountID is the 12-digit account of the connection.
func awsAccountID(r *record) string {
	if r.Account.AWS != nil && r.Account.AWS.AccountID != "" {
		return r.Account.AWS.AccountID
	}
	if parts := strings.SplitN(r.Account.Identity, ":", 6); len(parts) == 6 {
		return parts[4]
	}
	return strings.TrimPrefix(r.Account.Name, "AWS ")
}

// awsAdoptName names the account after its AWS account name. A name the person
// chose otherwise is left alone: only "AWS 123456789012" and the earlier AWS
// account name (each with its " (profile)" or " (keys)" suffix) are replaced.
func awsAdoptName(r *record, previous *AWSDetails) {
	d := r.Account.AWS
	if d == nil || d.AccountName == "" {
		return
	}
	name := r.Account.Name
	base, suffix := name, ""
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		base, suffix = name[:i], name[i:]
	}
	if base == "AWS "+d.AccountID || previous != nil && previous.AccountName != "" && base == previous.AccountName {
		r.Account.Name = d.AccountName + suffix
	}
}
