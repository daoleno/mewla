package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// isCodexImageModelPath reports the Codex image_gen endpoints whose request
// model a connection's image_model replaces. /v1/alpha/search is not one.
func isCodexImageModelPath(path string) bool {
	return path == "/v1/images/generations" || path == "/v1/images/edits"
}

// bodySplice replaces body[start:end] with value. The forwarded body is read
// from the original buffer around the value, so a multi-MB image edit is never
// copied a second time.
type bodySplice struct {
	start, end int
	value      []byte
	// from is the replaced bytes (the client's model), for the gateway log.
	from string
}

func (s bodySplice) reader(body []byte) io.Reader {
	return io.MultiReader(bytes.NewReader(body[:s.start]), bytes.NewReader(s.value), bytes.NewReader(body[s.end:]))
}

func (s bodySplice) length(body []byte) int64 {
	return int64(len(body) - (s.end - s.start) + len(s.value))
}

// imageModelSplice locates the request model in a JSON or multipart/form-data
// image body and returns the splice that sets it to model. A body without a
// model gets one. Encoded (compressed) bodies are refused rather than spliced.
func imageModelSplice(body []byte, header http.Header, model string) (bodySplice, error) {
	if err := ValidateModelID(model); err != nil {
		return bodySplice{}, fmt.Errorf("%w: image model: %v", ErrRequestBodyMalformed, err)
	}
	if enc := strings.TrimSpace(header.Get("Content-Encoding")); enc != "" && !strings.EqualFold(enc, "identity") {
		return bodySplice{}, fmt.Errorf("%w: cannot rewrite %s-encoded image request", ErrRequestBodyMalformed, enc)
	}
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		return multipartModelSplice(body, params["boundary"], model)
	}
	return jsonModelSplice(body, model)
}

func jsonModelSplice(body []byte, model string) (bodySplice, error) {
	encoded, err := json.Marshal(model)
	if err != nil {
		return bodySplice{}, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	malformed := fmt.Errorf("%w: image request is not a json object", ErrRequestBodyMalformed)
	i := skipJSONSpace(body, 0)
	if i >= len(body) || body[i] != '{' {
		return bodySplice{}, malformed
	}
	open := i
	i = skipJSONSpace(body, i+1)
	if i < len(body) && body[i] == '}' {
		return bodySplice{start: open + 1, end: open + 1, value: append([]byte(`"model":`), encoded...)}, nil
	}
	for i < len(body) {
		if body[i] != '"' {
			return bodySplice{}, malformed
		}
		keyEnd, ok := skipJSONString(body, i)
		if !ok {
			return bodySplice{}, malformed
		}
		isModel := jsonKeyEquals(body[i:keyEnd], "model")
		i = skipJSONSpace(body, keyEnd)
		if i >= len(body) || body[i] != ':' {
			return bodySplice{}, malformed
		}
		valueStart := skipJSONSpace(body, i+1)
		valueEnd, ok := skipJSONValue(body, valueStart)
		if !ok {
			return bodySplice{}, malformed
		}
		if isModel {
			return bodySplice{start: valueStart, end: valueEnd, value: encoded, from: string(body[valueStart:valueEnd])}, nil
		}
		i = skipJSONSpace(body, valueEnd)
		if i >= len(body) {
			return bodySplice{}, malformed
		}
		if body[i] == '}' {
			break
		}
		if body[i] != ',' {
			return bodySplice{}, malformed
		}
		i = skipJSONSpace(body, i+1)
	}
	// No top-level model: add one as the first member.
	value := append(append([]byte(`"model":`), encoded...), ',')
	return bodySplice{start: open + 1, end: open + 1, value: value}, nil
}

func jsonKeyEquals(raw []byte, key string) bool {
	if !bytes.ContainsRune(raw, '\\') {
		return string(raw[1:len(raw)-1]) == key
	}
	var decoded string
	return json.Unmarshal(raw, &decoded) == nil && decoded == key
}

func skipJSONSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	return i
}

// skipJSONString returns the index just past the string starting at b[i]=='"'.
func skipJSONString(b []byte, i int) (int, bool) {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

// skipJSONValue returns the index just past the value starting at b[i]. It
// checks structure only (balanced brackets, terminated strings).
func skipJSONValue(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"':
		return skipJSONString(b, i)
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '"':
				end, ok := skipJSONString(b, i)
				if !ok {
					return 0, false
				}
				i = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, true
				}
			}
		}
		return 0, false
	default:
		start := i
		for i < len(b) && !strings.ContainsRune(",}] \t\r\n", rune(b[i])) {
			i++
		}
		return i, i > start
	}
}

// multipartModelSplice finds the form-data part named "model" and splices its
// value; without one it adds a model part before the closing delimiter.
func multipartModelSplice(body []byte, boundary, model string) (bodySplice, error) {
	malformed := fmt.Errorf("%w: image request multipart body is malformed", ErrRequestBodyMalformed)
	if boundary == "" {
		return bodySplice{}, malformed
	}
	delim := []byte("--" + boundary)
	next := append([]byte("\r\n"), delim...)
	var i int
	if bytes.HasPrefix(body, delim) {
		i = len(delim)
	} else if at := bytes.Index(body, next); at >= 0 {
		i = at + len(next)
	} else {
		return bodySplice{}, malformed
	}
	for {
		if bytes.HasPrefix(body[i:], []byte("--")) {
			// Closing delimiter: insert a model part before its CRLF.
			at := i - len(next)
			if at < 0 {
				return bodySplice{}, malformed
			}
			value := []byte("\r\n--" + boundary + "\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n" + model)
			return bodySplice{start: at, end: at, value: value}, nil
		}
		if !bytes.HasPrefix(body[i:], []byte("\r\n")) {
			return bodySplice{}, malformed
		}
		headerStart := i + 2
		headerLen := bytes.Index(body[headerStart:], []byte("\r\n\r\n"))
		if headerLen < 0 {
			return bodySplice{}, malformed
		}
		contentStart := headerStart + headerLen + 4
		contentLen := bytes.Index(body[contentStart:], next)
		if contentLen < 0 {
			return bodySplice{}, malformed
		}
		contentEnd := contentStart + contentLen
		if multipartPartName(body[headerStart:headerStart+headerLen]) == "model" {
			return bodySplice{start: contentStart, end: contentEnd, value: []byte(model), from: string(body[contentStart:contentEnd])}, nil
		}
		i = contentEnd + len(next)
	}
}

func multipartPartName(headers []byte) string {
	for _, line := range strings.Split(string(headers), "\r\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Disposition") {
			continue
		}
		if _, params, err := mime.ParseMediaType(strings.TrimSpace(value)); err == nil {
			return params["name"]
		}
	}
	return ""
}
