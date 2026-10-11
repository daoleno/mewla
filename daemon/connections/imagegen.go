package connections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Image generation reaches any OpenAI-compatible images endpoint the user
// configures: base URL, API key and a default model per account.

func imageTools() []Tool {
	common := map[string]any{
		"prompt":        map[string]any{"type": "string", "minLength": 1, "maxLength": 32000},
		"model":         map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "description": "Defaults to the account's model"},
		"size":          map[string]any{"type": "string", "maxLength": 40, "description": "e.g. 1024x1024, 1536x1024, auto"},
		"quality":       map[string]any{"type": "string", "maxLength": 20, "description": "e.g. low, medium, high, auto"},
		"n":             map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
		"output_format": map[string]any{"type": "string", "enum": []string{"png", "jpeg", "webp"}},
		"background":    map[string]any{"type": "string", "maxLength": 20, "description": "e.g. transparent, opaque, auto"},
		"output_dir":    map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "description": "Folder for output files, relative to the CLI's working directory. Default: the daemon's media folder."},
	}
	edit := map[string]any{
		"images": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 4096}, "description": "Local image paths to edit or use as references"},
		"mask":   map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "description": "Local PNG mask path"},
	}
	for k, v := range common {
		edit[k] = v
	}
	generate, _ := json.Marshal(object(common, "prompt"))
	edits, _ := json.Marshal(object(edit, "prompt", "images"))
	return []Tool{
		{Name: "generate", Description: "Generate images from a prompt. Billed by the endpoint. Files are saved locally and returned as paths.", InputSchema: generate},
		{Name: "edit", Description: "Edit images, or generate from reference images, with a prompt (needs an endpoint with /images/edits). Billed. From the mewla CLI only.", InputSchema: edits},
	}
}

// imageVerify checks the key. Compatible endpoints differ in what they list,
// so only a rejected key fails; anything else is verified by the first call.
func (c *mediaCall) imageVerify(ctx context.Context) (bool, error) {
	_, err := c.do(ctx, http.MethodGet, strings.TrimRight(c.account.Endpoint, "/")+"/models", nil, "", 4<<20)
	if errors.Is(err, errAuth) {
		return false, err
	}
	var vendor vendorError
	if errors.As(err, &vendor) && strings.HasSuffix(vendor.short, "HTTP 403") {
		return false, keyError{service: c.service}
	}
	return err == nil, nil
}

func (c *mediaCall) invokeImage(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	if c.secret == "" {
		return nil, keyError{c.service, true}
	}
	if err := c.useOutputDir(args); err != nil {
		return nil, err
	}
	model, _ := args["model"].(string)
	if model == "" {
		model = c.account.Model
	}
	fields := map[string]any{"model": model}
	for _, key := range []string{"prompt", "size", "quality", "n", "output_format", "background"} {
		if v, ok := args[key]; ok {
			fields[key] = v
		}
	}
	base := strings.TrimRight(c.account.Endpoint, "/")
	var raw []byte
	var headers http.Header
	var err error
	switch tool {
	case "generate":
		body, _ := json.Marshal(fields)
		raw, err = c.do(ctx, http.MethodPost, base+"/images/generations", bytes.NewReader(body), "application/json", mediaMaxJSONBytes, &headers)
	case "edit":
		var body []byte
		var contentType string
		body, contentType, err = c.imageForm(fields, args)
		if err == nil {
			raw, err = c.do(ctx, http.MethodPost, base+"/images/edits", bytes.NewReader(body), contentType, mediaMaxJSONBytes, &headers)
		}
		var vendor vendorError
		if errors.As(err, &vendor) && (strings.HasSuffix(vendor.short, "HTTP 404") || strings.HasSuffix(vendor.short, "HTTP 405")) {
			return nil, errors.New("this endpoint does not support image edits (/images/edits)")
		}
	default:
		return nil, errors.New("tool not found")
	}
	if err != nil {
		return nil, err
	}
	return c.imageResult(ctx, raw, headers, model)
}

// imageForm builds the multipart body for /images/edits from local files.
func (c *mediaCall) imageForm(fields, args map[string]any) ([]byte, string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range fields {
		_ = form.WriteField(key, fmt.Sprint(value))
	}
	attach := func(field, p string) error {
		resolved, err := c.localPath(p)
		if err != nil {
			return err
		}
		f, err := os.Open(resolved)
		if err != nil {
			return errors.New("image could not be opened: " + resolved)
		}
		defer f.Close()
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filepath.Base(resolved)))
		contentType := mimeForPath(resolved)
		if contentType == "" {
			contentType = "image/png"
		}
		header.Set("Content-Type", contentType)
		part, err := form.CreatePart(header)
		if err != nil {
			return err
		}
		if n, err := io.Copy(part, io.LimitReader(f, 50<<20+1)); err != nil || n > 50<<20 {
			return errors.New("each image must be a readable file up to 50 MB")
		}
		return nil
	}
	images, _ := args["images"].([]any)
	for _, image := range images {
		if err := attach("image", fmt.Sprint(image)); err != nil {
			return nil, "", err
		}
	}
	if mask, ok := args["mask"].(string); ok {
		if err := attach("mask", mask); err != nil {
			return nil, "", err
		}
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), form.FormDataContentType(), nil
}

type imageResponse struct {
	Created      any    `json:"created,omitempty"`
	Model        string `json:"model,omitempty"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Usage        any    `json:"usage,omitempty"`
	Data         []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

// imageResult writes each returned image to a file. Streamed responses
// (server-sent events) are read to their completed event.
func (c *mediaCall) imageResult(ctx context.Context, raw []byte, headers http.Header, model string) ([]byte, error) {
	var response imageResponse
	if strings.Contains(headers.Get("Content-Type"), "text/event-stream") {
		event, err := imageStreamEvent(raw)
		if err != nil {
			return nil, err
		}
		response = event
	} else if json.Unmarshal(raw, &response) != nil {
		return nil, errors.New(c.service + " returned unreadable output")
	}
	if len(response.Data) == 0 {
		return nil, errors.New(c.service + " did not return an image")
	}
	mimeType := ""
	if response.OutputFormat != "" {
		mimeType = "image/" + response.OutputFormat
	}
	images := []map[string]any{}
	for _, item := range response.Data {
		var file mediaFile
		var err error
		switch {
		case item.B64JSON != "":
			file, err = c.saveBase64(item.B64JSON, mimeType)
		case item.URL != "":
			file, err = c.download(ctx, item.URL, "")
		default:
			err = errors.New(c.service + " returned an empty image")
		}
		if err != nil {
			return nil, err
		}
		image := map[string]any{"path": file.Path, "mime": file.Mime, "bytes": file.Bytes}
		if file.URL != "" {
			image["url"] = file.URL
		}
		if item.RevisedPrompt != "" {
			image["revised_prompt"] = item.RevisedPrompt
		}
		images = append(images, image)
	}
	if response.Model != "" {
		model = response.Model
	}
	result := map[string]any{"model": model, "images": images, "output_dir": c.dir}
	for key, value := range map[string]any{"created": response.Created, "size": response.Size, "quality": response.Quality, "usage": response.Usage} {
		if value != nil && value != "" {
			result[key] = value
		}
	}
	if id := headers.Get("X-Request-Id"); id != "" {
		result["request_id"] = id
	}
	return json.Marshal(result)
}

func imageStreamEvent(raw []byte) (imageResponse, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	data := []string{}
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(line[5:]))
		} else if line == "" && len(data) > 0 {
			var event struct {
				Type          string `json:"type"`
				B64JSON       string `json:"b64_json"`
				URL           string `json:"url"`
				RevisedPrompt string `json:"revised_prompt"`
				OutputFormat  string `json:"output_format"`
				Size          string `json:"size"`
				Quality       string `json:"quality"`
				Usage         any    `json:"usage"`
				Error         *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(strings.Join(data, "")), &event) == nil {
				if event.Error != nil && event.B64JSON == "" {
					return imageResponse{}, vendorError{"image stream failed", vendorMessage([]byte(`{"message":` + strconv.Quote(event.Error.Message) + `}`))}
				}
				if strings.HasSuffix(event.Type, ".completed") && (event.B64JSON != "" || event.URL != "") {
					response := imageResponse{OutputFormat: event.OutputFormat, Size: event.Size, Quality: event.Quality, Usage: event.Usage}
					response.Data = append(response.Data, struct {
						URL           string `json:"url"`
						B64JSON       string `json:"b64_json"`
						RevisedPrompt string `json:"revised_prompt"`
					}{event.URL, event.B64JSON, event.RevisedPrompt})
					return response, nil
				}
			}
			data = data[:0]
		}
		if err != nil {
			return imageResponse{}, errors.New("image stream ended without an image")
		}
	}
}
