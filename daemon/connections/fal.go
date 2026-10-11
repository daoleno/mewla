package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// fal's REST APIs. Its MCP server signs in by browser only; an API key and
// daemon-side downloads need the REST surface.
const (
	falAPI   = "https://api.fal.ai/v1"
	falRun   = "https://fal.run"
	falQueue = "https://queue.fal.run"
	falREST  = "https://rest.fal.ai"
	// fal's single-request upload limit; larger files need multipart.
	falMaxUpload = 90 << 20
)

const falModelPattern = `^[A-Za-z0-9][A-Za-z0-9._-]{0,99}(/[A-Za-z0-9][A-Za-z0-9._-]{0,99}){1,7}$`

var falModelID = regexp.MustCompile(falModelPattern)

func falTools() []Tool {
	model := map[string]any{"type": "string", "pattern": falModelPattern, "maxLength": 400, "description": "fal model (endpoint) ID from search_models, e.g. fal-ai/flux/schnell"}
	requestID := map[string]any{"type": "string", "pattern": "^[A-Za-z0-9_-]{1,100}$", "description": "request_id returned by submit"}
	input := map[string]any{"type": "object", "description": "Model input matching describe_model's input_schema. Pass local files through upload_file first."}
	outputDir := map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "description": "Folder for output files, relative to the CLI's working directory. Default: the daemon's media folder."}
	tool := func(name, desc string, read bool, schema map[string]any) Tool {
		raw, _ := json.Marshal(schema)
		return Tool{Name: name, Description: desc, InputSchema: raw, Read: read}
	}
	return []Tool{
		tool("search_models", "Search fal's model catalogue by text and category (one page). Free.", true, object(map[string]any{
			"query":    map[string]any{"type": "string", "maxLength": 200},
			"category": map[string]any{"type": "string", "maxLength": 100, "description": "e.g. text-to-image, image-to-video, text-to-video"},
			"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			"cursor":   map[string]any{"type": "string", "maxLength": 200},
		})),
		tool("describe_model", "Read a fal model's input and output schema and its unit price. Free.", true, object(map[string]any{"model": model}, "model")),
		tool("run", "Run a fal model and wait up to 10 minutes; for short jobs such as images. Billed by fal. Output files are saved locally and returned as paths.", false, object(map[string]any{"model": model, "input": input, "output_dir": outputDir}, "model", "input")),
		tool("submit", "Queue a fal job and return its request_id; use for video and other long jobs. Billed by fal. Follow with status, then result.", false, object(map[string]any{"model": model, "input": input}, "model", "input")),
		tool("status", "Check a queued fal job. Free.", true, object(map[string]any{"model": model, "request_id": requestID, "logs": map[string]any{"type": "boolean"}}, "model", "request_id")),
		tool("result", "Fetch a finished fal job; output files are saved locally and returned as paths. Free.", true, object(map[string]any{"model": model, "request_id": requestID, "output_dir": outputDir}, "model", "request_id")),
		tool("cancel", "Cancel a queued fal job.", false, object(map[string]any{"model": model, "request_id": requestID}, "model", "request_id")),
		tool("upload_file", "Upload a local file (up to 90 MB) to fal's CDN and return its URL for model inputs such as image_url. From the mewla CLI only.", false, object(map[string]any{"path": map[string]any{"type": "string", "minLength": 1, "maxLength": 4096}}, "path")),
	}
}

// falAppPath is where queue status, result and cancel live: the app, not the
// sub-endpoint, as in fal's own client.
func falAppPath(model string) string {
	parts := strings.Split(model, "/")
	n := 2
	if parts[0] == "workflows" || parts[0] == "comfy" {
		n = 3
	}
	if len(parts) < n {
		return model
	}
	return strings.Join(parts[:n], "/")
}

// falVerify checks a key with an authenticated, free request.
func (c *mediaCall) falVerify(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, falAPI+"/models/pricing?endpoint_id=fal-ai/flux/schnell", nil, "", 1<<20)
	return err
}

func (c *mediaCall) falPrice(ctx context.Context, model string) any {
	if c.secret == "" {
		return nil
	}
	raw, err := c.do(ctx, http.MethodGet, falAPI+"/models/pricing?"+url.Values{"endpoint_id": {model}}.Encode(), nil, "", 1<<20)
	if err != nil {
		return nil
	}
	var body struct {
		Prices []map[string]any `json:"prices"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Prices) == 0 {
		return nil
	}
	delete(body.Prices[0], "endpoint_id")
	return body.Prices[0]
}

func (c *mediaCall) invokeFal(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	model, _ := args["model"].(string)
	if _, ok := args["model"]; ok && !falModelID.MatchString(model) {
		return nil, errors.New("model must be a fal endpoint ID such as fal-ai/flux/schnell")
	}
	if tool != "search_models" && tool != "describe_model" && c.secret == "" {
		return nil, keyError{c.service, true}
	}
	requestID, _ := args["request_id"].(string)
	switch tool {
	case "search_models":
		query := url.Values{"status": {"active"}, "limit": {"20"}}
		for key, param := range map[string]string{"query": "q", "category": "category", "cursor": "cursor"} {
			if v, ok := args[key].(string); ok && v != "" {
				query.Set(param, v)
			}
		}
		if v, ok := args["limit"].(float64); ok {
			query.Set("limit", strconv.Itoa(int(v)))
		}
		raw, err := c.do(ctx, http.MethodGet, falAPI+"/models?"+query.Encode(), nil, "", 8<<20)
		if err != nil {
			return nil, err
		}
		var page struct {
			Models []struct {
				EndpointID string `json:"endpoint_id"`
				Metadata   struct {
					DisplayName string `json:"display_name"`
					Category    string `json:"category"`
					Description string `json:"description"`
				} `json:"metadata"`
			} `json:"models"`
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return nil, errors.New("fal returned an unreadable model list")
		}
		models := []map[string]any{}
		for _, item := range page.Models {
			desc := item.Metadata.Description
			if runes := []rune(desc); len(runes) > 300 {
				desc = string(runes[:300]) + "…"
			}
			models = append(models, map[string]any{"model": item.EndpointID, "name": item.Metadata.DisplayName, "category": item.Metadata.Category, "description": desc})
		}
		out := map[string]any{"models": models, "has_more": page.HasMore}
		if page.NextCursor != nil {
			out["next_cursor"] = *page.NextCursor
		}
		return json.Marshal(out)
	case "describe_model":
		raw, err := c.do(ctx, http.MethodGet, falAPI+"/models?"+url.Values{"endpoint_id": {model}, "expand": {"openapi-3.0"}}.Encode(), nil, "", 16<<20)
		if err != nil {
			return nil, err
		}
		var page struct {
			Models []struct {
				EndpointID string         `json:"endpoint_id"`
				Metadata   map[string]any `json:"metadata"`
				OpenAPI    map[string]any `json:"openapi"`
			} `json:"models"`
		}
		if json.Unmarshal(raw, &page) != nil || len(page.Models) == 0 {
			return nil, errors.New("fal has no model " + model + "; use search_models")
		}
		item := page.Models[0]
		in, out := falSchemas(item.OpenAPI)
		result := map[string]any{"model": item.EndpointID, "name": item.Metadata["display_name"], "category": item.Metadata["category"], "description": item.Metadata["description"], "input_schema": in, "output_schema": out}
		if price := c.falPrice(ctx, model); price != nil {
			result["price"] = price
		}
		return json.Marshal(result)
	case "run", "submit":
		body, _ := json.Marshal(args["input"])
		if tool == "run" {
			if err := c.useOutputDir(args); err != nil {
				return nil, err
			}
		}
		target := falRun + "/" + model
		if tool == "submit" {
			target = falQueue + "/" + model
		}
		var headers http.Header
		raw, err := c.do(ctx, http.MethodPost, target, bytes.NewReader(body), "application/json", mediaMaxJSONBytes, &headers)
		if err != nil {
			return nil, err
		}
		value, err := decodeJSON(raw)
		if err != nil {
			return nil, errors.New("fal returned unreadable output")
		}
		result := map[string]any{"model": model}
		if tool == "submit" {
			queued, _ := value.(map[string]any)
			for _, key := range []string{"request_id", "status", "queue_position"} {
				if v, ok := queued[key]; ok {
					result[key] = v
				}
			}
			if result["request_id"] == nil {
				return nil, errors.New("fal did not return a request_id")
			}
		} else {
			if result["output"], err = c.collect(ctx, value); err != nil {
				return nil, err
			}
			if id := headers.Get("X-Fal-Request-Id"); id != "" {
				result["request_id"] = id
			}
		}
		if units := headers.Get("X-Fal-Billable-Units"); units != "" {
			result["billable_units"] = units
		}
		if price := c.falPrice(ctx, model); price != nil {
			result["price"] = price
		}
		return c.result(result)
	case "status":
		target := falQueue + "/" + falAppPath(model) + "/requests/" + url.PathEscape(requestID) + "/status"
		if logs, _ := args["logs"].(bool); logs {
			target += "?logs=1"
		}
		raw, err := c.do(ctx, http.MethodGet, target, nil, "", 4<<20)
		if err != nil {
			return nil, err
		}
		value, err := decodeJSON(raw)
		status, _ := value.(map[string]any)
		if err != nil || status == nil {
			return nil, errors.New("fal returned an unreadable status")
		}
		if logs, ok := status["logs"].([]any); ok && len(logs) > 20 {
			status["logs"] = logs[len(logs)-20:]
		}
		for _, key := range []string{"response_url", "status_url", "cancel_url"} {
			delete(status, key)
		}
		status["model"] = model
		return json.Marshal(status)
	case "result":
		if err := c.useOutputDir(args); err != nil {
			return nil, err
		}
		raw, err := c.do(ctx, http.MethodGet, falQueue+"/"+falAppPath(model)+"/requests/"+url.PathEscape(requestID), nil, "", mediaMaxJSONBytes)
		if err != nil {
			return nil, err
		}
		value, err := decodeJSON(raw)
		if err != nil {
			return nil, errors.New("fal returned unreadable output")
		}
		output, err := c.collect(ctx, value)
		if err != nil {
			return nil, err
		}
		return c.result(map[string]any{"model": model, "request_id": requestID, "output": output})
	case "cancel":
		raw, err := c.do(ctx, http.MethodPut, falQueue+"/"+falAppPath(model)+"/requests/"+url.PathEscape(requestID)+"/cancel", nil, "", 1<<20)
		if err != nil {
			return nil, err
		}
		value, _ := decodeJSON(raw)
		return json.Marshal(map[string]any{"model": model, "request_id": requestID, "result": value})
	case "upload_file":
		p, _ := args["path"].(string)
		resolved, err := c.localPath(p)
		if err != nil {
			return nil, err
		}
		return c.falUpload(ctx, resolved)
	}
	return nil, errors.New("tool not found")
}

func (c *mediaCall) falUpload(ctx context.Context, p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.New("file could not be opened: " + p)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file: " + p)
	}
	if info.Size() > falMaxUpload {
		return nil, errors.New("files over 90 MB cannot be uploaded yet")
	}
	contentType := mimeForPath(p)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	initiate, _ := json.Marshal(map[string]string{"file_name": filepath.Base(p), "content_type": contentType})
	raw, err := c.do(ctx, http.MethodPost, falREST+"/storage/upload/initiate?storage_type=fal-cdn-v3", bytes.NewReader(initiate), "application/json", 1<<20)
	if err != nil {
		return nil, err
	}
	var target struct {
		UploadURL string `json:"upload_url"`
		FileURL   string `json:"file_url"`
	}
	if json.Unmarshal(raw, &target) != nil || !strings.HasPrefix(target.UploadURL, "https://") || !strings.HasPrefix(target.FileURL, "https://") {
		return nil, errors.New("fal did not return an upload URL")
	}
	// The signed upload URL carries its own authorization; the key never goes there.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target.UploadURL, f)
	if err != nil {
		return nil, errors.New("invalid upload URL")
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", mediaUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		return nil, errors.New("file upload to fal failed")
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("file upload to fal returned HTTP %d", resp.StatusCode)
	}
	return json.Marshal(map[string]any{"url": target.FileURL, "mime": contentType, "bytes": info.Size(), "path": p})
}

// falSchemas pulls a model's input (submit body) and output (result) schemas
// out of its OpenAPI document, with local references inlined when possible.
func falSchemas(doc map[string]any) (any, any) {
	if doc == nil {
		return nil, nil
	}
	expanded, err := expandRefs(doc, doc, 0)
	inlined := err == nil
	if !inlined {
		expanded = doc
	}
	root, _ := expanded.(map[string]any)
	paths, _ := root["paths"].(map[string]any)
	dig := func(v any, keys ...string) any {
		for _, key := range keys {
			m, _ := v.(map[string]any)
			v = m[key]
		}
		return v
	}
	var in, out any
	for p, item := range paths {
		if !strings.Contains(p, "/requests/") {
			if schema := dig(item, "post", "requestBody", "content", "application/json", "schema"); schema != nil {
				in = schema
			}
		} else if strings.HasSuffix(p, "/requests/{request_id}") {
			out = dig(item, "get", "responses", "200", "content", "application/json", "schema")
		}
	}
	if !inlined {
		// Recursive schemas stay as references; ship the definitions with them.
		components := dig(doc, "components", "schemas")
		in = map[string]any{"schema": in, "components": components}
		out = map[string]any{"schema": out, "components": components}
	}
	return in, out
}
