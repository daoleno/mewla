package friends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// uploadTimeout bounds one media upload; the service allows ten minutes.
const uploadTimeout = 9 * time.Minute

// Upload sends one prepared file to the service and returns its id. The
// signature covers the file's hash and kind rather than the whole body, so
// nothing large is buffered; the service checks the received bytes against
// the hash. A retry of the same file returns the same upload.
func (c *Client) Upload(ctx context.Context, path string, file MediaFile, kind, posterID string) (string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	base := strings.TrimRight(c.BaseURL, "/")
	mount := ""
	if i := strings.Index(base, "://"); i >= 0 {
		if j := strings.Index(base[i+3:], "/"); j >= 0 {
			mount = base[i+3+j:]
		}
	}
	signedPath := mount + "/v1/media"
	width, height := 0, 0
	if kind == KindVideo {
		width, height = file.Width, file.Height
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := linkproto.RandomID(16)
	if err != nil {
		return "", err
	}
	signature := c.Auth.CreateLinkSignature(reqsign.AccountScheme.Payload(http.MethodPut, signedPath, timestamp, nonce,
		UploadSignedBody(file.SHA256, kind, file.Mime, width, height, posterID)))
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, base+"/v1/media", handle)
	if err != nil {
		return "", err
	}
	request.ContentLength = file.Bytes
	request.Header.Set("Authorization", reqsign.AccountScheme.Header(c.Auth.PublicKeyHex(), timestamp, nonce, signature))
	request.Header.Set("Content-Type", file.Mime)
	request.Header.Set("X-Mewla-Sha256", file.SHA256)
	request.Header.Set("X-Mewla-Kind", kind)
	request.Header.Set("X-Mewla-Dims", strconv.Itoa(width)+"x"+strconv.Itoa(height))
	if posterID != "" {
		request.Header.Set("X-Mewla-Poster", posterID)
	}
	client := &http.Client{}
	if c.HTTP != nil {
		copied := *c.HTTP
		copied.Timeout = 0 // the context bounds the upload; the default is for small JSON
		client = &copied
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("reach the Friends service: %w", err)
	}
	defer response.Body.Close()
	var out struct {
		ID    string   `json:"id"`
		Error string   `json:"error"`
		Kinds []string `json:"kinds"`
	}
	_ = json.NewDecoder(response.Body).Decode(&out)
	if response.StatusCode >= 300 {
		// A service from before media answers an unknown route with a plain
		// 404 or 405 and no error code.
		if out.Error == "" && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed) {
			out.Error = "media_off"
		}
		return "", &ServiceError{Status: response.StatusCode, Code: out.Error, Kinds: out.Kinds}
	}
	if out.ID == "" {
		return "", errors.New("the Friends service sent a response this version cannot read")
	}
	return out.ID, nil
}
