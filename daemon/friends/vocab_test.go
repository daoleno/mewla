package friends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectVocabularyReadsOnlyNamesAndCatchesThem(t *testing.T) {
	root := t.TempDir()
	state, projects := filepath.Join(root, "state"), filepath.Join(root, "ws")
	write(t, filepath.Join(state, "work", "krakenfarm", "x"), "")
	write(t, filepath.Join(projects, "nightowl-api", ".git", "config"),
		"[remote \"origin\"]\n\turl = git@github.com:hollowtree-labs/nightowl-api.git\n")
	// A project Watch knows about outside every usual root.
	write(t, filepath.Join(state, "watch", "projects.json"),
		`[{"id":"p1","name":"Quartz Lantern","root":"/elsewhere/deep/saffron-rig","remotes":[{"name":"origin","url":"https://github.com/mossbank-co/saffron-rig.git"}],"github":{"owner":"mossbank-co","repo":"saffron-rig"}}]`)
	write(t, filepath.Join(state, "integrations.json"),
		`{"a":{"account":{"name":"Hollowtree Linear","identity":"wren-example"},"grants":{"x":"y"}}}`)
	write(t, filepath.Join(state, "computers", "computers.json"),
		`{"peers":[{"name":"Studio Rack","urls":["https://rack7.example.net:9876"],"client_seed_hex":"SECRET-SEED-VALUE"}]}`)
	write(t, filepath.Join(state, "addresses.json"), `{"entries":[{"url":"https://gateway.tailnet-xyz.ts.net"}]}`)
	write(t, filepath.Join(state, "integration-credentials.json"), `{"secrets":{"a":"SECRET-TOKEN-VALUE"}}`)

	v := CollectVocabulary(Machine{
		StateDir: state, HomeDir: filepath.Join(root, "luna-home"), ProjectRoots: []string{projects},
		Hostname: func() (string, error) { return "orchard-9.example.test", nil }, Owner: "wren-example",
	})
	for _, want := range []string{"krakenfarm", "nightowl-api", "hollowtree-labs", "Hollowtree Linear", "Studio Rack", "rack7", "orchard-9", "luna-home", "tailnet-xyz", "Quartz Lantern", "saffron-rig", "mossbank-co"} {
		found := false
		for _, term := range v.Terms {
			if term == want {
				found = true
			}
		}
		if !found {
			t.Errorf("vocabulary lacks %q", want)
		}
	}
	for _, term := range v.Terms {
		if strings.Contains(term, "SECRET") {
			t.Errorf("vocabulary took a secret: %q", term)
		}
	}
	scrubber := &Scrubber{Vocabulary: v}
	for _, text := range []string{
		"Moved krakenfarm to the new box.", "Fixed nightowl API tests.", "Synced Studio Rack.",
		"hollowtree labs wanted it Friday.", "ssh to orchard 9 failed.",
		"Rebuilt saffron rig overnight.", "mossbank co asked for a demo.", "Quartz Lantern shipped.",
	} {
		if len(scrubber.Check(text)) == 0 {
			t.Errorf("not caught: %q", text)
		}
	}
	if len(scrubber.Check("Thanks wren-example for the idea.")) != 0 {
		t.Error("the owner's public handle was refused")
	}
}
