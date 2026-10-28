package connections

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The AWS driver knows the AWS API from the service models that ship with the
// AWS CLI (botocore's service-2.json). They list every operation, say what it
// takes and returns, and are the only source of the names the driver passes to
// the CLI: a service or operation that is not in them is never run.

// awsServiceAlias maps the name of a model directory to the name the CLI uses
// for it. The CLI keeps the others as they are.
var awsServiceAlias = map[string]string{
	"s3":           "s3api",
	"config":       "configservice",
	"codedeploy":   "deploy",
	"agenttoolkit": "agent-toolkit",
}

// awsNotInCLI are operations the model has and the CLI does not offer under
// any name (it replaces them with its own commands, such as emr create-cluster).
// TestAWSCommandNamesMatchCLI keeps the list honest.
var awsNotInCLI = map[string]bool{
	"agent-toolkit/GetLatestSkillVersion": true, "agent-toolkit/GetSkill": true, "agent-toolkit/GetSkillFileChecksum": true, "agent-toolkit/ListSkills": true,
	"ec2/ImportInstance": true, "ec2/ImportVolume": true,
	"emr/AddJobFlowSteps": true, "emr/ListBootstrapActions": true, "emr/ListInstanceGroups": true, "emr/RunJobFlow": true,
	"emr/SetKeepJobFlowAliveWhenNoSteps": true, "emr/SetTerminationProtection": true, "emr/SetUnhealthyNodeReplacement": true,
	"emr/SetVisibleToAllUsers": true, "emr/TerminateJobFlows": true,
	"mturk/ListHITsForQualificationType": true, "rds/ModifyOptionGroup": true,
	"ses/DeleteVerifiedEmailAddress": true, "ses/ListVerifiedEmailAddresses": true, "ses/VerifyEmailAddress": true,
}

// awsOperation is one API operation.
type awsOperation struct {
	Service    string // CLI name, for example "rds"
	Name       string // API name, for example "DescribeDBInstances"
	Command    string // CLI name, for example "describe-db-instances"
	Summary    string
	Deprecated bool
	input      string // shape names
	output     string
}

type awsService struct {
	Name     string // CLI name
	FullName string
	file     string
	ops      map[string]*awsOperation // by awsOpKey
}

// awsIndex lists the services and operations of one CLI installation.
type awsIndex struct {
	services map[string]*awsService
	names    []string

	mu     sync.Mutex
	shapes map[string]*awsShapes // loaded on demand, a few at a time
	order  []string
}

var (
	firstCap  = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	numberCap = regexp.MustCompile(`([a-z])([0-9]+)([A-Z])`)
	endCap    = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	specialS  = regexp.MustCompile(`[A-Z]{2,}s$`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
	spaces    = regexp.MustCompile(`\s+`)
)

// awsCommandName turns an API operation name into the CLI's command name the
// way botocore's xform_name does: DescribeDBInstances is describe-db-instances.
func awsCommandName(name string) string {
	if loc := specialS.FindStringIndex(name); loc != nil {
		name = name[:loc[0]] + "-" + strings.ToLower(name[loc[0]:])
	}
	s := firstCap.ReplaceAllString(name, "${1}-${2}")
	s = numberCap.ReplaceAllString(s, "${1}-${2}-${3}")
	s = endCap.ReplaceAllString(s, "${1}-${2}")
	return strings.Trim(awsCommandFixes.Replace("-"+strings.ToLower(s)+"-"), "-")
}

// awsCommandFixes are the names the CLI spells differently from the rule above.
var awsCommandFixes = strings.NewReplacer(
	"-o-tel-", "-otel-", "-o-auth2-", "-oauth2-", "-whats-app-", "-whatsapp-",
	"-cachedi-scsi-", "-cached-iscsi-", "-storedi-scsi-", "-stored-iscsi-",
)

// awsOpKey is how a caller's spelling of an operation is matched: case and
// dashes do not matter, so DescribeInstances and describe-instances agree.
func awsOpKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "-", ""), "_", ""))
}

func plainText(doc string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(htmlTag.ReplaceAllString(doc, " ")), " "))
}

// firstSentence is the opening of an operation's documentation, for a list.
func firstSentence(doc string, limit int) string {
	text := plainText(doc)
	if i := strings.Index(text, ". "); i > 0 && i < limit {
		return text[:i+1]
	}
	return cutText(text, limit)
}

// awsModelDirs are the places the CLI keeps its models, relative to a
// directory above the executable: the self-contained installer (macOS pkg and
// Linux bundle) and a Python install such as Homebrew's.
func awsModelDirs(root string) []string {
	out := []string{
		filepath.Join(root, "dist", "awscli", "botocore", "data"),
		filepath.Join(root, "awscli", "botocore", "data"),
		filepath.Join(root, "botocore", "data"),
	}
	for _, pattern := range []string{
		filepath.Join(root, "lib", "python*", "site-packages", "awscli", "botocore", "data"),
		filepath.Join(root, "lib", "python*", "site-packages", "botocore", "data"),
		filepath.Join(root, "libexec", "lib", "python*", "site-packages", "awscli", "botocore", "data"),
		filepath.Join(root, "libexec", "lib", "python*", "site-packages", "botocore", "data"),
	} {
		matches, _ := filepath.Glob(pattern)
		out = append(out, matches...)
	}
	return out
}

// findAWSModels locates the CLI's service models by walking up from the real
// path of the aws executable.
func findAWSModels() (string, error) {
	exe, err := exec.LookPath("aws")
	if err != nil {
		return "", errAWSMissing
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	for i := 0; i < 7; i++ {
		for _, candidate := range awsModelDirs(dir) {
			if info, err := os.Stat(filepath.Join(candidate, "sts")); err == nil && info.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("the AWS CLI's service models were not found next to " + exe + "; search and docs need them, so reinstall the AWS CLI v2")
}

var errAWSMissing = errors.New("the AWS CLI is not installed on this computer; install AWS CLI v2 and connect again")

// openModel opens a service's newest model, plain or gzipped.
func openModel(serviceDir string) (io.ReadCloser, error) {
	versions, err := os.ReadDir(serviceDir)
	if err != nil {
		return nil, err
	}
	newest := ""
	for _, v := range versions {
		if v.IsDir() && v.Name() > newest {
			newest = v.Name()
		}
	}
	if newest == "" {
		return nil, errors.New("no model")
	}
	path := filepath.Join(serviceDir, newest, "service-2.json")
	if f, err := os.Open(path); err == nil {
		return f, nil
	}
	f, err := os.Open(path + ".gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return readCloser{zr, f}, nil
}

type readCloser struct {
	io.Reader
	file *os.File
}

func (r readCloser) Close() error { return r.file.Close() }

type awsModelDoc struct {
	Metadata struct {
		ServiceFullName string `json:"serviceFullName"`
		ServiceID       string `json:"serviceId"`
	} `json:"metadata"`
	Operations map[string]struct {
		Name          string `json:"name"`
		Documentation string `json:"documentation"`
		Deprecated    bool   `json:"deprecated"`
		Input         *struct {
			Shape string `json:"shape"`
		} `json:"input"`
		Output *struct {
			Shape string `json:"shape"`
		} `json:"output"`
	} `json:"operations"`
}

// loadAWSIndex reads the operations of every service. Shapes are skipped:
// they are read for one service at a time, when its documentation is asked for.
func loadAWSIndex(dir string) (*awsIndex, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	idx := &awsIndex{services: map[string]*awsService{}, shapes: map[string]*awsShapes{}}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		serviceDir := filepath.Join(dir, e.Name())
		rc, err := openModel(serviceDir)
		if err != nil {
			continue
		}
		var doc awsModelDoc
		err = json.NewDecoder(rc).Decode(&doc)
		rc.Close()
		if err != nil || len(doc.Operations) == 0 {
			continue
		}
		name := e.Name()
		if alias, ok := awsServiceAlias[name]; ok {
			name = alias
		}
		svc := &awsService{Name: name, FullName: doc.Metadata.ServiceFullName, file: serviceDir, ops: map[string]*awsOperation{}}
		for opName, op := range doc.Operations {
			if awsNotInCLI[name+"/"+opName] {
				continue
			}
			o := &awsOperation{Service: name, Name: opName, Command: awsCommandName(opName), Summary: firstSentence(op.Documentation, 200), Deprecated: op.Deprecated}
			if op.Input != nil {
				o.input = op.Input.Shape
			}
			if op.Output != nil {
				o.output = op.Output.Shape
			}
			svc.ops[awsOpKey(opName)] = o
		}
		idx.services[name] = svc
		idx.names = append(idx.names, name)
	}
	if len(idx.services) == 0 {
		return nil, errors.New("the AWS CLI's service models are empty")
	}
	sort.Strings(idx.names)
	return idx, nil
}

// service finds a service by the CLI's name; the model directory's name works too.
func (idx *awsIndex) service(name string) (*awsService, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if alias, ok := awsServiceAlias[name]; ok {
		name = alias
	}
	svc := idx.services[name]
	if svc == nil {
		return nil, callRefused(fmt.Sprintf("unknown AWS service %q; search finds services and operations", cutText(name, 64)))
	}
	return svc, nil
}

func (idx *awsIndex) operation(service, operation string) (*awsOperation, error) {
	svc, err := idx.service(service)
	if err != nil {
		return nil, err
	}
	op := svc.ops[awsOpKey(operation)]
	if op == nil {
		return nil, callRefused(fmt.Sprintf("%s has no operation %q; search finds the operation you need", svc.Name, cutText(operation, 80)))
	}
	return op, nil
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

// ---- documentation ------------------------------------------------------

type awsShape struct {
	Type          string                                           `json:"type"`
	Members       map[string]struct{ Shape, Documentation string } `json:"members"`
	Required      []string                                         `json:"required"`
	Member        *struct{ Shape string }                          `json:"member"`
	Key           *struct{ Shape string }                          `json:"key"`
	Value         *struct{ Shape string }                          `json:"value"`
	Enum          []string                                         `json:"enum"`
	Streaming     bool                                             `json:"streaming"`
	EventStream   bool                                             `json:"eventstream"`
	Documentation string                                           `json:"documentation"`
}

type awsShapes struct {
	shapes map[string]awsShape
	// members that stream or carry an event stream, by operation shape
}

func (idx *awsIndex) loadShapes(svc *awsService) (*awsShapes, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if s, ok := idx.shapes[svc.Name]; ok {
		return s, nil
	}
	rc, err := openModel(svc.file)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var doc struct {
		Shapes map[string]awsShape `json:"shapes"`
	}
	if err := json.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, err
	}
	s := &awsShapes{shapes: doc.Shapes}
	idx.shapes[svc.Name] = s
	idx.order = append(idx.order, svc.Name)
	for len(idx.order) > 2 {
		delete(idx.shapes, idx.order[0])
		idx.order = idx.order[1:]
	}
	return s, nil
}

// streams reports an operation whose request or reply is a stream. The CLI
// reads or writes a local file for those, which Mewla never lets a call do.
func (s *awsShapes) streams(shape string) bool {
	for _, member := range s.shapes[shape].Members {
		t := s.shapes[member.Shape]
		if t.Streaming || t.EventStream {
			return true
		}
	}
	return s.shapes[shape].EventStream
}

// typeOf is a compact type signature, for example list<structure{Name:string}>.
func (s *awsShapes) typeOf(name string, depth int) string {
	sh, ok := s.shapes[name]
	if !ok {
		return "unknown"
	}
	switch sh.Type {
	case "structure":
		if depth <= 0 {
			return "structure"
		}
		names := make([]string, 0, len(sh.Members))
		for n := range sh.Members {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > 12 {
			names = append(names[:12], "…")
		}
		parts := make([]string, 0, len(names))
		for _, n := range names {
			if n == "…" {
				parts = append(parts, n)
				continue
			}
			parts = append(parts, n+":"+s.typeOf(sh.Members[n].Shape, depth-1))
		}
		return "structure{" + strings.Join(parts, ", ") + "}"
	case "list":
		if sh.Member != nil {
			return "list<" + s.typeOf(sh.Member.Shape, depth) + ">"
		}
	case "map":
		if sh.Key != nil && sh.Value != nil {
			return "map<" + s.typeOf(sh.Key.Shape, 0) + ", " + s.typeOf(sh.Value.Shape, depth) + ">"
		}
	case "string":
		if len(sh.Enum) > 0 {
			values := sh.Enum
			if len(values) > 12 {
				values = append(append([]string{}, values[:12]...), "…")
			}
			return "string(" + strings.Join(values, "|") + ")"
		}
	}
	return sh.Type
}

type awsParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

func (s *awsShapes) members(shape string, describe bool) []awsParam {
	sh := s.shapes[shape]
	required := map[string]bool{}
	for _, n := range sh.Required {
		required[n] = true
	}
	names := make([]string, 0, len(sh.Members))
	for n := range sh.Members {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]awsParam, 0, len(names))
	for _, n := range names {
		m := sh.Members[n]
		p := awsParam{Name: n, Type: s.typeOf(m.Shape, 2), Required: required[n]}
		if describe {
			doc := m.Documentation
			if doc == "" {
				doc = s.shapes[m.Shape].Documentation
			}
			p.Description = firstSentence(doc, 200)
		}
		out = append(out, p)
	}
	return out
}

// ---- search -------------------------------------------------------------

type awsMatch struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Command   string `json:"command"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
}

func searchTokens(query string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		out = append(out, t)
	}
	return out
}

// search finds operations by words in their name, service and description.
// Every word must appear somewhere; name matches rank above description matches.
func (idx *awsIndex) search(query, service string, limit int) ([]awsMatch, error) {
	tokens := searchTokens(query)
	if len(tokens) == 0 {
		return nil, callRefused("say what you are looking for, for example \"rds instances\"")
	}
	scope := idx.names
	if service != "" {
		svc, err := idx.service(service)
		if err != nil {
			return nil, err
		}
		scope = []string{svc.Name}
	}
	type scored struct {
		op    *awsOperation
		score int
	}
	var hits []scored
	for _, name := range scope {
		svc := idx.services[name]
		serviceText := strings.ToLower(svc.Name + " " + svc.FullName)
		for _, op := range svc.ops {
			if op.Deprecated {
				continue
			}
			command := strings.ToLower(op.Command)
			words := strings.Split(command, "-")
			summary := searchTokens(op.Summary)
			total := 0
			for _, t := range tokens {
				stem := strings.TrimSuffix(t, "s")
				s := 0
				switch {
				case svc.Name == t:
					s = 4
				case contains(words, t):
					s = 5
				case contains(words, stem):
					s = 4
				case len(stem) >= 4 && strings.Contains(command, stem):
					s = 3
				case len(stem) >= 4 && strings.Contains(serviceText, stem):
					s = 2
				case contains(summary, t) || contains(summary, stem):
					s = 1
				}
				if s == 0 {
					total = 0
					break
				}
				total += s
			}
			if total > 0 {
				hits = append(hits, scored{op, total})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		// A read comes first among equals: it needs no authorization.
		if ki, kj := awsKind(hits[i].op.Name, nil), awsKind(hits[j].op.Name, nil); ki != kj {
			return ki == "read"
		}
		if hits[i].op.Service != hits[j].op.Service {
			return hits[i].op.Service < hits[j].op.Service
		}
		return hits[i].op.Name < hits[j].op.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]awsMatch, 0, len(hits))
	for _, h := range hits {
		out = append(out, awsMatch{Service: h.op.Service, Operation: h.op.Name, Command: "aws " + h.op.Service + " " + h.op.Command, Kind: awsKind(h.op.Name, nil), Summary: h.op.Summary})
	}
	return out, nil
}
