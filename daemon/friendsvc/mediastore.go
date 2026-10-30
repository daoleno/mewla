package friendsvc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MediaStore keeps the bytes of post media. Keys are random and never reused;
// anyone holding a key can read the object, which is how a public timeline
// shows it. Delete is idempotent.
type MediaStore interface {
	// Put stores size bytes of the file at path under key. sha256Hex is the
	// file's digest.
	Put(ctx context.Context, key, contentType, path string, size int64, sha256Hex string) error
	Delete(ctx context.Context, key string) error
	// URL is where the object is publicly readable.
	URL(key string) string
	// Open returns the object for the service to serve itself. Stores that a
	// public host serves return os.ErrNotExist.
	Open(key string) (*os.File, error)
}

// DirMediaStore keeps media in a directory of the service's own volume and
// serves it itself under GET <mount>/media/<key>. It is for tests and isolated
// runs, not for production.
type DirMediaStore struct {
	Dir string
	// PublicURL is the absolute address of the Friends mount, for example
	// https://host/friends. Media is read at PublicURL + "/media/<key>".
	PublicURL string
}

func (d *DirMediaStore) path(key string) (string, error) {
	if !validMediaKey(key) {
		return "", errors.New("invalid media key")
	}
	return filepath.Join(d.Dir, key), nil
}

func (d *DirMediaStore) Put(_ context.Context, key, _ string, path string, _ int64, _ string) error {
	target, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	staging, err := os.CreateTemp(d.Dir, ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(staging.Name())
	if _, err := io.Copy(staging, source); err != nil {
		staging.Close()
		return err
	}
	if err := staging.Close(); err != nil {
		return err
	}
	return os.Rename(staging.Name(), target)
}

func (d *DirMediaStore) Delete(_ context.Context, key string) error {
	target, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (d *DirMediaStore) URL(key string) string {
	return strings.TrimRight(d.PublicURL, "/") + "/media/" + key
}

func (d *DirMediaStore) Open(key string) (*os.File, error) {
	target, err := d.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

// validMediaKey is <32 lowercase hex>.<jpg|png|webp|mp4|webm>: no separators,
// so a key is never a path.
func validMediaKey(key string) bool {
	name, ext, ok := strings.Cut(key, ".")
	if !ok || len(name) != 32 {
		return false
	}
	if _, err := hex.DecodeString(name); err != nil || strings.ToLower(name) != name {
		return false
	}
	switch ext {
	case "jpg", "png", "webp", "mp4", "webm":
		return true
	}
	return false
}

// S3MediaStore keeps media in an S3-compatible bucket (Cloudflare R2 in
// production) that is publicly readable through PublicBaseURL, a hostname of
// its own, so user uploads never share an origin with the account service.
type S3MediaStore struct {
	// Endpoint is the S3 API address, for example
	// https://<account>.r2.cloudflarestorage.com. Requests are path-style.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// PublicBaseURL is where objects are read: the bucket's public custom
	// domain, with no bucket in the path.
	PublicBaseURL string
	HTTP          *http.Client
	Now           func() time.Time
}

func (s *S3MediaStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *S3MediaStore) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (s *S3MediaStore) objectURL(key string) string {
	return strings.TrimRight(s.Endpoint, "/") + "/" + s.Bucket + "/" + key
}

func (s *S3MediaStore) URL(key string) string {
	return strings.TrimRight(s.PublicBaseURL, "/") + "/" + key
}

func (s *S3MediaStore) Open(string) (*os.File, error) { return nil, os.ErrNotExist }

func (s *S3MediaStore) Put(ctx context.Context, key, contentType, path string, size int64, sha256Hex string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), file)
	if err != nil {
		return err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	s.sign(request, sha256Hex, s.now())
	return s.do(request)
}

func (s *S3MediaStore) Delete(ctx context.Context, key string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	s.sign(request, emptySHA256, s.now())
	err = s.do(request)
	var refused *s3Error
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return nil
	}
	return err
}

type s3Error struct {
	Status int
	Body   string
}

func (e *s3Error) Error() string {
	return fmt.Sprintf("object store answered %d: %s", e.Status, e.Body)
}

func (s *S3MediaStore) do(request *http.Request) error {
	response, err := s.client().Do(request)
	if err != nil {
		return fmt.Errorf("reach the object store: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
	if response.StatusCode >= 300 {
		return &s3Error{Status: response.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return nil
}

var emptySHA256 = func() string { sum := sha256.Sum256(nil); return hex.EncodeToString(sum[:]) }()

// sign adds AWS Signature Version 4 headers (service s3) to request with the
// given payload hash. Every header on the request is signed, so none can be
// changed in transit.
func (s *S3MediaStore) sign(request *http.Request, payloadHash string, now time.Time) {
	now = now.UTC()
	stamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	request.Header.Set("x-amz-date", stamp)
	request.Header.Set("x-amz-content-sha256", payloadHash)

	headers := map[string]string{"host": request.URL.Host}
	for name, values := range request.Header {
		headers[strings.ToLower(name)] = strings.TrimSpace(strings.Join(values, ","))
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonical := strings.Join([]string{
		request.Method, s3EscapePath(request.URL.Path), request.URL.RawQuery,
		canonicalHeaders.String(), signedHeaders, payloadHash,
	}, "\n")
	region := s.Region
	if region == "" {
		region = "auto"
	}
	scope := date + "/" + region + "/s3/aws4_request"
	digest := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(digest[:])
	key := hmacSHA256([]byte("AWS4"+s.SecretKey), date)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// s3EscapePath percent-encodes each path segment the way SigV4 wants it for S3
// (everything except unreserved characters; slashes stay).
func s3EscapePath(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = strings.ReplaceAll(url.PathEscape(segment), "+", "%2B")
	}
	return strings.Join(segments, "/")
}
