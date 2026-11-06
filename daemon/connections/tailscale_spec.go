package connections

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"gopkg.in/yaml.v3"
)

// Tailscale publishes its whole API as an OpenAPI document. The tools search
// that document and call an operation from it, so the connection covers every
// endpoint Tailscale offers, including ones added later, without a reviewed
// tool per endpoint.
const (
	tailscaleSpecURL = "https://api.tailscale.com/api/v2?outputOpenapiSchema=true"
	// tailscaleSpecFile is the copy kept in the state directory, so search works
	// when Tailscale's document can't be fetched.
	tailscaleSpecFile = "tailscale-api.yaml"
	tailscaleSpecLife = 7 * 24 * time.Hour
	maxTailscaleSpec  = 2 << 20
)

type tsParam struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type tsOperation struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Scope       string
	Params      []tsParam
	Body        any // the request body schema, references resolved
	BodyNeeded  bool
}

type tsIndex struct {
	ops  map[string]*tsOperation
	list []*tsOperation
}

var tsScopePattern = regexp.MustCompile("(?i)OAuth Scope:?\\s*`([a-z_:]+)`")

// parseTailscaleSpec indexes the operations of Tailscale's OpenAPI document.
// The document is YAML, OpenAPI 3.1, and refers to its own components; only
// local references are followed.
func parseTailscaleSpec(raw []byte) (*tsIndex, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, errors.New("Tailscale's API reference could not be read")
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return nil, errors.New("Tailscale's API reference has no operations")
	}
	index := &tsIndex{ops: map[string]*tsOperation{}}
	for path, rawItem := range paths {
		item, _ := rawItem.(map[string]any)
		shared, _ := item["parameters"].([]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			op, _ := item[method].(map[string]any)
			if op == nil {
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" || index.ops[id] != nil {
				continue
			}
			o := &tsOperation{ID: id, Method: strings.ToUpper(method), Path: path}
			o.Summary, _ = op["summary"].(string)
			o.Description, _ = op["description"].(string)
			if tags, ok := op["tags"].([]any); ok {
				for _, tag := range tags {
					if s, ok := tag.(string); ok {
						o.Tags = append(o.Tags, s)
					}
				}
			}
			if m := tsScopePattern.FindStringSubmatch(o.Description); m != nil {
				o.Scope = m[1]
			}
			seen := map[string]bool{}
			addParams := func(list []any) {
				for _, p := range list {
					p = tsResolve(doc, p, 0)
					pm, _ := p.(map[string]any)
					name, _ := pm["name"].(string)
					in, _ := pm["in"].(string)
					if name == "" || (in != "path" && in != "query") || seen[in+":"+name] {
						continue
					}
					seen[in+":"+name] = true
					param := tsParam{Name: name, In: in}
					param.Required, _ = pm["required"].(bool)
					if in == "path" {
						param.Required = true
					}
					param.Description, _ = pm["description"].(string)
					param.Description = tsShorten(param.Description, 160)
					if schema, ok := tsResolve(doc, pm["schema"], 0).(map[string]any); ok {
						param.Type, _ = schema["type"].(string)
					}
					o.Params = append(o.Params, param)
				}
			}
			own, _ := op["parameters"].([]any)
			addParams(own)
			addParams(shared)
			if body, ok := tsResolve(doc, op["requestBody"], 0).(map[string]any); ok {
				o.BodyNeeded, _ = body["required"].(bool)
				if content, ok := body["content"].(map[string]any); ok {
					for _, kind := range []string{"application/json", "application/hujson"} {
						if media, ok := content[kind].(map[string]any); ok {
							budget := 400
							o.Body = tsExpand(doc, media["schema"], 0, &budget)
							break
						}
					}
				}
			}
			index.ops[id] = o
			index.list = append(index.list, o)
		}
	}
	if len(index.list) == 0 {
		return nil, errors.New("Tailscale's API reference has no operations")
	}
	sort.Slice(index.list, func(i, j int) bool {
		if index.list[i].Path != index.list[j].Path {
			return index.list[i].Path < index.list[j].Path
		}
		return index.list[i].Method < index.list[j].Method
	})
	return index, nil
}

// tsResolve follows one local $ref chain.
func tsResolve(doc map[string]any, v any, depth int) any {
	for ; depth < 8; depth++ {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		ref, ok := m["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, "#/") {
			return v
		}
		var target any = doc
		for _, key := range strings.Split(ref[2:], "/") {
			key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
			obj, ok := target.(map[string]any)
			if !ok {
				return nil
			}
			target = obj[key]
		}
		v = target
	}
	return nil
}

// tsExpand inlines references up to a depth and size, keeping what an agent
// needs to write a body: types, property names, required lists, enums and
// short descriptions.
func tsExpand(doc map[string]any, v any, depth int, budget *int) any {
	*budget--
	if *budget < 0 || depth > 6 {
		return "…"
	}
	v = tsResolve(doc, v, 0)
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			switch k {
			case "type", "required", "enum", "format", "default":
				out[k] = val
			case "description":
				if s, ok := val.(string); ok {
					out[k] = tsShorten(s, 200)
				}
			case "properties":
				props := map[string]any{}
				if m, ok := val.(map[string]any); ok {
					for name, p := range m {
						props[name] = tsExpand(doc, p, depth+1, budget)
					}
				}
				out[k] = props
			case "items", "additionalProperties":
				if _, isBool := val.(bool); !isBool {
					out[k] = tsExpand(doc, val, depth+1, budget)
				}
			case "allOf", "oneOf", "anyOf":
				if list, ok := val.([]any); ok {
					items := make([]any, 0, len(list))
					for _, p := range list {
						items = append(items, tsExpand(doc, p, depth+1, budget))
					}
					out[k] = items
				}
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, p := range x {
			out = append(out, tsExpand(doc, p, depth+1, budget))
		}
		return out
	}
	return v
}

func tsShorten(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}

// search ranks operations by how many query words they contain; every word
// must appear somewhere in the operation.
func (x *tsIndex) search(query string, limit int) []*tsOperation {
	words := strings.Fields(strings.ToLower(query))
	if limit <= 0 || limit > 30 {
		limit = 15
	}
	type scored struct {
		op    *tsOperation
		score int
	}
	var hits []scored
	for _, op := range x.list {
		if strings.EqualFold(op.ID, strings.TrimSpace(query)) {
			return []*tsOperation{op}
		}
		name := strings.ToLower(op.ID + " " + op.Summary + " " + op.Path + " " + strings.Join(op.Tags, " "))
		text := name + " " + strings.ToLower(op.Description)
		score := 0
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				score += 3
			case strings.Contains(text, w):
				score++
			default:
				score = -1
			}
			if score < 0 {
				break
			}
		}
		if score > 0 || len(words) == 0 {
			hits = append(hits, scored{op, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*tsOperation, len(hits))
	for i, h := range hits {
		out[i] = h.op
	}
	return out
}

// summary is what search returns for one operation.
func (o *tsOperation) summary(detail bool) map[string]any {
	out := map[string]any{"operation": o.ID, "method": o.Method, "path": o.Path, "summary": o.Summary, "read": o.Method == "GET"}
	if o.Scope != "" {
		out["scope"] = o.Scope
	}
	var path, query []tsParam
	for _, p := range o.Params {
		if p.In == "path" {
			path = append(path, p)
		} else {
			query = append(query, p)
		}
	}
	if len(path) > 0 {
		out["path_params"] = path
	}
	if len(query) > 0 {
		out["query_params"] = query
	}
	if o.Body != nil {
		out["body"] = o.Body
		out["body_required"] = o.BodyNeeded
	}
	if detail {
		out["description"] = tsShorten(o.Description, 1500)
	}
	return out
}

// tailscaleSpecs holds the parsed document per state directory, so a search
// does not parse 250 KB each time.
var tailscaleSpecs = struct {
	sync.Mutex
	byFile map[string]*tsCached
}{byFile: map[string]*tsCached{}}

type tsCached struct {
	index   *tsIndex
	fetched time.Time
	failed  time.Time
}

// tailscaleIndex returns the API reference: the cached copy while it is fresh,
// otherwise Tailscale's current document, falling back to the cached copy when
// Tailscale can't be reached.
func (m *Manager) tailscaleIndex(ctx context.Context, r *record, force bool) (*tsIndex, error) {
	file := filepath.Join(filepath.Dir(m.path), tailscaleSpecFile)
	tailscaleSpecs.Lock()
	cached := tailscaleSpecs.byFile[file]
	tailscaleSpecs.Unlock()
	if cached == nil {
		cached = &tsCached{}
		if raw, err := os.ReadFile(file); err == nil {
			if index, err := parseTailscaleSpec(raw); err == nil {
				cached.index = index
				if info, err := os.Stat(file); err == nil {
					cached.fetched = info.ModTime()
				}
			}
		}
	}
	stale := cached.index == nil || time.Since(cached.fetched) >= tailscaleSpecLife
	backoff := !cached.failed.IsZero() && time.Since(cached.failed) < 10*time.Minute
	if (stale || force) && (force || !backoff) {
		raw, err := m.fetchTailscaleSpec(ctx, r)
		if err == nil {
			var index *tsIndex
			if index, err = parseTailscaleSpec(raw); err == nil {
				cached.index, cached.fetched, cached.failed = index, time.Now(), time.Time{}
				_ = atomicfile.Write(file, raw, 0o600)
			}
		}
		if err != nil {
			cached.failed = time.Now()
		}
	}
	tailscaleSpecs.Lock()
	tailscaleSpecs.byFile[file] = cached
	tailscaleSpecs.Unlock()
	if cached.index == nil {
		return nil, callRefused("Tailscale's API reference is unavailable right now. Try again shortly.")
	}
	return cached.index, nil
}

func (m *Manager) fetchTailscaleSpec(ctx context.Context, r *record) ([]byte, error) {
	target, err := url.Parse(tailscaleSpecURL)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(r.Account.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("invalid endpoint")
	}
	// The document is read from the connected API's own host, never elsewhere.
	target.Scheme, target.Host = endpoint.Scheme, endpoint.Host
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/yaml, text/yaml, */*")
	req.Header.Set("User-Agent", "Mewla-Integrations")
	resp, err := m.clientFor(r).Do(req)
	if err != nil {
		return nil, errNetwork
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, StatusError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTailscaleSpec+1))
	if err != nil || len(raw) > maxTailscaleSpec {
		return nil, errors.New("Tailscale's API reference is too large")
	}
	return raw, nil
}
