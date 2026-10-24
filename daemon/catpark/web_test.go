package catpark

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

func TestBundleHasEveryLineAndPicture(t *testing.T) {
	files := Bundle()
	raw, err := fs.ReadFile(files, "lines.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	var lines map[string]map[Kind]map[string][]string
	if err := json.Unmarshal([]byte(text[strings.Index(text, "{"):strings.LastIndex(text, "}")+1]), &lines); err != nil {
		t.Fatal(err)
	}
	exists := func(name string) {
		t.Helper()
		if _, err := fs.Stat(files, name); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	for _, language := range []string{"zh", "en"} {
		for _, kind := range Kinds {
			set := lines[language][kind]
			if len(set[PhaseStart]) != StartVariants || len(set[PhaseLong]) != LongVariants {
				t.Errorf("%s/%s: want %d start and %d long lines, have %d and %d", language, kind, StartVariants, LongVariants, len(set[PhaseStart]), len(set[PhaseLong]))
			}
			for _, group := range set {
				for _, line := range group {
					if strings.TrimSpace(line) == "" {
						t.Errorf("%s/%s has an empty line", language, kind)
					}
				}
			}
		}
	}
	for _, kind := range Kinds {
		exists("a/pc_" + string(kind) + ".jpg")
		exists("a/sv_" + SouvenirItem(kind) + ".webp")
	}
	for _, treat := range TreatKinds {
		exists("a/t_" + treat + ".webp")
	}
	for _, name := range []string{"index.html", "game.js", "game.css", "lines.js", "hand.woff2", "a/sign_bubble.webp",
		"a/yard_day.jpg", "a/yard_night.jpg", "a/home_nocat.jpg", "a/away.jpg", "a/night.jpg", "a/cat_home.webp", "a/cat_night.webp"} {
		exists(name)
	}
}

func TestManifestChangesWithContent(t *testing.T) {
	manifest, err := BuildManifest(Bundle())
	if err != nil || manifest.Version == "" || len(manifest.Files) < 40 {
		t.Fatalf("manifest: %v %d files", err, len(manifest.Files))
	}
	for _, file := range manifest.Files {
		if len(file.SHA256) != 64 || file.Size <= 0 {
			t.Fatalf("bad entry %+v", file)
		}
	}
}
