package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AWS is reached through AWS's own MCP server, signed in with AWS Sign-In
// (OAuth with a loopback return, see oauth.go). Mewla keeps no AWS key: the
// vault holds the OAuth grant, and AWS refuses it after 12 hours.
//
// The server offers one tool that calls AWS, run_script, which runs whatever
// Python it is given. Mewla never hands it model-written code. Every call here
// is a fixed script that makes exactly one call_boto3 request, so Mewla knows
// which service and operation runs and classifies it by the operation's name
// (see awsKind); the person's IAM permissions remain the real boundary.
//
// Five tools: whoami asks STS who is signed in, search and docs read AWS's
// documentation, read runs an operation that only reads, and execute runs any
// operation, which is the one that needs the person's authorization.

const (
	awsMCPEndpoint   = "https://aws-mcp.us-east-1.api.aws/mcp"
	awsIssuer        = "https://us-east-1.oauth.signin.aws"
	awsDefaultRegion = "us-east-1"

	awsToolRunScript = "aws___run_script"
	awsToolSearch    = "aws___search_documentation"
	awsToolRead      = "aws___read_documentation"
	awsToolTasks     = "aws___get_tasks"

	awsCallTimeout = 90 * time.Second
	awsTaskPolls   = 12
)

var (
	awsRegionName    = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]+){1,3}$`)
	awsServiceName   = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	awsOperationName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,99}$`)
	awsSecretKey     = regexp.MustCompile(`(?i)secret|password|token|key|credential|private`)
	awsDocURL        = regexp.MustCompile(`^https://docs\.aws\.amazon\.com/[A-Za-z0-9_./#?=&%:+~-]{0,900}$`)
)

// awsServiceAliases are the AWS CLI's names for services whose SDK name differs.
var awsServiceAliases = map[string]string{"s3api": "s3", "cloudwatch-logs": "logs"}

// callRefused is a call Mewla did not send: it names something that does not
// exist, or asks for what a tool does not do. It says nothing about the account.
type callRefused string

func (e callRefused) Error() string { return string(e) }

// awsAuthError is a call that needs a new sign-in. It is an errAuth, so the
// account reads "Needs sign-in again", and its text says what to do.
type awsAuthError string

func (e awsAuthError) Error() string        { return string(e) }
func (e awsAuthError) Is(target error) bool { return target == errAuth }

var errAWSExpired = awsAuthError("AWS session expired. Sign in again.")

// ---- tools --------------------------------------------------------------

func awsText(desc string, max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max, "description": desc}
}

func awsCallProps() map[string]any {
	return map[string]any{
		"service":   map[string]any{"type": "string", "pattern": "^[a-z0-9-]+$", "minLength": 1, "maxLength": 64, "description": "The AWS SDK's service name, for example ec2, rds, logs, s3, iam"},
		"operation": awsText("The API operation in PascalCase, for example DescribeDBInstances", 100),
		"params":    map[string]any{"type": "object", "description": "The operation's parameters, with the API's own names as the docs list them, for example {\"DBInstanceIdentifier\":\"main\"}"},
		"region":    awsText("A region such as us-east-1, instead of the account's default", 32),
	}
}

func awsTools() []Tool {
	return []Tool{
		hlTool("whoami", "Show who is signed in to AWS: the account ID, the identity's ARN and the default region. Asks STS GetCallerIdentity.", true, map[string]any{}),
		hlTool("search", "Search AWS's documentation, for example to find the operation that does what you need and its parameters (try \"rds describe instances API\"). Returns the matching passages and their addresses; docs reads a whole page.", true, map[string]any{
			"query": awsText("Words to look for; keep exact error strings as they are", 200),
			"limit": hlInt(1, 10),
		}, "query"),
		hlTool("docs", "Read one AWS documentation page as text, from an address search returned. Use start_index to continue a long page.", true, map[string]any{
			"url":         awsText("A https://docs.aws.amazon.com address", 1000),
			"start_index": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000000, "description": "Where to continue reading, from a previous result's end_index"},
		}, "url"),
		hlTool("read", "Run an AWS operation that only reads (Describe*, List*, Get*, BatchGet*, Head*, Lookup*, Search*) with the account's sign-in. Results are collected across pages, up to 1000 items. Refuses anything that changes state or hands out a secret: those need execute. Secret values are redacted in results.", true, awsCallProps(), "service", "operation"),
		hlTool("execute", "Run any AWS operation, changes included (create, update, delete, start, stop, put, send, invoke), with the account's sign-in. Can create, modify and delete real resources and cost money: only on the user's explicit instruction for this task, and say what it will do first. Secret values are redacted in results. Never retried.", false, awsCallProps(), "service", "operation"),
	}
}

// ---- classification -----------------------------------------------------

var awsReadPrefixes = []string{"Describe", "List", "Get", "BatchGet", "Head", "Lookup", "Search"}

// awsSecretName marks operations that hand out credentials or secret values.
// They are named Get*Token, Get*Credentials and the like. They are changes as
// far as Mewla's grants go, so reading them needs the person's authorization
// for the task, and results are redacted regardless.
var awsSecretName = regexp.MustCompile(`Token|Password|Credentials|SecretValue|PrivateKey|DownloadUrl`)

// awsPlainReads are read operations that the secret pattern would catch.
var awsPlainReads = map[string]bool{
	"GetAccountPasswordPolicy": true, "GetTokenBalance": true, "BatchGetTokenBalance": true, "ListTokenBalances": true,
	"DescribeTrustedTokenIssuer": true, "ListTrustedTokenIssuers": true, "GetChangeToken": true, "GetChangeTokenStatus": true,
	"GetPersonalAccessTokenMetadata": true, "DescribeIpamExternalResourceVerificationTokens": true,
}

// awsKind says whether an operation is a read the person allows by default
// ("read") or a change, or a read that may expose a secret ("write").
func awsKind(operation string, params map[string]any) string {
	read := false
	for _, p := range awsReadPrefixes {
		if strings.HasPrefix(operation, p) {
			rest := operation[len(p):]
			if rest == "" || rest[0] >= 'A' && rest[0] <= 'Z' || rest[0] >= '0' && rest[0] <= '9' {
				read = true
				break
			}
		}
	}
	if !read {
		return "write"
	}
	if awsSecretName.MatchString(operation) && !awsPlainReads[operation] {
		return "write"
	}
	// Parameter Store returns secret strings in the clear when asked to decrypt.
	if strings.HasPrefix(operation, "GetParameter") || operation == "GetParameters" {
		if decrypt, _ := params["WithDecryption"].(bool); decrypt {
			return "write"
		}
	}
	return "read"
}

// awsOperation puts an operation name in the PascalCase the API uses. A
// hyphenated or snake_case name is capitalized word by word; the server
// corrects an acronym it can still recognize.
func awsOperation(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// ---- calls --------------------------------------------------------------

// awsScript is the whole script of one call. Every value reaches it as JSON
// text decoded at run time, never as source, so no argument can add code.
func awsScript(service, operation, region string, params map[string]any) (string, error) {
	raw, err := json.Marshal(map[string]any{"service": service, "operation": operation, "region": region, "params": params})
	if err != nil {
		return "", callRefused("params must be a JSON object")
	}
	var literal bytes.Buffer
	enc := json.NewEncoder(&literal)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(string(raw)); err != nil {
		return "", callRefused("params must be a JSON object")
	}
	return "call = json.loads(" + strings.TrimSpace(literal.String()) + ")\n" +
		"try:\n" +
		"    data = await call_boto3(service_name=call[\"service\"], operation_name=call[\"operation\"], region_name=call[\"region\"], params=call[\"params\"])\n" +
		"    result = {\"ok\": True, \"data\": data}\n" +
		"except Exception as e:\n" +
		"    result = {\"ok\": False, \"error\": str(e)[:1500]}\n" +
		"result\n", nil
}

// awsProbeScript reads who is signed in and the account's name in one round
// trip. Each part is separate: an organization's policy often denies the
// account's name, and that must not fail the sign-in.
const awsProbeScript = `async def one(service, operation):
    try:
        data = await call_boto3(service_name=service, operation_name=operation, region_name="us-east-1", params={})
        return {"ok": True, "data": data}
    except Exception as e:
        return {"ok": False, "error": str(e)[:600]}
result = {
    "identity": await one("sts", "GetCallerIdentity"),
    "account": await one("account", "GetAccountInformation"),
    "aliases": await one("iam", "ListAccountAliases"),
}
result
`

// awsScriptResult is what run_script answers.
type awsScriptResult struct {
	Status      string          `json:"status"`
	Error       string          `json:"error"`
	Stderr      string          `json:"stderr"`
	ReturnValue json.RawMessage `json:"return_value"`
	TaskID      string          `json:"task_id"`
	WaitSeconds float64         `json:"recommended_wait_seconds"`
}

// awsMCP runs one tool of AWS's MCP server in a session of its own.
func (m *Manager) awsMCP(ctx context.Context, r *record, secret, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	if r.OAuth == nil || secret == "" {
		return nil, errAWSExpired
	}
	s, err := m.mcpSession(ctx, r, secret, nil)
	if err != nil {
		if isTransportError(err, errAuth) {
			return nil, errAWSExpired
		}
		return nil, err
	}
	defer s.Close()
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		if ctx.Err() != nil {
			return nil, timeoutError{service: "AWS", after: awsCallTimeout}
		}
		return nil, mcpCallError(r, err)
	}
	if result.IsError {
		return nil, awsFailure(resultText(result))
	}
	return result, nil
}

// awsRunScript runs a script and waits for it, polling the task AWS opens for
// a slow one. It returns the script's own result.
func (m *Manager) awsRunScript(ctx context.Context, r *record, secret, code string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, awsCallTimeout)
	defer cancel()
	result, err := m.awsMCP(ctx, r, secret, awsToolRunScript, map[string]any{"code": code})
	if err != nil {
		return nil, err
	}
	for poll := 1; ; poll++ {
		var out awsScriptResult
		if err := decodeAWSResult(result, &out); err != nil {
			return nil, err
		}
		switch out.Status {
		case "success":
			if len(out.ReturnValue) == 0 || string(out.ReturnValue) == "null" {
				return nil, awsFailure(firstNonEmpty(out.Stderr, "the script returned nothing"))
			}
			return out.ReturnValue, nil
		case "working", "pending", "running":
			if out.TaskID == "" || poll > awsTaskPolls {
				return nil, serviceError{"AWS", "the operation is still running; check its state before calling it again"}
			}
			wait := time.Duration(out.WaitSeconds * float64(time.Second))
			if wait < time.Second {
				wait = time.Second
			} else if wait > 10*time.Second {
				wait = 10 * time.Second
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, timeoutError{service: "AWS", after: awsCallTimeout}
			}
			result, err = m.awsMCP(ctx, r, secret, awsToolTasks, map[string]any{"task_ids": []string{out.TaskID}, "poll_iteration": poll})
			if err != nil {
				return nil, err
			}
		default:
			return nil, awsFailure(firstNonEmpty(out.Error, out.Stderr, "the script failed"))
		}
	}
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return ""
}

// decodeAWSResult reads a tool result: the structured content when the server
// sends it, else the JSON in its text.
func decodeAWSResult(result *mcp.CallToolResult, out any) error {
	var raw []byte
	if result.StructuredContent != nil {
		raw, _ = json.Marshal(result.StructuredContent)
	} else {
		raw = []byte(resultText(result))
	}
	if json.Unmarshal(raw, out) != nil {
		return serviceError{"AWS", "answered with something that is not JSON"}
	}
	return nil
}

// awsCallResult is the answer of one fixed call_boto3 script.
type awsCallResult struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

// awsCall runs one operation of the account and returns its decoded result.
func (m *Manager) awsCall(ctx context.Context, r *record, secret, service, operation, region string, params map[string]any) (any, error) {
	code, err := awsScript(service, operation, region, params)
	if err != nil {
		return nil, err
	}
	raw, err := m.awsRunScript(ctx, r, secret, code)
	if err != nil {
		return nil, err
	}
	var out awsCallResult
	if json.Unmarshal(raw, &out) != nil {
		return nil, serviceError{"AWS", "answered with something Mewla could not read"}
	}
	if !out.OK {
		return nil, awsFailure(out.Error)
	}
	var data any
	if len(out.Data) > 0 && json.Unmarshal(out.Data, &data) != nil {
		return nil, serviceError{"AWS", "answered with something Mewla could not read"}
	}
	return data, nil
}

// awsFailure turns AWS's error text into one line for the person.
func awsFailure(text string) error {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, expired := range []string{"expiredtoken", "requestexpired", "security token included in the request is expired", "invalid_token", "invalidclienttokenid", "unrecognizedclientexception", "token has expired"} {
		if strings.Contains(lower, expired) {
			return errAWSExpired
		}
	}
	if text == "" {
		return serviceError{"AWS", "the call failed without a message"}
	}
	return serviceError{"AWS", cutText(text, maxServiceErrorBytes)}
}

// ---- identity -----------------------------------------------------------

type awsIdentity struct {
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
	UserID  string `json:"UserId"`
}

func (m *Manager) awsWhoami(ctx context.Context, r *record, secret string) (awsIdentity, error) {
	data, err := m.awsCall(ctx, r, secret, "sts", "GetCallerIdentity", awsDefaultRegion, map[string]any{})
	if err != nil {
		return awsIdentity{}, err
	}
	return awsIdentityOf(data)
}

func awsIdentityOf(data any) (awsIdentity, error) {
	raw, _ := json.Marshal(data)
	var id awsIdentity
	if json.Unmarshal(raw, &id) != nil || id.Account == "" || id.Arn == "" {
		return awsIdentity{}, errors.New("AWS did not say who is signed in")
	}
	return id, nil
}

// discoverAWS checks the sign-in and returns the tools. It reads who is signed
// in and the account's name in one script.
func discoverAWS(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error) {
	probe, err := m.awsProbe(ctx, r, secret)
	if err != nil {
		return nil, err
	}
	id, err := awsIdentityOf(probe.Identity.Data)
	if err != nil {
		return nil, err
	}
	a := &r.Account
	a.Identity = id.Arn
	a.Region = awsDefaultRegion
	previous := a.AWS
	a.AWS = awsDetailsOf(id, probe)
	awsAdoptName(r, previous)
	return awsTools(), nil
}

// awsProbeResult is the answer of awsProbeScript.
type awsProbeResult struct {
	Identity awsPart `json:"identity"`
	Account  awsPart `json:"account"`
	Aliases  awsPart `json:"aliases"`
}

type awsPart struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"-"`
	Error string          `json:"error"`
}

func (p *awsPart) UnmarshalJSON(b []byte) error {
	var raw struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	p.OK, p.Error = raw.OK, raw.Error
	if raw.OK {
		var v any
		if json.Unmarshal(raw.Data, &v) == nil {
			p.Data, _ = json.Marshal(v)
		}
	}
	return nil
}

func (m *Manager) awsProbe(ctx context.Context, r *record, secret string) (awsProbeResult, error) {
	raw, err := m.awsRunScript(ctx, r, secret, awsProbeScript)
	if err != nil {
		return awsProbeResult{}, err
	}
	var probe awsProbeResult
	if json.Unmarshal(raw, &probe) != nil {
		return awsProbeResult{}, serviceError{"AWS", "answered with something Mewla could not read"}
	}
	if !probe.Identity.OK {
		return awsProbeResult{}, awsFailure(probe.Identity.Error)
	}
	return probe, nil
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

// ---- invoke -------------------------------------------------------------

func awsArgs(args map[string]any) (service, operation, region string, params map[string]any, err error) {
	service, _ = args["service"].(string)
	operation, _ = args["operation"].(string)
	region, _ = args["region"].(string)
	params = map[string]any{}
	if p, ok := args["params"]; ok && p != nil {
		if params, ok = p.(map[string]any); !ok {
			return "", "", "", nil, callRefused("params must be a JSON object")
		}
	}
	if alias, ok := awsServiceAliases[service]; ok {
		service = alias
	}
	if !awsServiceName.MatchString(service) {
		return "", "", "", nil, callRefused("name the AWS service, for example ec2")
	}
	if !awsOperationName.MatchString(operation) {
		return "", "", "", nil, callRefused("name the operation, for example DescribeInstances")
	}
	if region != "" && !awsRegionName.MatchString(region) {
		return "", "", "", nil, callRefused("region must look like us-east-1")
	}
	return service, awsOperation(operation), region, params, nil
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
	if r.OAuth == nil {
		// An account connected before AWS moved to its own sign-in.
		return nil, errAWSExpired
	}
	switch t.Name {
	case "whoami":
		id, err := m.awsWhoami(ctx, r, secret)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"account": id.Account, "arn": id.Arn, "user_id": id.UserID, "region": r.Account.Region})
	case "search":
		return m.awsSearch(ctx, r, secret, args)
	case "docs":
		return m.awsDocs(ctx, r, secret, args)
	}
	return m.awsOperationCall(ctx, r, secret, t.Name, args)
}

func (m *Manager) awsSearch(ctx context.Context, r *record, secret string, args map[string]any) ([]byte, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return nil, callRefused("say what to look for")
	}
	call := map[string]any{"search_phrase": query, "topics": []string{"reference_documentation"}}
	if n, ok := args["limit"].(float64); ok {
		call["limit"] = int(n)
	}
	return m.awsDocumentation(ctx, r, secret, awsToolSearch, call)
}

func (m *Manager) awsDocs(ctx context.Context, r *record, secret string, args map[string]any) ([]byte, error) {
	address, _ := args["url"].(string)
	if !awsDocURL.MatchString(address) {
		return nil, callRefused("docs reads a https://docs.aws.amazon.com page that search returned")
	}
	page := map[string]any{"url": address}
	if n, ok := args["start_index"].(float64); ok {
		page["start_index"] = int(n)
	}
	return m.awsDocumentation(ctx, r, secret, awsToolRead, map[string]any{"requests": []any{page}})
}

// awsDocumentation relays a documentation tool's answer.
func (m *Manager) awsDocumentation(ctx context.Context, r *record, secret, tool string, call map[string]any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, awsCallTimeout)
	defer cancel()
	result, err := m.awsMCP(ctx, r, secret, tool, call)
	if err != nil {
		return nil, err
	}
	var out any
	if result.StructuredContent != nil {
		out = result.StructuredContent
	} else if text := resultText(result); json.Unmarshal([]byte(text), &out) != nil {
		out = text
	}
	return json.Marshal(map[string]any{"result": out})
}

func (m *Manager) awsOperationCall(ctx context.Context, r *record, secret, tool string, args map[string]any) ([]byte, error) {
	service, operation, region, params, err := awsArgs(args)
	if err != nil {
		return nil, err
	}
	if tool == "read" && awsKind(operation, params) != "read" {
		return nil, callRefused(operation + " changes state or may return a secret, so read refuses it; execute runs it, with the person's authorization for this task")
	}
	if region == "" {
		region = r.Account.Region
	}
	if region == "" {
		region = awsDefaultRegion
	}
	data, err := m.awsCall(ctx, r, secret, service, operation, region, params)
	if err != nil {
		return nil, err
	}
	reply, err := json.Marshal(map[string]any{"account": awsAccountID(r), "service": service, "operation": operation, "region": region, "result": awsRedact(data)})
	if err != nil {
		return nil, err
	}
	if len(reply) > MaxResultBytes {
		return nil, errors.New("the result is larger than 1 MiB; ask for less with the operation's own filters and MaxResults")
	}
	return reply, nil
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
