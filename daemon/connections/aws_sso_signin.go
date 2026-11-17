package connections

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// IAM Identity Center (SSO) sign-in. A sign-in belongs to an sso-session, not
// to one profile: every profile naming the session shares it. Adding an
// account signs in to the session with a device code, then writes a profile
// for each account and role the person may use.

var awsSSOSessionHeader = regexp.MustCompile(`^\s*\[\s*sso-session\s+([^\]]+?)\s*\]`)

// awsSSOSessions reads the [sso-session NAME] sections of the config.
func awsSSOSessions(raw []byte) map[string]map[string]string {
	out := map[string]map[string]string{}
	var current map[string]string
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "[") {
			current = nil
			if m := awsSSOSessionHeader.FindStringSubmatch(s); m != nil && validAWSProfile(m[1]) {
				current = map[string]string{}
				out[m[1]] = current
			}
			continue
		}
		if current != nil && !strings.HasPrefix(s, "#") && !strings.HasPrefix(s, ";") {
			if key, value, ok := strings.Cut(s, "="); ok {
				current[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
			}
		}
	}
	return out
}

var awsPortalRegion = []*regexp.Regexp{
	regexp.MustCompile(`^[a-z0-9-]+\.portal\.([a-z]{2}(-[a-z]+)+-\d)\.app\.aws$`),
	regexp.MustCompile(`^[a-z0-9-]+\.([a-z]{2}(-[a-z]+)+-\d)\.portal\.amazonaws\.com$`),
}
var awsRegionName = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)

// awsPortal checks an access portal address and finds its region: newer
// portals name it in the host; a d-… .awsapps.com portal does not, so region
// (the person's choice, else us-east-1) applies.
func awsPortal(address, region string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return "", "", errors.New("Enter your AWS access portal URL, like https://d-1234567890.awsapps.com/start")
	}
	host := strings.ToLower(u.Hostname())
	for _, re := range awsPortalRegion {
		if m := re.FindStringSubmatch(host); m != nil {
			return "https://" + host, m[1], nil
		}
	}
	if strings.HasSuffix(host, ".awsapps.com") && strings.Count(host, ".") == 2 {
		region = strings.TrimSpace(region)
		if region == "" {
			region = awsDefaultRegion
		}
		if !awsRegionName.MatchString(region) {
			return "", "", errors.New("Enter the AWS Region of your access portal, like us-east-1")
		}
		return "https://" + host + "/start", region, nil
	}
	return "", "", errors.New("That is not an AWS access portal URL. It looks like https://d-1234567890.awsapps.com/start")
}

// ensureAWSSSOSession returns the sso-session for a portal, adding one to the
// config when none names it. The scope makes its token renew by itself.
func (m *Manager) ensureAWSSSOSession(startURL, region string) (string, error) {
	config, _, err := m.cloud.profileFiles()
	if err != nil {
		return "", err
	}
	raw, err := readOptional(config)
	if err != nil {
		return "", errors.New("AWS config could not be read")
	}
	sessions := awsSSOSessions(raw)
	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimRight(sessions[name]["sso_start_url"], "/") == strings.TrimRight(startURL, "/") {
			return name, nil
		}
	}
	u, _ := url.Parse(startURL)
	base := strings.SplitN(u.Hostname(), ".", 2)[0]
	name := base
	for i := 2; sessions[name] != nil; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	block := fmt.Sprintf("[sso-session %s]\nsso_start_url = %s\nsso_region = %s\nsso_registration_scopes = sso:account:access\n", name, startURL, region)
	if err := m.appendAWSConfig(config, raw, block); err != nil {
		return "", err
	}
	return name, nil
}

// appendAWSConfig adds sections to the config after a backup, refusing when the
// file changed since it was read.
func (m *Manager) appendAWSConfig(config string, before []byte, block string) error {
	backup, release, err := newCloudBackup(m.cloud.dir, time.Now().UTC().Format("20060102T150405.000000000"))
	if err != nil {
		return err
	}
	defer release()
	if err := atomicfile.Write(filepath.Join(backup, filepath.Base(config)), before, 0600); err != nil {
		return errors.New("AWS config backup failed; nothing was changed")
	}
	current, err := readOptional(config)
	if err != nil || !bytes.Equal(current, before) {
		return errors.New("AWS configuration changed; try again")
	}
	after := append([]byte{}, before...)
	if len(after) > 0 && !bytes.HasSuffix(after, []byte("\n")) {
		after = append(after, '\n')
	}
	after = append(after, block...)
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		return err
	}
	return atomicfile.Write(config, after, 0600)
}

// awsSSOToken is the session's cached sign-in, read only to list the accounts
// it may use and to tell one sign-in from the next. Never stored or sent
// anywhere but AWS.
type awsSSOToken struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"`
}

func readAWSSSOToken(configPath, session string) (awsSSOToken, bool) {
	sum := sha1.Sum([]byte(session))
	var token awsSSOToken
	ok := awsCacheJSON(filepath.Join(filepath.Dir(configPath), "sso", "cache", hex.EncodeToString(sum[:])+".json"), &token)
	return token, ok && token.AccessToken != ""
}

// awsSSOBinding names one sign-in to a session: a digest of its refresh token,
// which Identity Center keeps for the whole sign-in and replaces only when the
// person signs in again. "" when there is no refreshable sign-in.
func awsSSOBinding(configPath, session string) string {
	token, ok := readAWSSSOToken(configPath, session)
	if !ok || token.RefreshToken == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(session + "\x00" + token.RefreshToken))
	return hex.EncodeToString(sum[:16])
}

type awsAssignment struct {
	AccountID   string
	AccountName string
	Role        string
}

// awsPortalBase is where the access portal API answers; tests replace it.
var awsPortalBase = func(region string) string { return "https://portal.sso." + region + ".amazonaws.com" }

// listAWSAssignments asks the access portal which accounts and roles the
// signed-in person may use (the documented ListAccounts/ListAccountRoles).
func (m *Manager) listAWSAssignments(ctx context.Context, region, accessToken string) ([]awsAssignment, error) {
	get := func(path string, query url.Values, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsPortalBase(region)+path+"?"+query.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("x-amz-sso_bearer_token", accessToken)
		resp, err := m.http.Do(req)
		if err != nil {
			return errors.New("AWS access portal could not be reached")
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("AWS access portal refused the account list (%d)", resp.StatusCode)
		}
		return json.Unmarshal(body, out)
	}
	type account struct {
		ID   string `json:"accountId"`
		Name string `json:"accountName"`
	}
	var accounts []account
	for next := ""; ; {
		q := url.Values{"max_result": {"100"}}
		if next != "" {
			q.Set("next_token", next)
		}
		var page struct {
			Accounts []account `json:"accountList"`
			Next     string    `json:"nextToken"`
		}
		if err := get("/assignment/accounts", q, &page); err != nil {
			return nil, err
		}
		accounts = append(accounts, page.Accounts...)
		if next = page.Next; next == "" || len(accounts) > 1000 {
			break
		}
	}
	var out []awsAssignment
	for _, a := range accounts {
		for next := ""; ; {
			q := url.Values{"max_result": {"100"}, "account_id": {a.ID}}
			if next != "" {
				q.Set("next_token", next)
			}
			var page struct {
				Roles []struct {
					Name string `json:"roleName"`
				} `json:"roleList"`
				Next string `json:"nextToken"`
			}
			if err := get("/assignment/roles", q, &page); err != nil {
				return nil, err
			}
			for _, r := range page.Roles {
				out = append(out, awsAssignment{AccountID: a.ID, AccountName: a.Name, Role: r.Name})
			}
			if next = page.Next; next == "" {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountName != out[j].AccountName {
			return out[i].AccountName < out[j].AccountName
		}
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].Role < out[j].Role
	})
	return out, nil
}

var awsProfileUnsafe = regexp.MustCompile(`[^a-z0-9_.-]+`)

// awsAssignmentProfile is a readable profile name: the account, and the role
// too when the person has more than one role in that account.
func awsAssignmentProfile(a awsAssignment, roles int) string {
	name := firstNonEmpty(a.AccountName, a.AccountID)
	if roles > 1 {
		name += "-" + a.Role
	}
	name = awsProfileUnsafe.ReplaceAllString(strings.ToLower(name), "-")
	name = strings.Trim(name, "-.")
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// addAWSSSOProfiles writes a profile for each assignment the session has no
// profile for yet, and returns the profile the sign-in connects: the only
// assignment, else the first new one, else the first that already existed.
func (m *Manager) addAWSSSOProfiles(ctx context.Context, session string) (string, int, error) {
	config, _, err := m.cloud.profileFiles()
	if err != nil {
		return "", 0, err
	}
	raw, err := readOptional(config)
	if err != nil {
		return "", 0, errors.New("AWS config could not be read")
	}
	region := firstNonEmpty(awsSSOSessions(raw)[session]["sso_region"], awsDefaultRegion)
	token, ok := readAWSSSOToken(config, session)
	if !ok {
		return "", 0, errors.New("AWS sign-in finished, but its session could not be read")
	}
	assignments, err := m.listAWSAssignments(ctx, region, token.AccessToken)
	if err != nil {
		return "", 0, err
	}
	if len(assignments) == 0 {
		return "", 0, errors.New("This AWS sign-in has no account assigned. Ask your administrator to assign you an account in IAM Identity Center")
	}
	roles := map[string]int{}
	for _, a := range assignments {
		roles[a.AccountID]++
	}
	profiles := awsSections(raw, true)
	existing := ""
	var block strings.Builder
	first := ""
	added := 0
	for _, a := range assignments {
		found := ""
		for name, fields := range profiles {
			if fields["sso_session"] == session && fields["sso_account_id"] == a.AccountID && fields["sso_role_name"] == a.Role {
				found = name
				break
			}
		}
		if found != "" {
			if existing == "" {
				existing = found
			}
			continue
		}
		base := awsAssignmentProfile(a, roles[a.AccountID])
		name := base
		for i := 2; profiles[name] != nil || name == ""; i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		profiles[name] = map[string]string{"sso_session": session, "sso_account_id": a.AccountID, "sso_role_name": a.Role}
		fmt.Fprintf(&block, "[profile %s]\nsso_session = %s\nsso_account_id = %s\nsso_role_name = %s\n", name, session, a.AccountID, a.Role)
		if first == "" {
			first = name
		}
		added++
	}
	if added > 0 {
		if err := m.appendAWSConfig(config, raw, block.String()); err != nil {
			return "", 0, err
		}
	}
	return firstNonEmpty(first, existing), added, nil
}

// awsSSOSignedIn records the moment Mewla signed a session in, on every
// profile that shares the session, with the sign-in it belongs to.
func (m *Manager) awsSSOSignedIn(session string, at time.Time) {
	config, _, err := m.cloud.profileFiles()
	if err != nil {
		return
	}
	raw, err := readOptional(config)
	if err != nil {
		return
	}
	binding := awsSSOBinding(config, session)
	fields := awsSections(raw, true)
	for _, r := range m.records {
		if r.Account.Integration != "aws" || r.CLI == nil || fields[r.CLI.Profile]["sso_session"] != session {
			continue
		}
		start := at
		r.CLI.SignInAt, r.CLI.SignInBinding, r.CLI.SSOSession = &start, binding, session
		r.Account.ProfileSignInBy, r.Account.ProfileSignInLimit = awsProfileExpiry(config, fields[r.CLI.Profile], r.CLI)
		r.Account.ProfileSignedInAt = r.CLI.SignInAt
	}
}

// awsSessionPresets are the lengths Identity Center offers for a sign-in.
var awsSessionPresets = []time.Duration{
	time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 12 * time.Hour, 24 * time.Hour,
	3 * 24 * time.Hour, 5 * 24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 90 * 24 * time.Hour,
}

// awsSessionLengthOf is the length a sign-in found ended after observed had:
// the longest Identity Center offers that it outlasted, when it ended within a
// check's lag of it (half an hour, or 2% of a long session). A sign-in that
// ended well short of every length (signed out, revoked) teaches nothing.
func awsSessionLengthOf(observed time.Duration) time.Duration {
	found := time.Duration(0)
	for _, preset := range awsSessionPresets {
		if preset <= observed+5*time.Minute {
			found = preset
		}
	}
	if found == 0 || observed-found > max(45*time.Minute, found/50) {
		return 0
	}
	return found
}

// awsSessionEnded learns how long the organization's sign-ins last from one
// Mewla started that has just ended, for every profile of the session.
func (m *Manager) awsSessionEnded(r *record, at time.Time) {
	if r.CLI == nil || r.CLI.SSOSession == "" || r.CLI.SignInAt == nil || r.CLI.SignInBinding == "" {
		return
	}
	config, _, err := m.cloud.profileFiles()
	if err != nil || awsSSOBinding(config, r.CLI.SSOSession) != r.CLI.SignInBinding {
		return
	}
	length := awsSessionLengthOf(at.Sub(*r.CLI.SignInAt))
	if length == 0 {
		return
	}
	m.setAWSSessionLength(r.CLI.SSOSession, length)
}

func (m *Manager) setAWSSessionLength(session string, length time.Duration) {
	for _, other := range m.records {
		if other.Account.Integration == "aws" && other.CLI != nil && other.CLI.SSOSession == session {
			other.CLI.SessionLength = int64(length / time.Second)
			other.Account.ProfileSessionLength = other.CLI.SessionLength
		}
	}
}

// setCloudSessionLength is the person saying how long their organization's
// sign-ins last (Identity Center › Settings › Authentication), for a profile's
// whole session.
func (m *Manager) setCloudSessionLength(q Request) (Response, error) {
	r := m.records[q.ID]
	if r == nil || r.CLI == nil || r.Account.Integration != "aws" || r.CLI.SSOSession == "" {
		return Response{}, errors.New("Only an IAM Identity Center profile has a sign-in length")
	}
	length := time.Duration(q.Seconds) * time.Second
	valid := length == 0
	for _, preset := range awsSessionPresets {
		valid = valid || preset == length
	}
	if !valid {
		return Response{}, errors.New("Choose one of the lengths IAM Identity Center offers")
	}
	m.setAWSSessionLength(r.CLI.SSOSession, length)
	config, _, err := m.cloud.profileFiles()
	if err == nil {
		raw, _ := readOptional(config)
		fields := awsSections(raw, true)
		for _, other := range m.records {
			if other.Account.Integration == "aws" && other.CLI != nil && other.CLI.SSOSession == r.CLI.SSOSession {
				other.Account.ProfileSignInBy, other.Account.ProfileSignInLimit = awsProfileExpiry(config, fields[other.CLI.Profile], other.CLI)
			}
		}
	}
	if err := m.save(); err != nil {
		return Response{}, err
	}
	account := m.projection(r)
	return Response{Account: &account}, nil
}

// shareAWSSSOSignIns gives every profile of a session what Mewla knows about
// its sign-in (a profile added later, or by another sign-in's account list),
// and projects it: the sign-in's start while it is still the same sign-in.
func (m *Manager) shareAWSSSOSignIns(config string) {
	type known struct {
		at      *time.Time
		binding string
		length  int64
	}
	bySession := map[string]known{}
	bindings := map[string]string{}
	for _, r := range m.records {
		if r.Account.Integration != "aws" || r.CLI == nil || r.CLI.SSOSession == "" {
			continue
		}
		session := r.CLI.SSOSession
		if _, ok := bindings[session]; !ok {
			bindings[session] = awsSSOBinding(config, session)
		}
		k := bySession[session]
		if r.CLI.SignInAt != nil && r.CLI.SignInBinding != "" && r.CLI.SignInBinding == bindings[session] && (k.at == nil || r.CLI.SignInAt.After(*k.at)) {
			k.at, k.binding = r.CLI.SignInAt, r.CLI.SignInBinding
		}
		if r.CLI.SessionLength > k.length {
			k.length = r.CLI.SessionLength
		}
		bySession[session] = k
	}
	for _, r := range m.records {
		if r.Account.Integration != "aws" || r.CLI == nil {
			continue
		}
		r.Account.ProfileSignedInAt, r.Account.ProfileSessionLength = nil, 0
		if r.CLI.SSOSession == "" {
			continue
		}
		k := bySession[r.CLI.SSOSession]
		if k.at != nil {
			at := *k.at
			r.CLI.SignInAt, r.CLI.SignInBinding = &at, k.binding
			r.Account.ProfileSignedInAt = &at
		}
		r.CLI.SessionLength = k.length
		r.Account.ProfileSessionLength = k.length
	}
}
