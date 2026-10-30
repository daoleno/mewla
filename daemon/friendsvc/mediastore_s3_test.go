package friendsvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The AWS documentation's example signing key derivation and "get-vanilla"
// style vector, kept as a regression test for the hand-written signer: the
// signature below is what AWS's own tooling computes for these inputs.
func TestS3SignerMatchesAWSSignatureVersion4Vector(t *testing.T) {
	store := &S3MediaStore{Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	request, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	request.Header.Set("Range", "bytes=0-9")
	store.sign(request, emptySHA256, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	// From "Example: GET Object" in the S3 SigV4 header-auth documentation.
	want := "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	got := request.Header.Get("Authorization")
	if !strings.HasSuffix(got, "Signature="+want) || !strings.Contains(got, "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date") {
		t.Fatalf("Authorization = %s\nwant signature %s", got, want)
	}
}

// Needs a real S3 API: set MEWLA_TEST_S3_ENDPOINT (and the keys) to run, for
// example against a local MinIO.
func TestS3MediaStoreAgainstARealBucket(t *testing.T) {
	endpoint := os.Getenv("MEWLA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("MEWLA_TEST_S3_ENDPOINT is not set")
	}
	store := &S3MediaStore{Endpoint: endpoint, Region: "us-east-1", Bucket: "friends-media-test",
		AccessKey: os.Getenv("MEWLA_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("MEWLA_TEST_S3_SECRET_KEY"),
		PublicBaseURL: endpoint + "/friends-media-test"}
	ctx := context.Background()
	// The bucket is made by the test harness; make it here if it is missing.
	create, _ := http.NewRequestWithContext(ctx, http.MethodPut, endpoint+"/friends-media-test", nil)
	store.sign(create, emptySHA256, time.Now())
	if response, err := http.DefaultClient.Do(create); err == nil {
		response.Body.Close()
	}

	data := append([]byte("\xff\xd8\xff"), []byte(strings.Repeat("jpeg-ish bytes ", 1000))...)
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	key := "0123456789abcdef0123456789abcdef.jpg"
	if err := store.Put(ctx, key, "image/jpeg", path, int64(len(data)), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// MinIO's bucket is private by default; read back with a signed GET.
	get, _ := http.NewRequestWithContext(ctx, http.MethodGet, store.objectURL(key), nil)
	store.sign(get, emptySHA256, time.Now())
	response, err := http.DefaultClient.Do(get)
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/jpeg" || response.Header.Get("Cache-Control") == "" {
		t.Fatalf("read after Put: %v %v", err, response)
	}
	response.Body.Close()
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("a second Delete must succeed: %v", err)
	}
	get, _ = http.NewRequestWithContext(ctx, http.MethodGet, store.objectURL(key), nil)
	store.sign(get, emptySHA256, time.Now())
	response, err = http.DefaultClient.Do(get)
	if err != nil || response.StatusCode != 404 {
		t.Fatalf("read after Delete: %v %v", err, response)
	}
	response.Body.Close()
	// A wrong secret is refused.
	bad := *store
	bad.SecretKey = "not-the-secret"
	if err := bad.Put(ctx, key, "image/jpeg", path, int64(len(data)), hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("Put with a wrong secret succeeded")
	}
}
