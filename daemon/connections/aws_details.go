package connections

import (
	"context"
	"encoding/json"
	"errors"
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

// awsDetailsOf builds the details for an identity and what the probe read of
// its account. The name is the account's name, else its alias: a denial is
// ordinary (an organization's policy can deny both) and is not an error.
func awsDetailsOf(id awsIdentity, probe awsProbeResult) *AWSDetails {
	d := &AWSDetails{AccountID: id.Account, CheckedAt: time.Now().UTC()}
	d.Who, d.Session = parseAWSPrincipal(id.Arn)
	if probe.Account.OK {
		var info struct {
			AccountName  string `json:"AccountName"`
			AccountState string `json:"AccountState"`
		}
		if json.Unmarshal(probe.Account.Data, &info) == nil && strings.TrimSpace(info.AccountName) != "" {
			d.AccountName, d.AccountState = strings.TrimSpace(info.AccountName), info.AccountState
			return d
		}
	}
	if probe.Aliases.OK {
		var aliases struct {
			AccountAliases []string `json:"AccountAliases"`
		}
		if json.Unmarshal(probe.Aliases.Data, &aliases) == nil && len(aliases.AccountAliases) > 0 {
			d.AccountName = aliases.AccountAliases[0]
		}
	}
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
	waiting, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	probe, err := m.awsProbe(waiting, r, secret)
	var id awsIdentity
	if err == nil {
		id, err = awsIdentityOf(probe.Identity.Data)
	}
	if err != nil {
		if errors.Is(err, errAuth) {
			r.Account.Status = "authorization_required"
		}
		if r.Account.AWS == nil {
			r.Account.AWS = &AWSDetails{AccountID: awsAccountID(r)}
			r.Account.AWS.Who, r.Account.AWS.Session = parseAWSPrincipal(r.Account.Identity)
		}
		r.Account.AWS.CheckedAt = time.Now().UTC()
		_ = m.save()
		return
	}
	previous := r.Account.AWS
	r.Account.AWS = awsDetailsOf(id, probe)
	awsAdoptName(r, previous)
	_ = m.save()
}

// awsAdoptName names the account after its AWS account name. A name the person
// chose otherwise is left alone: only "AWS 123456789012" and the earlier AWS
// account name are replaced.
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
