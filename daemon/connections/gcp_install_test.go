package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type gcpInstallTransport func(*http.Request) (*http.Response, error)

func (f gcpInstallTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGCPInstallFailureCleanupAndRetry(t *testing.T) {
	for _, fault := range []string{"offline", "blocked", "checksum", "interrupted-download", "partial-install", "interrupted-previous-install", "publish"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			payload := "verified fixture archive"
			sum := sha256.Sum256([]byte(payload))
			pkg := gcpArchive{"linux-x86_64", hex.EncodeToString(sum[:])}
			mode := fault
			client := &http.Client{Transport: gcpInstallTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "dl.google.com" || !strings.Contains(req.URL.Path, "591.0.0") {
					t.Fatal("not pinned official download")
				}
				if mode == "offline" {
					return nil, errors.New("network offline")
				}
				status := 200
				body := io.ReadCloser(io.NopCloser(strings.NewReader(payload)))
				if mode == "blocked" {
					status = 403
				}
				if mode == "checksum" {
					body = io.NopCloser(strings.NewReader("corrupt"))
				}
				if mode == "interrupted-download" {
					body = io.NopCloser(gcpBrokenReader{})
				}
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{}}, nil
			})}
			run := func(_ context.Context, binary string, args, env []string) (cloudOutput, error) {
				switch filepath.Base(binary) {
				case "tar":
					stage := filepath.Join(args[3], "google-cloud-sdk")
					os.MkdirAll(filepath.Join(stage, "bin"), 0700)
					os.WriteFile(filepath.Join(stage, "bin", "gcloud"), []byte("fixture"), 0700)
				case "install.sh":
					if mode == "partial-install" {
						return cloudOutput{}, errors.New("interrupted after extracting SDK")
					}
					if mode == "publish" {
						os.WriteFile(filepath.Join(dir, "bin"), []byte("block publication"), 0600)
					}
				case "gcloud":
					return cloudOutput{Stdout: `{"Google Cloud SDK":"591.0.0"}`}, nil
				default:
					t.Fatalf("unexpected process %s", binary)
				}
				return cloudOutput{}, nil
			}
			if mode == "interrupted-previous-install" {
				os.MkdirAll(filepath.Join(dir, "google-cloud-sdk"), 0700)
				os.WriteFile(filepath.Join(dir, "google-cloud-sdk", "partial"), []byte("partial"), 0600)
				os.MkdirAll(filepath.Join(dir, "install-crashed"), 0700)
				os.WriteFile(filepath.Join(dir, "install-crashed", "partial"), []byte("partial"), 0600)
				mode = "offline"
			}
			err := installGCPArchive(context.Background(), dir, pkg, client, run)
			if err == nil || strings.ContainsAny(err.Error(), "\r\n") {
				t.Fatal("expected one-line failure", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "google-cloud-sdk")); !os.IsNotExist(err) {
				t.Fatal("SDK was published on failure")
			}
			stages, _ := filepath.Glob(filepath.Join(dir, "install-*"))
			if len(stages) != 0 {
				t.Fatal("staging directories leaked")
			}
			if _, err := os.Stat(filepath.Join(dir, "bin", "gcloud")); err == nil {
				t.Fatal("broken executable was published")
			}
			if mode == "publish" {
				os.Remove(filepath.Join(dir, "bin"))
			}
			mode = "normal"
			if err := installGCPArchive(context.Background(), dir, pkg, client, run); err != nil {
				t.Fatal("retry failed", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "bin", "gcloud")); err != nil {
				t.Fatal("retry did not publish CLI", err)
			}
		})
	}
}

type gcpBrokenReader struct{}

func (gcpBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestGCPInstallDownloadHasDeadline(t *testing.T) {
	client := &http.Client{Transport: gcpInstallTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := installGCPArchive(ctx, t.TempDir(), gcpArchives["linux/amd64"], client, runCloudProcess)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("download did not terminate on deadline")
	}
}

type installFlowGcloud struct {
	gcloudCLI
	manual bool
	fail   bool
}

func (p *installFlowGcloud) binary() string         { return "gcloud-install-test-not-on-path" }
func (p *installFlowGcloud) installPaths() []string { return nil }
func (p *installFlowGcloud) installSupport() (string, string) {
	if p.manual {
		return gcpInstallSupport("unsupported/test")
	}
	return "", ""
}
func (p *installFlowGcloud) prepareLogin(*Manager, context.Context, string) error { return nil }
func (p *installFlowGcloud) install(_ context.Context, dir string) error {
	if p.fail {
		return errors.New("Couldn't download gcloud. Check your connection and retry.")
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "bin", p.binary()), []byte("#!/bin/sh\nprintf '%s\\n' 'https://accounts.google.com/o/oauth2/auth?client_id=vendor&response_type=code'\nread code\n"), 0700)
}
func TestCloudInstallRetryAndManualCheckWithoutRestart(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry", true: "manual"}[manual], func(t *testing.T) {
			m, _, _, _ := testCloudManager(t)
			defer m.Close()
			p := &installFlowGcloud{manual: manual, fail: true}
			m.cloud.providers["gcp"] = p
			f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gcp"}}).Flow
			if !f.InstallRequired || f.InstallManual != manual {
				t.Fatal("missing install card")
			}
			q := Request{Action: "connect_finish", FlowID: f.ID}
			if manual {
				if f.InstallURL != gcpInstallDocs || !strings.Contains(f.Message, "Google Cloud CLI (gcloud)") {
					t.Fatal("manual directions missing")
				}
				again := mustHandle(t, m, q).Flow
				if !again.InstallManual || again.Installing {
					t.Fatal("check again started an unsupported install")
				}
				p.fail = false
				if err := p.install(context.Background(), filepath.Join(m.cloud.dir, "gcp")); err != nil {
					t.Fatal(err)
				}
			} else {
				installing := mustHandle(t, m, q).Flow
				if !installing.Installing {
					t.Fatal("not installing")
				}
				<-m.connectFlows[f.ID].cli.done
				failed := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID}).Flow
				if !failed.InstallRequired || !failed.InstallFailed || failed.Installing || failed.Status != "waiting" {
					t.Fatal("failed install cannot retry", failed)
				}
				p.fail = false
			}
			result := mustHandle(t, m, q).Flow
			if result.Installing {
				<-m.connectFlows[f.ID].cli.done
				result = mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID}).Flow
			}
			if !result.CodeRequired || result.InstallRequired || result.Installing || result.ID != f.ID {
				t.Fatal("retry/check did not advance directly into remote login", result)
			}
		})
	}
}
