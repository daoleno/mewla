package catpark

import (
	"encoding/json"
	"io/fs"
	"regexp"
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
	object := func(from, to string) string {
		start := strings.Index(text, from)
		end := len(text)
		if to != "" {
			end = strings.Index(text, to)
		}
		part := text[start:end]
		return part[strings.Index(part, "{") : strings.LastIndex(part, "}")+1]
	}
	var lines map[string]map[Kind]map[string][]string
	if err := json.Unmarshal([]byte(object("window.CAT_LINES", "window.CAT_UI")), &lines); err != nil {
		t.Fatal(err)
	}
	var words map[string]map[string]string
	if err := json.Unmarshal([]byte(object("window.CAT_UI", "")), &words); err != nil {
		t.Fatal(err)
	}
	// Every name a screen reader hears in the page is spoken in both languages.
	page, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`id="([^"]+)"[^>]*aria-label="`).FindAllStringSubmatch(string(page), -1) {
		for _, language := range []string{"zh", "en"} {
			if strings.TrimSpace(words[language][match[1]]) == "" {
				t.Errorf("%s has no %s name", match[1], language)
			}
		}
	}
	for _, key := range []string{"home"} {
		for _, language := range []string{"zh", "en"} {
			if words[language][key] == "" {
				t.Errorf("no %s name for %s", language, key)
			}
		}
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
		"a/yard_day.jpg", "a/yard_night.jpg", "a/home_nocat.jpg", "a/away.jpg", "a/night.jpg", "a/night_away.jpg", "a/cat_home.webp", "a/cat_night.webp"} {
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

func TestEveryStyleHasItsWholeRoomAndAName(t *testing.T) {
	files := Bundle()
	raw, err := fs.ReadFile(files, "lines.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	part := text[strings.Index(text, "window.CAT_STYLES"):]
	part = part[strings.Index(part, "{") : strings.Index(part, "};")+1]
	var names map[string]map[string]string
	if err := json.Unmarshal([]byte(part), &names); err != nil {
		t.Fatal(err)
	}
	page, err := fs.ReadFile(files, "game.js")
	if err != nil {
		t.Fatal(err)
	}
	listed := regexp.MustCompile(`var STYLES = \[([^\]]*)\]`).FindStringSubmatch(string(page))
	if listed == nil {
		t.Fatal("the page lists no styles")
	}
	want := make([]string, 0, len(Styles))
	for _, style := range Styles {
		want = append(want, "'"+string(style)+"'")
	}
	if strings.Join(want, ", ") != strings.TrimSpace(listed[1]) {
		t.Errorf("the page's styles %q differ from the daemon's %q", listed[1], strings.Join(want, ", "))
	}
	for _, style := range Styles {
		for _, language := range []string{"zh", "en"} {
			if strings.TrimSpace(names[language][string(style)]) == "" {
				t.Errorf("style %s has no %s name", style, language)
			}
		}
		slots := []string{"card"}
		if style != DefaultStyle {
			slots = []string{"home", "away", "night", "card"}
		}
		for _, slot := range slots {
			if _, err := fs.Stat(files, "a/s/"+string(style)+"/"+slot+".jpg"); err != nil {
				t.Errorf("style %s is missing its %s picture", style, slot)
			}
		}
	}
}
