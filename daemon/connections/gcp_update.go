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
	"sync"
	"time"
)

var gcpUpdateMu sync.Mutex

// The digest-pinned archive is a bootstrap for Google's official component
// updater, not a pin on the version users run. Each update uses a private copy;
// neither the active SDK nor the person's gcloud configuration is mutated.
func updateGCPSDK(ctx context.Context, dir string, pkg gcpArchive, client *http.Client, run func(context.Context, string, []string, []string) (cloudOutput, error)) error {
	gcpUpdateMu.Lock()
	defer gcpUpdateMu.Unlock()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://dl.google.com/dl/cloudsdk/channels/rapid/components-2.json", nil)
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var manifest struct {
		Version string `json:"version"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&manifest) != nil || !cloudVersionNumber.MatchString(manifest.Version) {
		return errors.New("Could not check the official gcloud release")
	}
	current := filepath.Join(dir, "bin", "gcloud")
	env := gcloudCLI{}.environment()
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	version, versionErr := cloudExecutableVersion(probe, "gcp", current, run, env)
	cancel()
	if versionErr == nil && !cloudVersionLess(version, manifest.Version) {
		return nil
	}
	generation, lease, err := newCloudGeneration(dir, "sdk-")
	if err != nil {
		return err
	}
	defer closeCloudLease(lease)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(generation)
		}
	}()
	// Install/update commands may write logs or SDK settings; keep those away
	// from the real credentials. Normal commands retain CLOUDSDK_CONFIG unchanged.
	config := filepath.Join(generation, "setup-config")
	if err = os.Mkdir(config, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(config)
	env = cloudInstallEnvironment(env, config)
	sdk := filepath.Join(generation, "google-cloud-sdk")
	if versionErr == nil {
		binary, err := filepath.EvalSymlinks(current)
		if err != nil {
			return err
		}
		source := filepath.Dir(filepath.Dir(binary))
		out, err := run(ctx, "cp", []string{"-R", source, sdk}, env)
		if err != nil || out.ExitCode != 0 {
			return errors.New("Could not stage the gcloud update")
		}
	} else {
		// Bootstrap installer also receives an isolated configuration directory.
		isolatedRun := func(ctx context.Context, binary string, args, _ []string) (cloudOutput, error) {
			return run(ctx, binary, args, env)
		}
		if err = installGCPArchive(ctx, generation, pkg, client, isolatedRun); err != nil {
			return err
		}
	}
	binary := filepath.Join(sdk, "bin", "gcloud")
	// Google verifies component archives against its HTTPS manifest checksums.
	out, err := run(ctx, binary, []string{"components", "update", "--quiet"}, env)
	if err != nil || out.ExitCode != 0 {
		return errors.New("Could not update the staged gcloud SDK")
	}
	probe, cancel = context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	version, err = cloudExecutableVersion(probe, "gcp", binary, run, env)
	if err != nil || version != manifest.Version {
		return errors.New("Updated gcloud failed its version check")
	}
	out, err = run(probe, binary, []string{"info", "--format=json"}, env)
	var info map[string]json.RawMessage
	if err != nil || out.ExitCode != 0 || json.Unmarshal([]byte(out.Stdout), &info) != nil || len(info) == 0 {
		return errors.New("Updated gcloud failed its smoke test")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = activateCloudGeneration(dir, "gcloud", binary); err != nil {
		return err
	}
	published = true
	pruneCloudGenerations(dir, "gcloud")
	return nil
}
func cloudInstallEnvironment(env []string, config string) []string {
	out := []string{}
	for _, entry := range env {
		if !strings.HasPrefix(entry, "CLOUDSDK_CONFIG=") {
			out = append(out, entry)
		}
	}
	return append(out, "CLOUDSDK_CONFIG="+config)
}
