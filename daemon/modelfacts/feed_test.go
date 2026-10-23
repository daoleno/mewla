package modelfacts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func countingServer(t *testing.T, body string, hits *atomic.Int64, hold <-chan struct{}) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if hold != nil {
			<-hold
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestReadersOfOneFeedShareOneDownload(t *testing.T) {
	var hits atomic.Int64
	server := countingServer(t, `{"openai":{"models":{}}}`, &hits, nil)
	path := filepath.Join(t.TempDir(), "models-dev.json")
	pricing := Open(Config{URL: server.URL, Path: path})
	picker := Open(Config{URL: server.URL, Path: path})
	if pricing != picker {
		t.Fatal("the same URL and file opened two feeds")
	}

	if !pricing.Stale(TTL) {
		t.Fatal("a feed nobody downloaded is fresh")
	}
	if _, _, err := pricing.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	raw, at := picker.Raw()
	if string(raw) != `{"openai":{"models":{}}}` || time.Since(at) > time.Minute {
		t.Fatalf("picker sees %q at %v", raw, at)
	}
	if picker.Stale(TTL) {
		t.Fatal("a feed downloaded just now is stale")
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
}

func TestConcurrentRefreshesShareOneRequest(t *testing.T) {
	var hits atomic.Int64
	release := make(chan struct{})
	server := countingServer(t, `{}`, &hits, release)
	feed := Open(Config{URL: server.URL, Path: filepath.Join(t.TempDir(), "f.json")})

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := feed.Refresh(context.Background(), nil)
			errs <- err
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
}

func TestADownloadOutlivesOneCallerButNotTheLastOne(t *testing.T) {
	var hits atomic.Int64
	release := make(chan struct{})
	server := countingServer(t, `{"a":1}`, &hits, release)
	feed := Open(Config{URL: server.URL, Path: filepath.Join(t.TempDir(), "f.json")})

	patient := make(chan error, 1)
	go func() {
		_, _, err := feed.Refresh(context.Background(), nil)
		patient <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := feed.Refresh(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
	close(release)
	if err := <-patient; err != nil {
		t.Fatalf("the patient caller lost the download: %v", err)
	}
	if raw, _ := feed.Raw(); string(raw) != `{"a":1}` {
		t.Fatalf("raw = %q", raw)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
}

func TestTheLastCallerGivingUpCancelsTheRequest(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "f.json")
	feed := Open(Config{URL: server.URL, Path: path})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := feed.Refresh(ctx, nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("the request outlived its last caller")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a cancelled download wrote the cache: %v", err)
	}
}

func TestTheCacheFileSurvivesARestart(t *testing.T) {
	var hits atomic.Int64
	server := countingServer(t, `{"x":{"models":{}}}`, &hits, nil)
	path := filepath.Join(t.TempDir(), "sub", "models-dev.json")
	first := &Feed{url: server.URL, path: path, client: http.DefaultClient}
	if _, _, err := first.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache file: %v %v", info, err)
	}

	second := &Feed{url: server.URL, path: path, client: http.DefaultClient}
	raw, at := second.Raw()
	if string(raw) != `{"x":{"models":{}}}` || second.Stale(TTL) || time.Since(at) > time.Minute {
		t.Fatalf("restarted feed = %q at %v", raw, at)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
	old := time.Now().Add(-2 * TTL)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	third := &Feed{url: server.URL, path: path, client: http.DefaultClient}
	if !third.Stale(TTL) {
		t.Fatal("a cache file older than the TTL is fresh")
	}
}

func TestACorruptCacheFileIsNoCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-dev.json")
	if err := os.WriteFile(path, []byte(`{"truncated`), 0o600); err != nil {
		t.Fatal(err)
	}
	feed := &Feed{path: path, client: http.DefaultClient}
	if raw, at := feed.Raw(); raw != nil || !at.IsZero() || !feed.Stale(TTL) {
		t.Fatalf("raw=%q at=%v", raw, at)
	}
}

func TestBadAnswersKeepThePreviousCatalog(t *testing.T) {
	var mode atomic.Value
	mode.Store("ok")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case "status":
			http.Error(w, "down", http.StatusBadGateway)
		case "garbage":
			_, _ = w.Write([]byte(`not json`))
		case "huge":
			_, _ = w.Write([]byte(`{"pad":"` + strings.Repeat("x", MaxBytes) + `"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()
	feed := &Feed{url: server.URL, path: filepath.Join(t.TempDir(), "f.json"), client: server.Client()}
	if _, _, err := feed.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	mode.Store("status")
	var status *StatusError
	if _, _, err := feed.Refresh(context.Background(), nil); !errors.As(err, &status) || status.Code != http.StatusBadGateway {
		t.Fatalf("err = %v", err)
	}
	mode.Store("garbage")
	if _, _, err := feed.Refresh(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v", err)
	}
	mode.Store("huge")
	if _, _, err := feed.Refresh(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("err = %v", err)
	}
	if raw, _ := feed.Raw(); string(raw) != `{"ok":true}` {
		t.Fatalf("a failed refresh replaced the catalog with %.40q", raw)
	}
}

func TestAFeedWithoutAURLNeverDownloads(t *testing.T) {
	feed := &Feed{client: http.DefaultClient}
	if _, _, err := feed.Refresh(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err = %v", err)
	}
}

func TestARejectedCatalogReplacesNeitherTheFileNorTheCopy(t *testing.T) {
	var body atomic.Value
	const good = `{"good":{"models":{"m":{"name":"M"}}}}`
	body.Store(good)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "f.json")
	feed := &Feed{url: server.URL, path: path, client: server.Client()}
	needsGood := func(catalog *Catalog) error {
		if _, ok := catalog.Model("good", "m"); !ok {
			return errors.New("no supported rates")
		}
		return nil
	}
	if _, _, err := feed.Refresh(context.Background(), needsGood); err != nil {
		t.Fatal(err)
	}
	body.Store(`{}`)
	if _, _, err := feed.Refresh(context.Background(), needsGood); err == nil || !strings.Contains(err.Error(), "no supported rates") {
		t.Fatalf("err = %v", err)
	}
	if raw, _ := feed.Raw(); string(raw) != good {
		t.Fatalf("copy = %q", raw)
	}
	if onDisk, _ := os.ReadFile(path); string(onDisk) != good {
		t.Fatalf("file = %q", onDisk)
	}
}
