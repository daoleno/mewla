package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGCPUpdateStagesOfficialUpdaterAndRollsBack(t *testing.T) {
	for _, fault := range []string{"none", "updater", "version", "smoke"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			sdk := filepath.Join(dir, "versions", "old", "google-cloud-sdk")
			old := filepath.Join(sdk, "bin", "gcloud")
			os.MkdirAll(filepath.Dir(old), 0700)
			os.WriteFile(old, []byte("old"), 0700)
			os.MkdirAll(filepath.Join(dir, "bin"), 0700)
			os.Symlink(old, filepath.Join(dir, "bin", "gcloud"))
			client := &http.Client{Transport: gcpInstallTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://dl.google.com/dl/cloudsdk/channels/rapid/components-2.json" {
					t.Fatal("unexpected metadata source")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"592.0.0"}`))}, nil
			})}
			t.Setenv("CLOUDSDK_CONFIG", "/person/config")
			updated := false
			run := func(_ context.Context, binary string, args, env []string) (cloudOutput, error) {
				if binary == filepath.Join(dir, "bin", "gcloud") {
					return cloudOutput{Stdout: `{"Google Cloud SDK":"591.0.0"}`}, nil
				}
				for _, entry := range env {
					if entry == "CLOUDSDK_CONFIG=/person/config" {
						t.Fatal("updater could mutate user config")
					}
				}
				if binary == "cp" {
					if args[1] != sdk {
						t.Fatal("copied wrong SDK")
					}
					os.MkdirAll(filepath.Join(args[2], "bin"), 0700)
					os.WriteFile(filepath.Join(args[2], "bin", "gcloud"), []byte("new"), 0700)
					return cloudOutput{}, nil
				}
				if !strings.HasPrefix(binary, filepath.Join(dir, "versions", "sdk-")) {
					t.Fatal("updater mutates active SDK", binary)
				}
				if args[0] == "components" {
					if strings.Join(args, " ") != "components update --quiet" {
						t.Fatal(args)
					}
					updated = true
					if fault == "updater" {
						return cloudOutput{}, errors.New("injected failed component download")
					}
					return cloudOutput{}, nil
				}
				if args[0] == "version" {
					if !updated {
						t.Fatal("candidate was not updated")
					}
					version := "592.0.0"
					if fault == "version" {
						version = "591.0.0"
					}
					data, _ := json.Marshal(map[string]string{"Google Cloud SDK": version})
					return cloudOutput{Stdout: string(data)}, nil
				}
				if fault == "smoke" {
					return cloudOutput{ExitCode: 1}, nil
				}
				return cloudOutput{Stdout: `{"installation":{"sdk_root":"fixture"}}`}, nil
			}
			err := updateGCPSDK(context.Background(), dir, gcpArchives["linux/amd64"], client, run)
			active, _ := filepath.EvalSymlinks(filepath.Join(dir, "bin", "gcloud"))
			if fault == "none" {
				if err != nil || active == old {
					t.Fatal("update not activated", err)
				}
			} else {
				if err == nil || active != old {
					t.Fatal("failed update did not roll back", err)
				}
				stages, _ := filepath.Glob(filepath.Join(dir, "versions", "sdk-*"))
				if len(stages) != 0 {
					t.Fatal("failed staging leaked")
				}
			}
			if data, err := os.ReadFile(old); err != nil || string(data) != "old" {
				t.Fatal("running SDK was changed")
			}
		})
	}
}
func TestGcloudVersionMinimum(t *testing.T) {
	for _, version := range []string{"399.0.0", "400.0.0", "591.0.0", "broken"} {
		run := func(context.Context, string, []string, []string) (cloudOutput, error) {
			data, _ := json.Marshal(map[string]string{"Google Cloud SDK": version})
			return cloudOutput{Stdout: string(data)}, nil
		}
		_, err := cloudExecutableVersion(context.Background(), "gcp", "fixture", run, nil)
		if (err == nil) != (version == "400.0.0" || version == "591.0.0") {
			t.Fatal(version, err)
		}
	}
}
