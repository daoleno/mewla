package modelprofiles

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LocalProvider is a model server found answering on this computer.
type LocalProvider struct {
	PresetID string `json:"preset_id"`
	Label    string `json:"label"`
	BaseURL  string `json:"base_url"`
	Models   int    `json:"models"`
	// ConnectionIDs are the connections that already point at it.
	ConnectionIDs []string `json:"connection_ids,omitempty"`
}

// localTarget is a port a local model server listens on by default.
type localTarget struct {
	PresetID string
	Port     int
}

// localTargets are the ports looked at, each its server's default. The
// daemon's own gateway (3425) is not one: it lists models too.
var localTargets = []localTarget{
	{ProviderPresetOllama, 11434},
	{ProviderPresetLMStudio, 1234},
	{ProviderPresetLocal, 8000}, // vLLM, SGLang
	{ProviderPresetLocal, 8080}, // llama.cpp's server
	{ProviderPresetLocal, 1337}, // Jan
}

const localProbeTimeout = 1500 * time.Millisecond

// DetectLocalProviders asks each default local port for its model list, all
// at once, and returns the servers that answered with one.
func (o *Owner) DetectLocalProviders(ctx context.Context) []LocalProvider {
	found := probeLocalProviders(ctx, NewSafeHTTPClient(localProbeTimeout), "localhost", localTargets)
	if o == nil || o.store == nil || len(found) == 0 {
		return found
	}
	for i := range found {
		for _, profile := range o.store.Catalog().Profiles {
			if isAccountConnection(profile) && sameLocalAddress(profile.BaseURL, found[i].BaseURL) {
				found[i].ConnectionIDs = append(found[i].ConnectionIDs, profile.ID)
			}
		}
	}
	return found
}

func probeLocalProviders(ctx context.Context, client *http.Client, host string, targets []localTarget) []LocalProvider {
	results := make([]*LocalProvider, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := "http://" + host + ":" + strconv.Itoa(target.Port)
			if n, ok := localModelCount(ctx, client, base); ok {
				label := target.PresetID
				if spec, known := lookupPreset(target.PresetID); known {
					label = spec.Public.Label
				}
				results[i] = &LocalProvider{PresetID: target.PresetID, Label: label, BaseURL: base, Models: n}
			}
		}()
	}
	wg.Wait()
	var out []LocalProvider
	for _, r := range results {
		if r != nil {
			out = append(out, *r)
		}
	}
	return out
}

// localModelCount reads base's OpenAI model list: {"data": [{"id": …}, …]}.
// Anything else (no server, a web page, another kind of API) is no server.
func localModelCount(ctx context.Context, client *http.Client, base string) (int, bool) {
	ctx, cancel := context.WithTimeout(ctx, localProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		return 0, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, false
	}
	var list struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &list) != nil || list.Data == nil {
		return 0, false
	}
	n := 0
	for _, m := range *list.Data {
		if strings.TrimSpace(m.ID) != "" {
			n++
		}
	}
	return n, true
}

// sameLocalAddress reports whether two base URLs reach the same local port,
// localhost and 127.0.0.1 alike, a trailing /v1 or not.
func sameLocalAddress(a, b string) bool {
	norm := func(u string) string {
		u = strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/")
		u = strings.TrimSuffix(u, "/v1")
		u = strings.Replace(u, "://127.0.0.1:", "://localhost:", 1)
		u = strings.Replace(u, "://[::1]:", "://localhost:", 1)
		return u
	}
	return norm(a) != "" && norm(a) == norm(b)
}
