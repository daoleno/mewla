package connections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AWS is reached through the AWS CLI on the server's computer. The CLI already
// signs requests for every service and protocol, and it refreshes the session
// of a profile signed in with `aws login` or `aws sso login`, so Mewla keeps no
// AWS credential of its own for a profile. Access keys pasted into Extensions
// are the exception: they sit in the vault and reach the CLI only through the
// environment of one call.
//
// Five tools: search and docs read the CLI's own service models, whoami asks
// STS who is signed in, read runs an operation that only reads, and execute
// runs any operation, which is the one that needs the person's authorization.
// The classification is by the operation's name (see awsKind); the person's
// IAM permissions remain the real boundary.

const (
	awsMethodProfile = "aws_profile"
	awsMethodKeys    = "aws_keys"

	awsCallTimeout = 18 * time.Second
	awsDefaultKeys = "us-east-1"
)

var (
	awsProfileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@+=:/-]{0,63}$`)
	awsRegionName  = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]+){1,3}$`)
	awsAccessKeyID = regexp.MustCompile(`^[A-Z0-9]{16,128}$`)
	awsServiceName = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	awsSecretKey   = regexp.MustCompile(`(?i)secret|password|token|key|credential|private`)
)

// awsCommand is one run of the CLI.
type awsCommand struct {
	Args  []string // after "aws"
	Env   []string // the whole environment of the child
	Input []byte   // JSON parameters, handed over in a private file
}

// awsExec runs the CLI and returns what it printed. A non-zero exit is an
// *awsExitError carrying its error text.
type awsExec func(ctx context.Context, c awsCommand) ([]byte, error)

type awsExitError struct{ stderr string }

func (e *awsExitError) Error() string { return e.stderr }

// limitedBuffer keeps at most max bytes and counts the rest.
type limitedBuffer struct {
	buf     bytes.Buffer
	max     int
	dropped bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); len(p) > room {
		b.dropped = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

var errAWSTooLarge = errors.New("the result is larger than 1 MiB; ask for less with the operation's own filters and MaxResults, or cut it down with query")

func runAWS(ctx context.Context, c awsCommand) ([]byte, error) {
	bin, err := exec.LookPath("aws")
	if err != nil {
		return nil, errAWSMissing
	}
	args := c.Args
	if len(c.Input) > 0 {
		// Parameters can hold values that must not appear in a process list, so
		// they go through a file only this user can open.
		dir, err := os.MkdirTemp("", "mewla-aws-")
		if err != nil {
			return nil, errors.New("could not prepare the AWS call")
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "params.json")
		if err := os.WriteFile(path, c.Input, 0o600); err != nil {
			return nil, errors.New("could not prepare the AWS call")
		}
		args = append(append([]string{}, args...), "--cli-input-json=file://"+path)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = c.Env
	cmd.WaitDelay = 2 * time.Second
	out := &limitedBuffer{max: MaxResultBytes + 1}
	errOut := &limitedBuffer{max: 8 << 10}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, &awsExitError{stderr: errOut.buf.String()}
		}
		return nil, err
	}
	if out.dropped {
		return nil, errAWSTooLarge
	}
	return out.buf.Bytes(), nil
}

// ---- profiles -----------------------------------------------------------

// AWSProfile is a profile of the AWS CLI on the server's computer. It says how
// the profile signs in and never holds a key.
type AWSProfile struct {
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
	// Kind is login (aws login), sso (aws sso login), keys, role or other.
	Kind string `json:"kind"`
}

func awsConfigFiles() (config, credentials string) {
	home, _ := os.UserHomeDir()
	config = os.Getenv("AWS_CONFIG_FILE")
	if config == "" {
		config = filepath.Join(home, ".aws", "config")
	}
	credentials = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if credentials == "" {
		credentials = filepath.Join(home, ".aws", "credentials")
	}
	return config, credentials
}

// parseAWSFile reads the top-level settings of each section of an AWS config
// or credentials file. The config file names its sections "profile NAME" and
// the credentials file names them "NAME".
func parseAWSFile(path string, config bool) map[string]map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]map[string]string{}
	var section map[string]string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			section = nil
			switch {
			case !config:
				section = map[string]string{}
			case name == "default":
				name = "default"
				section = map[string]string{}
			case strings.HasPrefix(name, "profile "):
				name = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
				section = map[string]string{}
			}
			if section != nil {
				out[name] = section
			}
			continue
		}
		// Indented lines belong to a nested block such as s3 settings.
		if section == nil || raw != strings.TrimLeft(raw, " \t") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			section[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	return out
}

func awsProfileKind(settings map[string]string) string {
	switch {
	case settings["login_session"] != "":
		return "login"
	case settings["sso_session"] != "" || settings["sso_start_url"] != "":
		return "sso"
	case settings["aws_access_key_id"] != "":
		return "keys"
	case settings["role_arn"] != "":
		return "role"
	}
	return "other"
}

// awsProfiles lists the profiles of this computer's AWS CLI, sorted by name.
func awsProfiles() []AWSProfile {
	configFile, credentialsFile := awsConfigFiles()
	merged := map[string]map[string]string{}
	for name, s := range parseAWSFile(credentialsFile, false) {
		merged[name] = s
	}
	for name, s := range parseAWSFile(configFile, true) {
		if merged[name] == nil {
			merged[name] = map[string]string{}
		}
		for k, v := range s {
			merged[name][k] = v
		}
	}
	out := make([]AWSProfile, 0, len(merged))
	for name, s := range merged {
		if awsProfileName.MatchString(name) {
			out = append(out, AWSProfile{Name: name, Region: s["region"], Kind: awsProfileKind(s)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func awsProfile(name string) (AWSProfile, bool) {
	for _, p := range awsProfiles() {
		if p.Name == name {
			return p, true
		}
	}
	return AWSProfile{}, false
}

// ---- keys ---------------------------------------------------------------

type awsKeys struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
}

// parseAWSKeys reads pasted access keys: this driver's own JSON, or what
// `aws configure export-credentials --format process` prints.
func parseAWSKeys(text string) (awsKeys, error) {
	var k awsKeys
	var cli struct {
		AccessKeyID     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
	}
	raw := []byte(strings.TrimSpace(text))
	if json.Unmarshal(raw, &k) != nil || k.AccessKeyID == "" {
		if json.Unmarshal(raw, &cli) != nil {
			return k, errors.New("paste an access key ID and secret access key")
		}
		k = awsKeys{AccessKeyID: cli.AccessKeyID, SecretAccessKey: cli.SecretAccessKey, SessionToken: cli.SessionToken}
	}
	k.AccessKeyID = strings.TrimSpace(k.AccessKeyID)
	k.SecretAccessKey = strings.TrimSpace(k.SecretAccessKey)
	k.SessionToken = strings.TrimSpace(k.SessionToken)
	if !awsAccessKeyID.MatchString(k.AccessKeyID) {
		return k, errors.New("that is not an AWS access key ID")
	}
	if len(k.SecretAccessKey) < 16 || len(k.SecretAccessKey) > 256 || len(k.SessionToken) > 8<<10 || strings.ContainsAny(k.SecretAccessKey+k.SessionToken, " \r\n\t") {
		return k, errors.New("that is not an AWS secret access key")
	}
	return k, nil
}

// awsKeyValues are the parts of a saved key that must never appear in output.
func awsKeyValues(r *record, secret string) []string {
	if r.Account.AuthMethod != awsMethodKeys {
		return nil
	}
	k, err := parseAWSKeys(secret)
	if err != nil {
		return nil
	}
	return []string{k.AccessKeyID, k.SecretAccessKey, k.SessionToken}
}

// ---- environment and errors ---------------------------------------------

var awsPassthrough = []string{"PATH", "LANG", "LC_ALL", "TZ", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR", "AWS_CA_BUNDLE", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY", "https_proxy", "http_proxy", "no_proxy", "all_proxy"}

// awsEnv is the whole environment of a call. No inherited AWS_* setting
// reaches the CLI except where its config files are (and a CA bundle), so the
// account that answers is the one the connection names.
func awsEnv(r *record, secret, home string) ([]string, error) {
	env := []string{"AWS_PAGER=", "AWS_CLI_AUTO_PROMPT=off", "AWS_RETRY_MODE=standard"}
	for _, name := range awsPassthrough {
		if v := os.Getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}
	switch r.Account.AuthMethod {
	case awsMethodProfile:
		env = append(env, "HOME="+os.Getenv("HOME"))
		for _, name := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE"} {
			if v := os.Getenv(name); v != "" {
				env = append(env, name+"="+v)
			}
		}
	case awsMethodKeys:
		k, err := parseAWSKeys(secret)
		if err != nil {
			return nil, errAWSKeysLost
		}
		region := r.Account.Region
		if region == "" {
			region = awsDefaultKeys
		}
		env = append(env, "HOME="+home, "AWS_CONFIG_FILE="+os.DevNull, "AWS_SHARED_CREDENTIALS_FILE="+os.DevNull, "AWS_EC2_METADATA_DISABLED=true",
			"AWS_ACCESS_KEY_ID="+k.AccessKeyID, "AWS_SECRET_ACCESS_KEY="+k.SecretAccessKey, "AWS_DEFAULT_REGION="+region)
		if k.SessionToken != "" {
			env = append(env, "AWS_SESSION_TOKEN="+k.SessionToken)
		}
	default:
		return nil, errors.New("this AWS account has no sign-in; connect it again")
	}
	return env, nil
}

// callRefused is a call Mewla did not send: it names something that does not
// exist, or asks for what a tool does not do. It says nothing about the account.
type callRefused string

func (e callRefused) Error() string { return string(e) }

var errAWSKeysLost = awsAuthError("the saved access keys are unreadable; add them again")

// awsAuthError is a call that needs a new sign-in. It is an errAuth, so the
// account reads "Needs sign-in again", and its text says what to do.
type awsAuthError string

func (e awsAuthError) Error() string        { return string(e) }
func (e awsAuthError) Is(target error) bool { return target == errAuth }

// awsLoginCommand is the command that renews a profile's session.
func awsLoginCommand(r *record) string {
	profile := r.Account.Profile
	if r.Account.AuthMethod == awsMethodKeys {
		return ""
	}
	if p, ok := awsProfile(profile); ok && p.Kind == "sso" {
		return "aws sso login --profile " + profile
	}
	return "aws login --profile " + profile
}

// awsFailure turns the CLI's error text into one line for the person.
func awsFailure(r *record, stderr string) error {
	text := strings.TrimSpace(stderr)
	text = strings.TrimSpace(strings.TrimPrefix(text, "aws: [ERROR]:"))
	lower := strings.ToLower(text)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(lower, strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
	switch {
	case has("could not be found") && has("config profile"):
		return fmt.Errorf("AWS profile %s is no longer in this computer's AWS config", r.Account.Profile)
	case has("error loading login session", "reauthenticate with 'aws login'", "error loading sso token", "sso session associated with this profile has expired", "token has expired and refresh failed", "aws sso login",
		"expiredtoken", "requestexpired", "security token included in the request is expired", "nocredentials", "unable to locate credentials",
		"invalidclienttokenid", "unrecognizedclientexception", "signaturedoesnotmatch", "authfailure"):
		if r.Account.AuthMethod == awsMethodKeys {
			return awsAuthError("these AWS access keys expired or were revoked; add new keys")
		}
		return awsAuthError("AWS session expired; run `" + awsLoginCommand(r) + "`")
	case has("you must specify a region"):
		return callRefused("this profile has no default region; pass region, for example us-east-1")
	}
	if text == "" {
		return serviceError{"AWS", "the CLI failed without a message"}
	}
	return serviceError{"AWS", cutText(text, maxServiceErrorBytes)}
}

// ---- tools --------------------------------------------------------------

func awsText(desc string, max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max, "description": desc}
}

func awsCallProps() map[string]any {
	return map[string]any{
		"service":   map[string]any{"type": "string", "pattern": "^[a-z0-9-]+$", "minLength": 1, "maxLength": 64, "description": "The AWS CLI's service name, for example ec2, rds, logs, s3api, iam"},
		"operation": awsText("The API operation, for example DescribeDBInstances; describe-db-instances works too", 100),
		"params":    map[string]any{"type": "object", "description": "The operation's parameters, with the API's own names as docs lists them, for example {\"DBInstanceIdentifier\":\"main\"}"},
		"region":    awsText("A region such as us-east-1, instead of the account's default", 32),
		"query":     awsText("A JMESPath expression to cut the result down, for example DBInstances[].DBInstanceIdentifier", 1000),
	}
}

func awsTools() []Tool {
	return []Tool{
		hlTool("whoami", "Show who is signed in to AWS: the account ID, the identity's ARN, the profile and the default region. Asks STS GetCallerIdentity.", true, map[string]any{}),
		hlTool("search", "Search the AWS API for the operations that do what you need, across every service (for example \"rds instances\" or \"cloudwatch metric statistics\"). Each match names its service and operation and says whether read can run it. Use docs for its parameters.", true, map[string]any{
			"query":   awsText("Words from the operation's name or description", 200),
			"service": map[string]any{"type": "string", "pattern": "^[a-z0-9-]+$", "maxLength": 64, "description": "Only this service"},
			"limit":   hlInt(1, 25),
		}, "query"),
		hlTool("docs", "Show one operation: what it does, its parameters and types, and what it returns. Parameters go in params with exactly these names.", true, map[string]any{
			"service":   awsCallProps()["service"],
			"operation": awsCallProps()["operation"],
		}, "service", "operation"),
		hlTool("read", "Run an AWS operation that only reads (Describe*, List*, Get*, BatchGet*, Head*, Lookup*, Search*) with the account's sign-in. Returns one page; pass the operation's own MaxResults and NextToken to page. Refuses anything that changes state or hands out a secret: those need execute. Secret values are redacted in results.", true, awsCallProps(), "service", "operation"),
		hlTool("execute", "Run any AWS operation, changes included (create, update, delete, start, stop, put, send, invoke), with the account's sign-in. Can create, modify and delete real resources and cost money: only on the user's explicit instruction for this task, and say what it will do first. Secret values are redacted in results. Never retried.", false, awsCallProps(), "service", "operation"),
	}
}

// ---- model index --------------------------------------------------------

// awsModels returns the operations index, reading the CLI's models on first use.
func (m *Manager) awsModels() (*awsIndex, error) {
	m.awsOnce.Do(func() {
		dir, err := m.awsModelDir()
		if err == nil {
			m.awsIdx, err = loadAWSIndex(dir)
		}
		m.awsIdxErr = err
	})
	return m.awsIdx, m.awsIdxErr
}

// ---- driver -------------------------------------------------------------

// awsRun runs one operation of the account and returns the CLI's JSON output.
func (m *Manager) awsRun(ctx context.Context, r *record, secret, service, command string, params map[string]any, region, query string) ([]byte, error) {
	home := ""
	if r.Account.AuthMethod == awsMethodKeys {
		dir, err := os.MkdirTemp("", "mewla-aws-home-")
		if err != nil {
			return nil, errors.New("could not prepare the AWS call")
		}
		defer os.RemoveAll(dir)
		home = dir
	}
	env, err := awsEnv(r, secret, home)
	if err != nil {
		return nil, err
	}
	args := []string{service, command, "--output=json", "--no-cli-pager", "--no-cli-auto-prompt", "--no-paginate"}
	if r.Account.AuthMethod == awsMethodProfile {
		args = append(args, "--profile="+r.Account.Profile)
	}
	if region != "" {
		args = append(args, "--region="+region)
	}
	if query != "" {
		args = append(args, "--query="+query)
	}
	input, err := json.Marshal(params)
	if err != nil {
		return nil, callRefused("params must be a JSON object")
	}
	ctx, cancel := context.WithTimeout(ctx, awsCallTimeout)
	defer cancel()
	out, err := m.awsExec(ctx, awsCommand{Args: args, Env: env, Input: input})
	if err != nil {
		var exit *awsExitError
		if errors.As(err, &exit) {
			return nil, awsFailure(r, string(redactValues([]byte(exit.stderr), awsKeyValues(r, secret))))
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, timeoutError{service: "AWS", after: awsCallTimeout}
		}
		return nil, err
	}
	return out, nil
}

type awsIdentity struct {
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
	UserID  string `json:"UserId"`
}

func (m *Manager) awsWhoami(ctx context.Context, r *record, secret string) (awsIdentity, error) {
	out, err := m.awsRun(ctx, r, secret, "sts", "get-caller-identity", map[string]any{}, "", "")
	if err != nil {
		return awsIdentity{}, err
	}
	var id awsIdentity
	if json.Unmarshal(out, &id) != nil || id.Account == "" || id.Arn == "" {
		return awsIdentity{}, errors.New("AWS did not say who is signed in")
	}
	return id, nil
}

func discoverAWS(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error) {
	a := &r.Account
	switch a.AuthMethod {
	case awsMethodProfile:
		if !awsProfileName.MatchString(a.Profile) {
			return nil, errors.New("this account has no profile; connect it again")
		}
		p, ok := awsProfile(a.Profile)
		if !ok {
			return nil, fmt.Errorf("AWS profile %s is no longer in this computer's AWS config", a.Profile)
		}
		a.Region = p.Region
	case awsMethodKeys:
		if _, err := parseAWSKeys(secret); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("this account has no sign-in; connect it again")
	}
	id, err := m.awsWhoami(ctx, r, secret)
	if err != nil {
		return nil, err
	}
	a.Identity = id.Arn
	return awsTools(), nil
}

// awsAccountID is the 12-digit account of an identity, as the account name uses it.
func awsAccountName(id awsIdentity) string { return "AWS " + id.Account }

func addAWSDriver(m *Manager, ctx context.Context, in *Input) (Response, error) {
	profile := strings.TrimSpace(in.Profile)
	credential := strings.TrimSpace(in.Credential)
	region := strings.TrimSpace(in.Region)
	if region != "" && !awsRegionName.MatchString(region) {
		return Response{}, errors.New("that is not an AWS region")
	}
	if (profile == "") == (credential == "") {
		return Response{}, errors.New("choose an AWS profile on this computer, or paste access keys")
	}
	method, secret := awsMethodProfile, ""
	cand := &record{Account: Account{ID: uuid.NewString(), Integration: "aws", Enabled: true, History: []Event{}}, Grants: map[string]string{}}
	if profile != "" {
		if !awsProfileName.MatchString(profile) {
			return Response{}, errors.New("that is not a valid AWS profile name")
		}
		cand.Account.Profile = profile
	} else {
		keys, err := parseAWSKeys(credential)
		if err != nil {
			return Response{}, err
		}
		raw, _ := json.Marshal(keys)
		method, secret = awsMethodKeys, string(raw)
		if region == "" {
			region = awsDefaultKeys
		}
		cand.Account.Region = region
	}
	cand.Account.AuthMethod = method
	if err := m.discover(ctx, cand, secret); err != nil {
		return Response{}, errors.New(redactText(redactText(err.Error(), secret), credential))
	}
	id, err := m.awsWhoami(ctx, cand, secret)
	if err != nil {
		return Response{}, err
	}
	// The same identity through the same sign-in is one account: connecting it
	// again renews it in place and keeps what the person allowed.
	var r *record
	for _, existing := range m.records {
		a := existing.Account
		if a.Integration == "aws" && a.Status != "disconnected" && a.Identity == cand.Account.Identity && a.AuthMethod == method && a.Profile == cand.Account.Profile {
			r = existing
		}
	}
	if r == nil && len(m.records) >= 50 {
		return Response{}, errors.New("maximum of 50 accounts reached")
	}
	if r == nil {
		r = cand
		r.Account.Name = awsAccountName(id)
		for _, other := range m.records {
			if other.Account.Name == r.Account.Name && other.Account.Integration == "aws" && other.Account.Status != "disconnected" {
				if method == awsMethodProfile {
					r.Account.Name += " (" + profile + ")"
				} else {
					r.Account.Name += " (keys)"
				}
				break
			}
		}
	}
	ref := "integration:" + r.Account.ID
	if method == awsMethodKeys {
		if err := m.vault.Set(ref, secret); err != nil {
			return Response{}, errors.New("credential could not be stored")
		}
	}
	isNew := r == cand
	if !isNew {
		r.Account.Tools, r.Account.Identity, r.Account.Region = cand.Account.Tools, cand.Account.Identity, cand.Account.Region
		r.Account.Status, r.Account.VerifiedAt, r.Account.Enabled = cand.Account.Status, cand.Account.VerifiedAt, true
	}
	before := map[string]bool{}
	for name := range r.Grants {
		before[name] = true
	}
	for _, t := range r.Account.Tools {
		if t.Read && !before[t.Name] {
			r.Grants[t.Name] = fingerprint(t)
		}
	}
	m.records[r.Account.ID] = r
	if err := m.save(); err != nil {
		if isNew {
			delete(m.records, r.Account.ID)
			_ = m.vault.Delete(ref)
		}
		return Response{}, err
	}
	a := m.projection(r)
	return Response{Account: &a}, nil
}

// ---- invoke -------------------------------------------------------------

// awsUnrunnable are operations whose CLI command is not a plain request: it
// writes to a file or opens an interactive session through a helper program.
var awsUnrunnable = map[string]string{
	"lambda/Invoke":      "writes its reply to a local file",
	"ssm/StartSession":   "opens an interactive session through the Session Manager plugin",
	"ecs/ExecuteCommand": "opens an interactive session through the Session Manager plugin",
}

func awsArgs(args map[string]any) (service, operation, region, query string, params map[string]any, err error) {
	service, _ = args["service"].(string)
	operation, _ = args["operation"].(string)
	region, _ = args["region"].(string)
	query, _ = args["query"].(string)
	params = map[string]any{}
	if p, ok := args["params"]; ok && p != nil {
		if params, ok = p.(map[string]any); !ok {
			return "", "", "", "", nil, callRefused("params must be a JSON object")
		}
	}
	if !awsServiceName.MatchString(service) {
		return "", "", "", "", nil, callRefused("name the AWS service, for example ec2")
	}
	if operation == "" {
		return "", "", "", "", nil, callRefused("name the operation, for example DescribeInstances")
	}
	if region != "" && !awsRegionName.MatchString(region) {
		return "", "", "", "", nil, callRefused("region must look like us-east-1")
	}
	if err := awsCheckParams(params); err != nil {
		return "", "", "", "", nil, err
	}
	return service, operation, region, query, params, nil
}

// awsCheckParams refuses a parameter that points the CLI at a file. The CLI
// resolves file:// and fileb:// in its own arguments; a call must never read a
// path on the server's disk.
func awsCheckParams(v any) error {
	switch v := v.(type) {
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		if strings.HasPrefix(lower, "file://") || strings.HasPrefix(lower, "fileb://") {
			return callRefused("a parameter cannot read a file from this computer; put the content in the parameter itself")
		}
	case []any:
		for _, e := range v {
			if err := awsCheckParams(e); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, e := range v {
			if err := awsCheckParams(k); err != nil {
				return err
			}
			if err := awsCheckParams(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func invokeAWSDriver(m *Manager, ctx context.Context, r *record, secret string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	var known *Tool
	for _, c := range awsTools() {
		if c.Name == t.Name {
			v := c
			known = &v
		}
	}
	if known == nil || fingerprint(*known) != fingerprint(t) {
		return nil, errors.New("tool definition changed; refresh this account, then confirm the update in Extensions")
	}
	switch t.Name {
	case "whoami":
		id, err := m.awsWhoami(ctx, r, secret)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"account": id.Account, "arn": id.Arn, "user_id": id.UserID, "profile": r.Account.Profile, "region": r.Account.Region})
	case "search", "docs":
		idx, err := m.awsModels()
		if err != nil {
			return nil, err
		}
		if t.Name == "search" {
			query, _ := args["query"].(string)
			service, _ := args["service"].(string)
			limit := 10
			if n, ok := args["limit"].(float64); ok {
				limit = int(n)
			}
			matches, err := idx.search(query, service, limit)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{"matches": matches, "next": "docs shows an operation's parameters; read runs a Describe/List/Get, execute anything else"})
		}
		return m.awsDocs(idx, args)
	}
	return m.awsCall(ctx, r, secret, t.Name, args)
}

func (m *Manager) awsDocs(idx *awsIndex, args map[string]any) ([]byte, error) {
	service, _ := args["service"].(string)
	operation, _ := args["operation"].(string)
	op, err := idx.operation(service, operation)
	if err != nil {
		return nil, err
	}
	svc := idx.services[op.Service]
	shapes, err := idx.loadShapes(svc)
	if err != nil {
		return nil, errors.New("could not read the AWS service model")
	}
	kind := awsKind(op.Name, nil)
	tool := "read"
	if kind == "write" {
		tool = "execute"
	}
	var doc string
	if rc, err := openModel(svc.file); err == nil {
		var d struct {
			Operations map[string]struct {
				Documentation string `json:"documentation"`
			} `json:"operations"`
		}
		if json.NewDecoder(rc).Decode(&d) == nil {
			doc = plainText(d.Operations[op.Name].Documentation)
		}
		rc.Close()
	}
	out := map[string]any{
		"service": op.Service, "operation": op.Name, "command": "aws " + op.Service + " " + op.Command,
		"kind": kind, "run_with": tool, "description": cutText(doc, 1800),
		"parameters": shapes.members(op.input, true), "returns": shapes.members(op.output, false),
	}
	if shapes.streams(op.input) || shapes.streams(op.output) {
		out["note"] = "This operation streams data to or from a local file, which Mewla does not run."
	}
	if op.Deprecated {
		out["deprecated"] = true
	}
	return json.Marshal(out)
}

func (m *Manager) awsCall(ctx context.Context, r *record, secret, tool string, args map[string]any) ([]byte, error) {
	service, operation, region, query, params, err := awsArgs(args)
	if err != nil {
		return nil, err
	}
	idx, err := m.awsModels()
	if err != nil {
		return nil, err
	}
	op, err := idx.operation(service, operation)
	if err != nil {
		return nil, err
	}
	if why, ok := awsUnrunnable[op.Service+"/"+op.Name]; ok {
		return nil, callRefused(op.Service + " " + op.Command + " " + why + ", which Mewla does not run")
	}
	shapes, err := idx.loadShapes(idx.services[op.Service])
	if err != nil {
		return nil, errors.New("could not read the AWS service model")
	}
	if shapes.streams(op.input) || shapes.streams(op.output) {
		return nil, callRefused(fmt.Sprintf("%s %s streams data to or from a local file, which Mewla does not run", op.Service, op.Command))
	}
	if tool == "read" && awsKind(op.Name, params) != "read" {
		return nil, callRefused(op.Name + " changes state or may return a secret, so read refuses it; execute runs it, with the person's authorization for this task")
	}
	out, err := m.awsRun(ctx, r, secret, op.Service, op.Command, params, region, query)
	if err != nil {
		return nil, err
	}
	var result any
	if text := bytes.TrimSpace(out); len(text) > 0 {
		if err := json.Unmarshal(text, &result); err != nil {
			return nil, errors.New("AWS returned something that is not JSON; use query to return JSON")
		}
	}
	reply := map[string]any{"account": strings.TrimPrefix(r.Account.Name, "AWS "), "service": op.Service, "operation": op.Name, "result": awsRedact(result)}
	if region != "" {
		reply["region"] = region
	} else if r.Account.Region != "" {
		reply["region"] = r.Account.Region
	}
	return json.Marshal(reply)
}

// ---- redaction ----------------------------------------------------------

var awsSecretFields = map[string]bool{
	"secretaccesskey": true, "sessiontoken": true, "secretstring": true, "secretbinary": true, "password": true,
	"masteruserpassword": true, "privatekey": true, "plaintext": true, "userdata": true, "keymaterial": true,
	"authorizationtoken": true, "accesstoken": true, "refreshtoken": true, "idtoken": true, "secretkey": true,
}

const awsRedacted = "[REDACTED]"

// awsRedact hides secret values in a result: the fields AWS uses for them
// anywhere in the tree, and the values of environment variables whose names
// look like secrets (Lambda's Variables, container definitions' environment).
func awsRedact(v any) any {
	switch v := v.(type) {
	case []any:
		for i := range v {
			v[i] = awsRedact(v[i])
		}
	case map[string]any:
		for k, e := range v {
			lower := strings.ToLower(k)
			switch {
			case awsSecretFields[lower]:
				if _, isString := e.(string); isString || e != nil {
					v[k] = awsRedacted
				}
			case lower == "variables":
				if vars, ok := e.(map[string]any); ok {
					for name := range vars {
						if awsSecretKey.MatchString(name) {
							vars[name] = awsRedacted
						}
					}
				}
				v[k] = awsRedact(v[k])
			case lower == "environment":
				if list, ok := e.([]any); ok {
					for _, item := range list {
						if kv, ok := item.(map[string]any); ok {
							name, _ := kv["name"].(string)
							if name == "" {
								name, _ = kv["Name"].(string)
							}
							if awsSecretKey.MatchString(name) {
								for _, field := range []string{"value", "Value"} {
									if _, has := kv[field]; has {
										kv[field] = awsRedacted
									}
								}
							}
						}
					}
				}
				v[k] = awsRedact(v[k])
			default:
				v[k] = awsRedact(e)
			}
		}
	}
	return v
}

// ---- manager wiring -----------------------------------------------------

type awsState struct {
	awsExec     awsExec
	awsModelDir func() (string, error)
	awsOnce     sync.Once
	awsIdx      *awsIndex
	awsIdxErr   error
}

// awsProfilesResponse lists the profiles of this computer's AWS CLI.
func (m *Manager) awsProfilesResponse() (Response, error) {
	if _, err := exec.LookPath("aws"); err != nil {
		return Response{}, errAWSMissing
	}
	return Response{Profiles: awsProfiles()}, nil
}
