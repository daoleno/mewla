package modelprofiles

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestClaudeRouteEndpointRequiresExplicitLocalRoute(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		want      bool
	}{
		{"managed", "http://127.0.0.1:45653/r/rt_owned", true},
		{"ipv6", "http://[::1]:45653/r/rt_owned", true},
		{"upstream", "https://api.example.test/r/rt_owned", false},
		{"wildcard", "http://0.0.0.0:45653/r/rt_owned", false},
		{"hostname", "http://localhost:45653/r/rt_owned", false},
		{"ephemeral", "http://127.0.0.1:0/r/rt_owned", false},
		{"path", "http://127.0.0.1:45653/v1", false},
		{"nested", "http://127.0.0.1:45653/r/rt_owned/v1", false},
		{"credentials", "http://user:pass@127.0.0.1:45653/r/rt_owned", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"env": map[string]string{EnvAnthropicBaseURL: tc.url}})
			_, ok := claudeRouteEndpoint([]string{"/usr/bin/claude", "--settings", string(raw)})
			if ok != tc.want {
				t.Fatalf("accepted=%v want=%v", ok, tc.want)
			}
			if _, ok := claudeRouteEndpoint([]string{"unrelated", "--settings", string(raw)}); ok {
				t.Fatal("unrelated process admitted")
			}
		})
	}
}

func TestRestartRestoresLiveClientPortAlongsidePersistedPort(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	cfg := OwnerConfig{ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener, Lookup: readyLookup("fixture"), Verifier: lifecycleTestVerifier{}}
	owner, err := StartOwner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := owner.UpsertProfile(codexResponsesProfile("a", "gpt-5", "up-a"), 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "live:@1"); err != nil {
		t.Fatal(err)
	}
	originalAddr := owner.ListenAddr()
	routeID := plan.State.Binding.RouteID
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	// Model the incident: shared listener metadata was overwritten while a
	// surviving provider still has originalAddr embedded in its launch argv.
	replacement, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	replacementAddr := replacement.Addr().String()
	_ = replacement.Close()
	lf, _ := NewListenerFile(listener)
	if err := lf.Save(replacementAddr); err != nil {
		t.Fatal(err)
	}
	cfg.LiveRouteEndpoints = func() ([]LiveRouteEndpoint, error) {
		return []LiveRouteEndpoint{{routeID, originalAddr}, {routeID, originalAddr}, {"rt_foreign", "127.0.0.1:1"}}, nil
	}
	for range 2 {
		restored, err := StartOwner(cfg)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Timeout: time.Second}
		for _, addr := range []string{originalAddr, replacementAddr} {
			response, err := client.Get("http://" + addr + "/r/" + routeID + "/v1/models")
			if err != nil {
				_ = restored.Close()
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				_ = restored.Close()
				t.Fatalf("%s status=%d", addr, response.StatusCode)
			}
		}
		if err := restored.Close(); err != nil {
			t.Fatal(err)
		}
		for _, addr := range []string{originalAddr, replacementAddr} {
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				t.Fatalf("listener leaked on %s: %v", addr, err)
			}
			_ = ln.Close()
		}
	}
}
