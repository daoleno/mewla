package connections

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// ---- fixtures -----------------------------------------------------------

type awsFixtureOp struct {
	name, doc string
	input     string
	output    string
}

// writeAWSModels writes small service models in botocore's layout.
func writeAWSModels(t *testing.T, gz bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	str := map[string]any{"type": "string"}
	member := func(shape string) map[string]any {
		return map[string]any{"shape": shape, "documentation": "<p>The " + shape + ".</p>"}
	}
	services := map[string]struct {
		full string
		ops  []awsFixtureOp
		shp  map[string]any
	}{
		"sts": {"AWS Security Token Service", []awsFixtureOp{{"GetCallerIdentity", "<p>Returns details about the IAM identity whose credentials are used to call the operation.</p>", "Empty", "Identity"}, {"GetSessionToken", "<p>Returns temporary credentials.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}, "Identity": map[string]any{"type": "structure", "members": map[string]any{"Account": member("String"), "Arn": member("String")}}, "String": str}},
		"rds": {"Amazon Relational Database Service", []awsFixtureOp{
			{"DescribeDBInstances", "<p>Describes provisioned RDS instances. This API supports pagination.</p>", "DescribeDBInstancesMessage", "DBInstanceMessage"},
			{"DeleteDBInstance", "<p>Deletes a previously provisioned DB instance.</p>", "DeleteDBInstanceMessage", "DBInstanceMessage"},
			{"ModifyDBInstance", "<p>Modifies settings for a DB instance.</p>", "DeleteDBInstanceMessage", "DBInstanceMessage"}},
			map[string]any{"String": str, "Integer": map[string]any{"type": "integer"},
				"Filter":                     map[string]any{"type": "structure", "members": map[string]any{"Name": member("String"), "Values": member("Values")}, "required": []string{"Name", "Values"}},
				"Values":                     map[string]any{"type": "list", "member": map[string]any{"shape": "String"}},
				"FilterList":                 map[string]any{"type": "list", "member": map[string]any{"shape": "Filter"}},
				"DescribeDBInstancesMessage": map[string]any{"type": "structure", "members": map[string]any{"DBInstanceIdentifier": member("String"), "Filters": member("FilterList"), "MaxRecords": member("Integer")}},
				"DeleteDBInstanceMessage":    map[string]any{"type": "structure", "required": []string{"DBInstanceIdentifier"}, "members": map[string]any{"DBInstanceIdentifier": member("String")}},
				"DBInstanceMessage":          map[string]any{"type": "structure", "members": map[string]any{"Marker": member("String")}}}},
		"ec2": {"Amazon Elastic Compute Cloud", []awsFixtureOp{
			{"DescribeInstances", "<p>Describes the specified instances or all instances.</p>", "Empty", "Empty"},
			{"RunInstances", "<p>Launches the specified number of instances.</p>", "Empty", "Empty"},
			{"DescribeVpcs", "<p>Describes your VPCs.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}}},
		"s3": {"Amazon Simple Storage Service", []awsFixtureOp{
			{"GetObject", "<p>Retrieves an object.</p>", "Empty", "GetObjectOutput"},
			{"ListBuckets", "<p>Returns a list of buckets.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}, "Body": map[string]any{"type": "blob", "streaming": true},
				"GetObjectOutput": map[string]any{"type": "structure", "members": map[string]any{"Body": map[string]any{"shape": "Body"}}}}},
		"lambda": {"AWS Lambda", []awsFixtureOp{{"Invoke", "<p>Invokes a function.</p>", "Empty", "Empty"}, {"GetFunctionConfiguration", "<p>Returns configuration.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}}},
		"ssm": {"AWS Systems Manager", []awsFixtureOp{{"GetParameter", "<p>Gets a parameter.</p>", "Empty", "Empty"}, {"StartSession", "<p>Initiates a connection to a target.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}}},
		"ecs": {"Amazon Elastic Container Service", []awsFixtureOp{{"ExecuteCommand", "<p>Runs a command in a container.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}}},
		"secretsmanager": {"AWS Secrets Manager", []awsFixtureOp{{"GetSecretValue", "<p>Retrieves a secret.</p>", "Empty", "Empty"}, {"ListSecrets", "<p>Lists secrets.</p>", "Empty", "Empty"}},
			map[string]any{"Empty": map[string]any{"type": "structure", "members": map[string]any{}}}},
	}
	for name, s := range services {
		ops := map[string]any{}
		for _, o := range s.ops {
			ops[o.name] = map[string]any{"name": o.name, "documentation": o.doc, "input": map[string]any{"shape": o.input}, "output": map[string]any{"shape": o.output}}
		}
		doc := map[string]any{"metadata": map[string]any{"serviceFullName": s.full, "serviceId": name}, "operations": ops, "shapes": s.shp}
		raw, _ := json.Marshal(doc)
		version := filepath.Join(dir, name, "2020-01-01")
		if err := os.MkdirAll(version, 0o755); err != nil {
			t.Fatal(err)
		}
		if gz {
			f, _ := os.Create(filepath.Join(version, "service-2.json.gz"))
			zw := gzip.NewWriter(f)
			zw.Write(raw)
			zw.Close()
			f.Close()
		} else if err := os.WriteFile(filepath.Join(version, "service-2.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type awsFake struct {
	mu    sync.Mutex
	calls []awsCommand
	reply func(c awsCommand) ([]byte, error)
}

func (f *awsFake) run(_ context.Context, c awsCommand) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	return f.reply(c)
}

func (f *awsFake) last() awsCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

const awsWho = `{"UserId":"AIDAEXAMPLE","Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/audit"}`

// awsTestManager has a fake CLI and fixture models, and an AWS config with
// profiles of each kind in a private HOME.
func awsTestManager(t *testing.T, reply func(c awsCommand) ([]byte, error)) (*Manager, *awsFake) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(name, "")
	}
	if err := os.MkdirAll(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := `[default]
region = us-east-1

[profile work]
login_session = arn:aws:iam::123456789012:user/audit
region = ap-southeast-1

[profile work-sso]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = Audit
region = eu-west-1

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = eu-west-1

[profile deploy]
role_arn = arn:aws:iam::123456789012:role/deploy
source_profile = default

[profile static]
aws_access_key_id = AKIAEXAMPLEEXAMPLE1
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
s3 =
    max_concurrent_requests = 5
`
	if err := os.WriteFile(filepath.Join(home, ".aws", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &awsFake{reply: reply}
	m, err := newManager(filepath.Join(t.TempDir(), "catalog.json"), modelprofiles.NewMemoryCredentialStore(), &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	models := writeAWSModels(t, false)
	m.awsExec = fake.run
	m.awsModelDir = func() (string, error) { return models, nil }
	return m, fake
}

func awsOK(c awsCommand) ([]byte, error) {
	if len(c.Args) >= 2 && c.Args[0] == "sts" {
		return []byte(awsWho), nil
	}
	return []byte(`{"DBInstances":[{"DBInstanceIdentifier":"main"}]}`), nil
}

func addAWSProfile(t *testing.T, m *Manager, profile string) *Account {
	t.Helper()
	return mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "aws", Profile: profile}}).Account
}

func invokeAWS(m *Manager, a *Account, tool string, args string) (Response, error) {
	return m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: tool, Arguments: json.RawMessage(args)})
}

func awsArgsHave(c awsCommand, want ...string) bool {
	have := map[string]bool{}
	for _, a := range c.Args {
		have[a] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

// ---- names and classes --------------------------------------------------

func TestAWSCommandNames(t *testing.T) {
	for api, want := range map[string]string{
		"DescribeDBInstances":                      "describe-db-instances",
		"DescribeInstances":                        "describe-instances",
		"ListBuckets":                              "list-buckets",
		"GetCallerIdentity":                        "get-caller-identity",
		"GetOTelEnrichment":                        "get-otel-enrichment",
		"CreateOAuth2Token":                        "create-oauth2-token",
		"SendWhatsAppMessage":                      "send-whatsapp-message",
		"DescribeCachediSCSIVolumes":               "describe-cached-iscsi-volumes",
		"ListHITsForQualificationType":             "list-hi-ts-for-qualification-type",
		"AssumeRoleWithSAML":                       "assume-role-with-saml",
		"DescribeVpcEndpointServiceConfigurations": "describe-vpc-endpoint-service-configurations",
		"GetMetricData":                            "get-metric-data",
		"DescribeIpv6Pools":                        "describe-ipv6-pools",
	} {
		if got := awsCommandName(api); got != want {
			t.Errorf("%s: got %s, want %s", api, got, want)
		}
	}
}

func TestAWSKind(t *testing.T) {
	for name, want := range map[string]string{
		"DescribeInstances": "read", "ListBuckets": "read", "GetMetricData": "read", "BatchGetItem": "read", "HeadBucket": "read",
		"LookupEvents": "read", "SearchTransitGatewayRoutes": "read", "GetAccountPasswordPolicy": "read", "GetChangeToken": "read",
		"RunInstances": "write", "DeleteDBInstance": "write", "PutObject": "write", "StartQuery": "write", "TerminateInstances": "write",
		"Invoke": "write", "SendMessage": "write", "Listen": "write", "Getaway": "write", "Describer": "write",
		"GetSecretValue": "write", "BatchGetSecretValue": "write", "GetSessionToken": "write", "GetFederationToken": "write",
		"GetAuthorizationToken": "write", "GetPasswordData": "write", "GetRoleCredentials": "write", "GetClusterCredentials": "write",
		"GetCredentialsForIdentity": "write", "GetOpenIdToken": "write", "GetDownloadUrlForLayer": "write",
	} {
		if got := awsKind(name, nil); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
	if awsKind("GetParameter", nil) != "read" || awsKind("GetParameter", map[string]any{"WithDecryption": false}) != "read" {
		t.Error("an undecrypted parameter is a read")
	}
	for _, op := range []string{"GetParameter", "GetParameters", "GetParametersByPath"} {
		if awsKind(op, map[string]any{"WithDecryption": true}) != "write" {
			t.Errorf("%s with decryption returns secrets", op)
		}
	}
}

func TestAWSRedact(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"Credentials":{"AccessKeyId":"ASIA1","SecretAccessKey":"s3cret","SessionToken":"tok"},
		"Function":{"Environment":{"Variables":{"DB_PASSWORD":"hunter2","REGION":"us-east-1"}}},
		"TaskDefinition":{"containerDefinitions":[{"environment":[{"name":"API_KEY","value":"abc"},{"name":"MODE","value":"prod"}]}]},
		"SecretString":"x","Instances":[{"UserData":{"Value":"IyEvYmluL3No"},"Id":"i-1"}]}`), &v)
	out, _ := json.Marshal(awsRedact(v))
	text := string(out)
	for _, leaked := range []string{"s3cret", "tok\"", "hunter2", "abc", "IyEvYmluL3No", "\"x\""} {
		if strings.Contains(text, leaked) {
			t.Errorf("%q leaked: %s", leaked, text)
		}
	}
	for _, kept := range []string{"ASIA1", "us-east-1", "prod", "i-1"} {
		if !strings.Contains(text, kept) {
			t.Errorf("%q was removed: %s", kept, text)
		}
	}
}

// ---- profiles and models ------------------------------------------------

func TestAWSProfiles(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	fakeAWS(t, "true")
	r := mustHandle(t, m, Request{Action: "aws_profiles"})
	kinds := map[string]string{}
	regions := map[string]string{}
	for _, p := range r.Profiles {
		kinds[p.Name], regions[p.Name] = p.Kind, p.Region
	}
	want := map[string]string{"default": "other", "work": "login", "work-sso": "sso", "deploy": "role", "static": "keys"}
	if len(kinds) != len(want) {
		t.Fatalf("profiles: %v", kinds)
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Errorf("%s: %s, want %s", name, kinds[name], kind)
		}
	}
	if regions["work"] != "ap-southeast-1" {
		t.Errorf("region: %v", regions)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "wJalr") || strings.Contains(string(raw), "AKIAEXAMPLE") {
		t.Fatal("a profile list carries a key")
	}
}

func TestAWSModelLocations(t *testing.T) {
	models := writeAWSModels(t, true)
	root := filepath.Dir(filepath.Dir(models))
	// Move the models into each known installer layout and find them again.
	for name, rel := range map[string][]string{
		"bundle":   {"dist", "awscli", "botocore", "data"},
		"homebrew": {"libexec", "lib", "python3.13", "site-packages", "awscli", "botocore", "data"},
		"pip":      {"lib", "python3.12", "site-packages", "botocore", "data"},
	} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			dest := filepath.Join(append([]string{base}, rel...)...)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(models, dest); err != nil {
				// Copy instead when the temp dirs sit on different file systems.
				t.Skip("cannot move fixture: ", err)
			}
			defer os.Rename(dest, models)
			found := ""
			for _, c := range awsModelDirs(base) {
				if _, err := os.Stat(filepath.Join(c, "sts")); err == nil {
					found = c
				}
			}
			if found != dest {
				t.Fatalf("found %q, want %q", found, dest)
			}
			idx, err := loadAWSIndex(found)
			if err != nil || idx.services["rds"] == nil {
				t.Fatalf("gzipped models: %v", err)
			}
		})
	}
	_ = root
}

func TestAWSIndexAliasesAndLookup(t *testing.T) {
	idx, err := loadAWSIndex(writeAWSModels(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if idx.services["s3api"] == nil || idx.services["s3"] != nil {
		t.Fatal("the s3 model is the CLI's s3api")
	}
	for _, spelling := range []string{"DescribeDBInstances", "describe-db-instances", "describedbinstances"} {
		op, err := idx.operation("rds", spelling)
		if err != nil || op.Command != "describe-db-instances" {
			t.Fatalf("%s: %v", spelling, err)
		}
	}
	if op, err := idx.operation("s3", "ListBuckets"); err != nil || op.Service != "s3api" {
		t.Fatalf("the s3 alias: %v", err)
	}
	if _, err := idx.operation("rds", "NoSuchThing"); err == nil {
		t.Fatal("an unknown operation")
	}
	if _, err := idx.operation("--profile=x", "ListBuckets"); err == nil {
		t.Fatal("an option as a service")
	}
}

func TestAWSSearch(t *testing.T) {
	idx, _ := loadAWSIndex(writeAWSModels(t, false))
	matches, err := idx.search("rds instances", "", 10)
	if err != nil || len(matches) == 0 || matches[0].Operation != "DescribeDBInstances" {
		t.Fatalf("%v %+v", err, matches)
	}
	if matches[0].Kind != "read" || matches[0].Command != "aws rds describe-db-instances" {
		t.Fatalf("%+v", matches[0])
	}
	all, _ := idx.search("instance", "", 10)
	kinds := map[string]string{}
	for _, m := range all {
		kinds[m.Operation] = m.Kind
	}
	if kinds["RunInstances"] != "write" || kinds["DescribeInstances"] != "read" {
		t.Fatalf("%v", kinds)
	}
	if only, _ := idx.search("instance", "ec2", 10); len(only) != 2 {
		t.Fatalf("one service: %+v", only)
	}
	if _, err := idx.search("  !! ", "", 10); err == nil {
		t.Fatal("an empty query")
	}
}

// ---- connecting ---------------------------------------------------------

func TestAWSConnectProfile(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "inherited-secret")
	t.Setenv("AWS_PROFILE", "inherited-profile")
	m, fake := awsTestManager(t, awsOK)
	t.Setenv("AWS_SECRET_ACCESS_KEY", "inherited-secret")
	t.Setenv("AWS_PROFILE", "inherited-profile")
	a := addAWSProfile(t, m, "work")
	if a.Name != "AWS 123456789012" || a.Identity != "arn:aws:iam::123456789012:user/audit" || a.Status != "connected" {
		t.Fatalf("%+v", a)
	}
	if a.Profile != "work" || a.Region != "ap-southeast-1" || a.AuthMethod != "aws_profile" {
		t.Fatalf("%+v", a)
	}
	c := fake.last()
	if !awsArgsHave(c, "sts", "get-caller-identity", "--profile=work", "--output=json", "--no-cli-pager", "--no-cli-auto-prompt", "--no-paginate") {
		t.Fatalf("argv: %v", c.Args)
	}
	for _, e := range c.Env {
		if strings.Contains(e, "inherited") {
			t.Fatalf("an inherited setting reached the CLI: %s", e)
		}
	}
	if a.Access.Read != "allowed" || a.Access.Write != "off" {
		t.Fatalf("read is on, execute is off: %+v", a.Access)
	}
	granted := map[string]bool{}
	for _, tool := range a.Tools {
		granted[tool.Name] = tool.Allowed
	}
	if !granted["whoami"] || !granted["search"] || !granted["docs"] || !granted["read"] || granted["execute"] {
		t.Fatalf("grants: %v", granted)
	}
	// Connecting the same profile again renews the account.
	again := addAWSProfile(t, m, "work")
	if again.ID != a.ID || len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatal("the same profile twice is one account")
	}
	// A second profile for the same AWS account is its own row, named apart.
	other := addAWSProfile(t, m, "work-sso")
	if other.ID == a.ID || other.Name != "AWS 123456789012 (work-sso)" {
		t.Fatalf("%+v", other)
	}
	// The profile list is the config; a missing or malformed name is refused.
	for _, bad := range []string{"nope", "--debug", "a b", "../x"} {
		if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Profile: bad}}); err == nil {
			t.Errorf("profile %q was accepted", bad)
		}
	}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws"}}); err == nil {
		t.Error("neither a profile nor keys")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Profile: "work", Credential: "{}"}}); err == nil {
		t.Error("both a profile and keys")
	}
}

func TestAWSConnectKeys(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	const secret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	creds := `{"access_key_id":"AKIAEXAMPLEEXAMPLE1","secret_access_key":"` + secret + `","session_token":"FwoGZXIvYXdzEXAMPLETOKEN"}`
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "aws", Credential: creds, Region: "eu-west-1"}}).Account
	if a.AuthMethod != "aws_keys" || a.Profile != "" || a.Region != "eu-west-1" || a.Name != "AWS 123456789012" {
		t.Fatalf("%+v", a)
	}
	env := strings.Join(fake.last().Env, "\n")
	for _, want := range []string{"AWS_ACCESS_KEY_ID=AKIAEXAMPLEEXAMPLE1", "AWS_SECRET_ACCESS_KEY=" + secret, "AWS_SESSION_TOKEN=FwoGZXIvYXdzEXAMPLETOKEN", "AWS_DEFAULT_REGION=eu-west-1", "AWS_CONFIG_FILE=" + os.DevNull, "AWS_SHARED_CREDENTIALS_FILE=" + os.DevNull} {
		if !strings.Contains(env, want) {
			t.Errorf("env is missing %s", want)
		}
	}
	if strings.Contains(strings.Join(fake.last().Args, " "), "--profile") {
		t.Error("keys never name a profile")
	}
	// The keys are kept in the vault only, and never in the catalog.
	raw, _ := os.ReadFile(m.path)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "AKIAEXAMPLE") {
		t.Fatal("a key reached the account catalog")
	}
	if saved, ok, _ := m.vault.Get("integration:" + a.ID); !ok || !strings.Contains(saved, secret) {
		t.Fatal("the keys are not in the vault")
	}
	// `aws configure export-credentials --format process` output works too.
	cli := `{"Version":1,"AccessKeyId":"ASIAEXAMPLEEXAMPLE2","SecretAccessKey":"` + secret + `","SessionToken":"tok-tok-tok-tok-tok"}`
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Credential: cli}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "AKIA", `{"access_key_id":"nope","secret_access_key":"` + secret + `"}`, `{"access_key_id":"AKIAEXAMPLEEXAMPLE1","secret_access_key":"short"}`} {
		_, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Credential: bad}})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// The same identity with new keys renews the account in place.
	if n := len(mustHandle(t, m, Request{Action: "list"}).Accounts); n != 1 {
		t.Fatalf("new keys for the same identity made %d accounts", n)
	}
	// A result that echoes the keys is redacted.
	fake.reply = func(c awsCommand) ([]byte, error) {
		return []byte(`{"Echo":"` + secret + `","Other":"ASIAEXAMPLEEXAMPLE2","Token":"tok-tok-tok-tok-tok"}`), nil
	}
	got, err := invokeAWS(m, a, "read", `{"service":"rds","operation":"DescribeDBInstances"}`)
	if err != nil || strings.Contains(string(got.Result), secret) || strings.Contains(string(got.Result), "ASIAEXAMPLE") || strings.Contains(string(got.Result), "tok-tok") {
		t.Fatalf("%v %s", err, got.Result)
	}
}

func TestAWSKeysWithoutRegionDefaultToUSEast1(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	creds := `{"access_key_id":"AKIAEXAMPLEEXAMPLE1","secret_access_key":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "aws", Credential: creds}}).Account
	if a.Region != "us-east-1" || !strings.Contains(strings.Join(fake.last().Env, "\n"), "AWS_DEFAULT_REGION=us-east-1") {
		t.Fatalf("%+v", a)
	}
}

// ---- calls --------------------------------------------------------------

func TestAWSReadCall(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	a := addAWSProfile(t, m, "work")
	got, err := invokeAWS(m, a, "read", `{"service":"rds","operation":"describe-db-instances","params":{"DBInstanceIdentifier":"main"},"region":"us-west-2","query":"DBInstances[].DBInstanceIdentifier"}`)
	if err != nil {
		t.Fatal(err)
	}
	c := fake.last()
	want := []string{"rds", "describe-db-instances", "--output=json", "--no-cli-pager", "--no-cli-auto-prompt", "--no-paginate", "--profile=work", "--region=us-west-2", "--query=DBInstances[].DBInstanceIdentifier"}
	if strings.Join(c.Args, "|") != strings.Join(want, "|") {
		t.Fatalf("argv:\n%v\nwant\n%v", c.Args, want)
	}
	if string(c.Input) != `{"DBInstanceIdentifier":"main"}` {
		t.Fatalf("params: %s", c.Input)
	}
	var reply struct {
		Account, Service, Operation, Region string
		Result                              struct{ DBInstances []map[string]string }
	}
	if json.Unmarshal(got.Result, &reply) != nil || reply.Account != "123456789012" || reply.Service != "rds" || reply.Operation != "DescribeDBInstances" || reply.Region != "us-west-2" || reply.Result.DBInstances[0]["DBInstanceIdentifier"] != "main" {
		t.Fatalf("%s", got.Result)
	}
	// Without a region of its own the call reports the profile's.
	got, _ = invokeAWS(m, a, "read", `{"service":"rds","operation":"DescribeDBInstances"}`)
	if !strings.Contains(string(got.Result), `"region":"ap-southeast-1"`) {
		t.Fatalf("%s", got.Result)
	}
	if awsArgsHave(fake.last(), "--region=ap-southeast-1") {
		t.Fatal("the profile's own region is left to the CLI")
	}
}

func TestAWSRefusals(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	a := addAWSProfile(t, m, "work")
	before := len(fake.calls)
	for name, c := range map[string]struct{ tool, args, contains string }{
		"a change through read":         {"read", `{"service":"rds","operation":"DeleteDBInstance","params":{"DBInstanceIdentifier":"main"}}`, "execute"},
		"a launch through read":         {"read", `{"service":"ec2","operation":"RunInstances"}`, "execute"},
		"a secret through read":         {"read", `{"service":"secretsmanager","operation":"GetSecretValue","params":{"SecretId":"x"}}`, "execute"},
		"temporary keys via read":       {"read", `{"service":"sts","operation":"GetSessionToken"}`, "execute"},
		"a decrypted parameter":         {"read", `{"service":"ssm","operation":"GetParameter","params":{"Name":"/db","WithDecryption":true}}`, "execute"},
		"a change, not allowed":         {"execute", `{"service":"rds","operation":"DeleteDBInstance","params":{"DBInstanceIdentifier":"main"}}`, "not authorized"},
		"an unknown service":            {"read", `{"service":"nosuch","operation":"ListThings"}`, "unknown AWS service"},
		"an unknown operation":          {"read", `{"service":"rds","operation":"DescribeNothing"}`, "no operation"},
		"an option as a service":        {"read", `{"service":"--profile=other","operation":"ListBuckets"}`, "do not match"},
		"a file parameter":              {"read", `{"service":"rds","operation":"DescribeDBInstances","params":{"Filters":[{"Name":"x","Values":["file:///etc/passwd"]}]}}`, "read a file"},
		"a binary file parameter":       {"read", `{"service":"rds","operation":"DescribeDBInstances","params":{"DBInstanceIdentifier":"fileb://x"}}`, "read a file"},
		"a streamed download":           {"read", `{"service":"s3","operation":"GetObject","params":{"Bucket":"b","Key":"k"}}`, "local file"},
		"a lambda invoke":               {"read", `{"service":"lambda","operation":"Invoke","params":{"FunctionName":"f"}}`, "local file"},
		"bad region":                    {"read", `{"service":"rds","operation":"DescribeDBInstances","region":"us-east-1 --debug"}`, "region must look like"},
		"params that are not an object": {"read", `{"service":"rds","operation":"DescribeDBInstances","params":[1]}`, "do not match"},
	} {
		if _, err := invokeAWS(m, a, c.tool, c.args); err == nil || !strings.Contains(err.Error(), c.contains) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(fake.calls) != before {
		t.Fatalf("a refused call reached the CLI: %v", fake.calls[before:])
	}
}

// Even with execute allowed, streamed and file-reading operations stay refused.
func TestAWSExecuteWhenAllowed(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	a := addAWSProfile(t, m, "work")
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "write", Allowed: true})
	if _, err := invokeAWS(m, a, "execute", `{"service":"rds","operation":"DeleteDBInstance","params":{"DBInstanceIdentifier":"main"}}`); err != nil {
		t.Fatal(err)
	}
	if !awsArgsHave(fake.last(), "delete-db-instance") {
		t.Fatalf("%v", fake.last().Args)
	}
	for _, args := range []string{
		`{"service":"s3","operation":"GetObject"}`,
		`{"service":"lambda","operation":"Invoke"}`,
		`{"service":"ssm","operation":"StartSession","params":{"Target":"i-1"}}`,
		`{"service":"ecs","operation":"ExecuteCommand","params":{"command":"sh","interactive":true,"task":"t"}}`,
		`{"service":"rds","operation":"ModifyDBInstance","params":{"DBInstanceIdentifier":"file://etc/hosts"}}`,
	} {
		n := len(fake.calls)
		if _, err := invokeAWS(m, a, "execute", args); err == nil || len(fake.calls) != n {
			t.Errorf("%s: %v", args, err)
		}
	}
	if _, err := invokeAWS(m, a, "execute", `{"service":"ssm","operation":"StartSession"}`); err == nil || !strings.Contains(err.Error(), "Session Manager") {
		t.Errorf("an interactive session: %v", err)
	}
	// A reads-only operation also runs through execute.
	if _, err := invokeAWS(m, a, "execute", `{"service":"rds","operation":"DescribeDBInstances"}`); err != nil {
		t.Fatal(err)
	}
}

func TestAWSSearchAndDocsTools(t *testing.T) {
	m, fake := awsTestManager(t, awsOK)
	a := addAWSProfile(t, m, "work")
	n := len(fake.calls)
	got, err := invokeAWS(m, a, "search", `{"query":"delete rds instance"}`)
	if err != nil || !strings.Contains(string(got.Result), `"operation":"DeleteDBInstance"`) || !strings.Contains(string(got.Result), `"kind":"write"`) {
		t.Fatalf("%v %s", err, got.Result)
	}
	got, err = invokeAWS(m, a, "docs", `{"service":"rds","operation":"DescribeDBInstances"}`)
	if err != nil {
		t.Fatal(err)
	}
	var docs struct {
		Command, Kind, Description string
		RunWith                    string `json:"run_with"`
		Parameters                 []awsParam
	}
	json.Unmarshal(got.Result, &docs)
	if docs.Command != "aws rds describe-db-instances" || docs.Kind != "read" || docs.RunWith != "read" || !strings.Contains(docs.Description, "Describes provisioned RDS instances") {
		t.Fatalf("%s", got.Result)
	}
	types := map[string]string{}
	for _, p := range docs.Parameters {
		types[p.Name] = p.Type
	}
	if types["MaxRecords"] != "integer" || types["Filters"] != "list<structure{Name:string, Values:list<string>}>" {
		t.Fatalf("%v", types)
	}
	got, _ = invokeAWS(m, a, "docs", `{"service":"s3","operation":"GetObject"}`)
	if !strings.Contains(string(got.Result), "streams data") {
		t.Fatalf("%s", got.Result)
	}
	if len(fake.calls) != n {
		t.Fatal("search and docs read the local models and call nothing")
	}
}

func TestAWSWhoami(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	a := addAWSProfile(t, m, "work")
	got, err := invokeAWS(m, a, "whoami", `{}`)
	if err != nil || !strings.Contains(string(got.Result), `"account":"123456789012"`) || !strings.Contains(string(got.Result), `"profile":"work"`) {
		t.Fatalf("%v %s", err, got.Result)
	}
}

func TestAWSModelsMissing(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	m.awsModelDir = func() (string, error) { return "", errors.New("models not found") }
	a := addAWSProfile(t, m, "work")
	if _, err := invokeAWS(m, a, "search", `{"query":"rds"}`); err == nil || !strings.Contains(err.Error(), "models not found") {
		t.Fatalf("search without models: %v", err)
	}
	if _, err := invokeAWS(m, a, "read", `{"service":"rds","operation":"DescribeDBInstances"}`); err == nil {
		t.Fatal("a call needs the models to name it")
	}
	if _, err := invokeAWS(m, a, "whoami", `{}`); err != nil {
		t.Fatalf("whoami needs no models: %v", err)
	}
}

// ---- expiry -------------------------------------------------------------

func TestAWSSessionExpiry(t *testing.T) {
	expired := false
	var stderr string
	m, _ := awsTestManager(t, func(c awsCommand) ([]byte, error) {
		if expired {
			return nil, &awsExitError{stderr: stderr}
		}
		return awsOK(c)
	})
	login := addAWSProfile(t, m, "work")
	sso := addAWSProfile(t, m, "work-sso")
	for _, c := range []struct {
		account *Account
		stderr  string
		hint    string
	}{
		{login, "\naws: [ERROR]: Error loading login session token: Unable to load a existing login session for session arn:aws:iam::123456789012:user/audit, Please reauthenticate with 'aws login'.\n", "run `aws login --profile work`"},
		{sso, "\naws: [ERROR]: Error loading SSO Token: Token for corp does not exist\n", "run `aws sso login --profile work-sso`"},
		{login, "\naws: [ERROR]: An error occurred (ExpiredToken) when calling the GetCallerIdentity operation: The security token included in the request is expired\n", "run `aws login --profile work`"},
	} {
		expired, stderr = true, c.stderr
		_, err := invokeAWS(m, c.account, "whoami", `{}`)
		if err == nil || !strings.Contains(err.Error(), c.hint) || strings.Contains(err.Error(), "\n") {
			t.Fatalf("%q: %v", c.stderr, err)
		}
		if !errors.Is(err, errAuth) {
			t.Fatal("an expired session is an authorization error")
		}
		if got := mustHandle(t, m, Request{Action: "get", ID: c.account.ID}).Account; got.Status != "authorization_required" {
			t.Fatalf("status %s", got.Status)
		}
		// Check, after signing in again, brings the account back.
		expired = false
		if got := mustHandle(t, m, Request{Action: "refresh", ID: c.account.ID}).Account; got.Status != "connected" {
			t.Fatalf("after refresh: %s", got.Status)
		}
	}
	// Check while still signed out reads "Needs sign-in again", not a failure.
	expired, stderr = true, "aws: [ERROR]: Error loading SSO Token: Token for corp does not exist"
	if _, err := m.Handle(context.Background(), Request{Action: "refresh", ID: sso.ID}); err == nil {
		t.Fatal("refresh while signed out")
	}
	if got := mustHandle(t, m, Request{Action: "get", ID: sso.ID}).Account; got.Status != "authorization_required" {
		t.Fatalf("status %s", got.Status)
	}
	// Connecting a signed-out profile says what to run, and adds nothing.
	n := len(mustHandle(t, m, Request{Action: "list"}).Accounts)
	_, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Profile: "deploy"}})
	if err == nil || len(mustHandle(t, m, Request{Action: "list"}).Accounts) != n {
		t.Fatalf("%v", err)
	}
}

func TestAWSFailureText(t *testing.T) {
	r := &record{Account: Account{AuthMethod: awsMethodProfile, Profile: "p"}}
	k := &record{Account: Account{AuthMethod: awsMethodKeys}}
	cases := []struct {
		r        *record
		stderr   string
		contains string
		auth     bool
	}{
		{r, "aws: [ERROR]: An error occurred (AccessDenied) when calling the ListBuckets operation: Access Denied", "AWS reported an error: An error occurred (AccessDenied)", false},
		{r, "aws: [ERROR]: The config profile (p) could not be found", "no longer in this computer's AWS config", false},
		{r, "aws: [ERROR]: You must specify a region. You can also configure your region by running \"aws configure\".", "pass region", false},
		{k, "aws: [ERROR]: An error occurred (InvalidClientTokenId) when calling the GetCallerIdentity operation: The security token included in the request is invalid.", "add new keys", true},
		{r, "aws: [ERROR]: An error occurred (NoCredentials): Unable to locate credentials.", "run `aws login --profile p`", true},
		{r, "", "without a message", false},
	}
	for _, c := range cases {
		err := awsFailure(c.r, c.stderr)
		if !strings.Contains(err.Error(), c.contains) || errors.Is(err, errAuth) != c.auth {
			t.Errorf("%q: %v (auth=%v)", c.stderr, err, errors.Is(err, errAuth))
		}
	}
}

// ---- the real exec boundary ---------------------------------------------

// fakeAWS puts an `aws` program first on PATH that records how it was run.
func fakeAWS(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\n{ echo \"ARGS:$*\"; env | sort; for a in \"$@\"; do case \"$a\" in --cli-input-json=file://*) f=${a#--cli-input-json=file://}; echo \"PARAMS:$(cat \"$f\")\"; echo \"MODE:$(stat -c %a \"$f\" 2>/dev/null || stat -f %Lp \"$f\")\"; echo \"FILE:$f\";; esac; done; } > " + log + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestRunAWSEnvironmentAndParams(t *testing.T) {
	log := fakeAWS(t, `echo '{"ok":true}'`)
	t.Setenv("AWS_SECRET_ACCESS_KEY", "inherited-secret")
	t.Setenv("AWS_PROFILE", "inherited-profile")
	t.Setenv("AWS_ENDPOINT_URL", "http://169.254.169.254")
	t.Setenv("AWS_CONFIG_FILE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := &record{Account: Account{AuthMethod: awsMethodProfile, Profile: "work"}}
	env, err := awsEnv(r, "", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runAWS(context.Background(), awsCommand{Args: []string{"rds", "describe-db-instances", "--profile=work"}, Env: env, Input: []byte(`{"Password":"p@ss"}`)})
	if err != nil || strings.TrimSpace(string(out)) != `{"ok":true}` {
		t.Fatalf("%v %s", err, out)
	}
	raw, _ := os.ReadFile(log)
	text := string(raw)
	if !strings.Contains(text, "ARGS:rds describe-db-instances --profile=work --cli-input-json=file://") {
		t.Fatalf("argv: %s", text)
	}
	if strings.Contains(text, "p@ss\n") && !strings.Contains(text, "PARAMS:{\"Password\":\"p@ss\"}") {
		t.Fatal("params")
	}
	if !strings.Contains(text, `PARAMS:{"Password":"p@ss"}`) || !strings.Contains(text, "MODE:600") {
		t.Fatalf("params travel in a private file: %s", text)
	}
	if strings.Contains(strings.SplitN(text, "PARAMS:", 2)[0], "p@ss") {
		t.Fatal("params appeared in argv")
	}
	for _, leaked := range []string{"inherited-secret", "inherited-profile", "AWS_ENDPOINT_URL", "169.254"} {
		if strings.Contains(text, leaked) {
			t.Errorf("%s reached the CLI's environment", leaked)
		}
	}
	for _, want := range []string{"AWS_PAGER=", "AWS_CLI_AUTO_PROMPT=off", "HOME=" + home} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "FILE:") {
			if _, err := os.Stat(strings.TrimPrefix(line, "FILE:")); err == nil {
				t.Fatal("the params file was left behind")
			}
		}
	}
}

func TestRunAWSKeysEnvironment(t *testing.T) {
	log := fakeAWS(t, `echo '{}'`)
	t.Setenv("HOME", t.TempDir())
	r := &record{Account: Account{AuthMethod: awsMethodKeys, Region: "eu-west-1"}}
	secret := `{"access_key_id":"AKIAEXAMPLEEXAMPLE1","secret_access_key":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`
	env, err := awsEnv(r, secret, "/private/home")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runAWS(context.Background(), awsCommand{Args: []string{"sts", "get-caller-identity"}, Env: env}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(log)
	text := string(raw)
	if strings.Contains(strings.SplitN(text, "\n", 2)[0], "AKIA") {
		t.Fatal("a key is in argv")
	}
	for _, want := range []string{"HOME=/private/home", "AWS_ACCESS_KEY_ID=AKIAEXAMPLEEXAMPLE1", "AWS_CONFIG_FILE=" + os.DevNull, "AWS_EC2_METADATA_DISABLED=true", "AWS_DEFAULT_REGION=eu-west-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestRunAWSFailureAndLimits(t *testing.T) {
	fakeAWS(t, `echo "aws: [ERROR]: Error loading SSO Token: Token for corp does not exist" >&2; exit 253`)
	_, err := runAWS(context.Background(), awsCommand{Args: []string{"sts", "get-caller-identity"}, Env: []string{"PATH=" + os.Getenv("PATH")}})
	var exit *awsExitError
	if !errors.As(err, &exit) || !strings.Contains(exit.stderr, "Error loading SSO Token") {
		t.Fatalf("%v", err)
	}
	fakeAWS(t, `head -c 1100000 /dev/zero | tr '\0' 'a'`)
	if _, err := runAWS(context.Background(), awsCommand{Args: []string{"ec2", "describe-instances"}, Env: []string{"PATH=" + os.Getenv("PATH")}}); !errors.Is(err, errAWSTooLarge) {
		t.Fatalf("a result over 1 MiB: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := runAWS(context.Background(), awsCommand{Args: []string{"sts"}}); !errors.Is(err, errAWSMissing) {
		t.Fatalf("no CLI: %v", err)
	}
}

func TestAWSProfilesNeedTheCLI(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	t.Setenv("PATH", t.TempDir())
	if _, err := m.Handle(context.Background(), Request{Action: "aws_profiles"}); !errors.Is(err, errAWSMissing) {
		t.Fatalf("%v", err)
	}
}

func TestAWSDisconnectDeletesKeys(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	creds := `{"access_key_id":"AKIAEXAMPLEEXAMPLE1","secret_access_key":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "aws", Credential: creds}}).Account
	mustHandle(t, m, Request{Action: "disconnect", ID: a.ID})
	if _, ok, _ := m.vault.Get("integration:" + a.ID); ok {
		t.Fatal("the keys outlive the account")
	}
	if _, err := invokeAWS(m, a, "whoami", `{}`); err == nil {
		t.Fatal("a disconnected account runs nothing")
	}
}

// A refused call and AWS's own error (AccessDenied) are not a fault of the
// account; only a call that cannot reach or sign in to AWS is.
func TestAWSAnswersDoNotMarkTheAccount(t *testing.T) {
	denied := false
	m, _ := awsTestManager(t, func(c awsCommand) ([]byte, error) {
		if denied {
			return nil, &awsExitError{stderr: "aws: [ERROR]: An error occurred (AccessDenied) when calling the DescribeDBInstances operation: not allowed"}
		}
		return awsOK(c)
	})
	a := addAWSProfile(t, m, "work")
	if _, err := invokeAWS(m, a, "read", `{"service":"rds","operation":"DeleteDBInstance"}`); err == nil {
		t.Fatal("a refusal")
	}
	denied = true
	if _, err := invokeAWS(m, a, "read", `{"service":"rds","operation":"DescribeDBInstances"}`); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("%v", err)
	}
	if got := mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account; got.Status != "connected" {
		t.Fatalf("status %s", got.Status)
	}
}
