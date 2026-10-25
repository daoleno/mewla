package catpark

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// script reads the JSON object a bundle script assigns to a window global.
func script(t *testing.T, files fs.FS, name, global string, into any) {
	t.Helper()
	raw, err := fs.ReadFile(files, name)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	at := strings.Index(text, global)
	if at < 0 {
		t.Fatalf("%s does not set %s", name, global)
	}
	body := strings.TrimSpace(text[at+len(global):])
	body = strings.TrimSpace(strings.TrimPrefix(body, "="))
	body = body[:strings.LastIndex(body, "}")+1]
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

type kitFile struct {
	Meta map[string][]float64 `json:"meta"`
	Acts []struct {
		ID string `json:"id"`
		A  string `json:"a"`
		B  string `json:"b"`
		// N is the night picture of a place in the home with the cat painted in
		N string `json:"n"`
	} `json:"acts"`
	Cards map[string]struct {
		F string `json:"f"`
	} `json:"cards"`
	Yard map[string]struct {
		A string `json:"a"`
		B string `json:"b"`
	} `json:"yard"`
	Props map[string]string    `json:"props"`
	Spots map[string][]float64 `json:"spots"`
	Sit   string               `json:"sit"`
	Ready string               `json:"ready"`
	// Slots are the parts of the cat's day, in order
	Slots []string `json:"slots"`
}

func readKit(t *testing.T, files fs.FS) kitFile {
	t.Helper()
	var kit kitFile
	script(t, files, "kit.js", "window.CAT_KIT", &kit)
	return kit
}

func existsIn(t *testing.T, files fs.FS) func(string) {
	return func(name string) {
		t.Helper()
		if _, err := fs.Stat(files, name); err != nil {
			t.Errorf("missing %s", name)
		}
	}
}

// Every id in the catalog has its pictures and its words,
// so no postcard, prop or line can come out blank.
func TestCatalogIsWhole(t *testing.T) {
	files := Bundle()
	world := World()
	kit := readKit(t, files)
	exists := existsIn(t, files)
	for i := 1; i < len(world.Levels); i++ {
		if world.Levels[i] <= world.Levels[i-1] {
			t.Fatalf("levels must rise: %v", world.Levels)
		}
	}
	for _, kind := range Kinds {
		if !world.isHobby(world.HobbyOf(kind)) {
			t.Errorf("kind %s teaches no known hobby", kind)
		}
		if _, ok := world.Places[string(kind)]; !ok {
			t.Errorf("kind %s has no place", kind)
		}
		exists("a/sv_" + SouvenirItem(kind) + ".webp")
	}
	for _, rare := range world.Rare {
		if _, ok := world.Places[rare]; !ok {
			t.Errorf("rare place %s is not a place", rare)
		}
	}
	for _, hobby := range world.Hobbies {
		prop, inRoom := kit.Props[hobby]
		_, inYard := kit.Yard[hobby]
		_, atSpot := kit.Spots[hobby]
		if !inRoom && !inYard && !atSpot {
			t.Errorf("hobby %s has nowhere to be done", hobby)
		}
		if inRoom {
			exists("a/" + prop + ".webp")
		}
	}
	for id, place := range world.Places {
		exists("a/pl_" + id + ".jpg")
		if len(place.Spot) != 3 || len(place.Acts) == 0 {
			t.Errorf("place %s needs a spot and acts", id)
		}
		for _, v := range place.Spot {
			if v <= 0 || v >= 1 {
				t.Errorf("place %s spot %v is off the picture", id, place.Spot)
			}
		}
		for _, act := range place.Acts {
			card, ok := kit.Cards[act]
			if !ok {
				t.Errorf("place %s shows %s, which no postcard can draw", id, act)
				continue
			}
			exists("a/" + card.F + ".webp")
		}
		switch place.Hour {
		case "", "dawn", "day", "dusk", "night":
		default:
			t.Errorf("place %s has no such hour %q", id, place.Hour)
		}
	}
	for _, friend := range world.Friends {
		exists("a/f_" + friend + ".webp")
	}
	for _, food := range world.Foods {
		exists("a/t_" + food + ".webp")
	}
	words := world.Words
	for id, place := range world.Places {
		if strings.TrimSpace(words.Places[id]) == "" {
			t.Errorf("place %s has no name", id)
		}
		for _, act := range place.Acts {
			if len(words.Acts[act]) == 0 {
				t.Errorf("act %s has no line", act)
			}
			for _, line := range words.Acts[act] {
				if !strings.Contains(line, "{p}") {
					t.Errorf("%q does not say where", line)
				}
			}
		}
	}
	for _, friend := range world.Friends {
		if words.Friends[friend] == "" {
			t.Errorf("friend %s says nothing", friend)
		}
	}
	for _, weather := range world.Weathers {
		if weather != "clear" && words.Weathers[weather] == "" {
			t.Errorf("weather %s says nothing", weather)
		}
	}
	for _, hour := range []string{"dawn", "dusk", "night"} {
		if words.Times[hour] == "" {
			t.Errorf("no line for %s", hour)
		}
	}
	for _, food := range world.Foods {
		if words.Foods[food] == "" {
			t.Errorf("food %s has no name", food)
		}
	}
	for _, hobby := range world.Hobbies {
		if words.Hobbies[hobby] == "" {
			t.Errorf("hobby %s has no name", hobby)
		}
	}
	if words.Welcome == "" || len(words.Letter) == 0 || !strings.Contains(words.Treat, "{t}") {
		t.Errorf("the welcome note, letters and treat line must be written")
	}
	for key, needs := range map[string][]string{"note": {"{p}"}, "in": {"{n}"}, "soon": nil, "line": {"{p}", "{n}"}, "line_soon": {"{p}"}} {
		line := words.Away[key]
		if strings.TrimSpace(line) == "" {
			t.Errorf("the away note's %q line must be written", key)
		}
		for _, hole := range needs {
			if !strings.Contains(line, hole) {
				t.Errorf("the away note's %q line must say %s", key, hole)
			}
		}
	}
	for key, needs := range map[string][]string{"ask": nil, "hint": nil, "ok": nil, "sign": {"{n}"}} {
		line := words.Name[key]
		if strings.TrimSpace(line) == "" {
			t.Errorf("the name card's %q line must be written", key)
		}
		for _, hole := range needs {
			if !strings.Contains(line, hole) {
				t.Errorf("the name card's %q line must say %s", key, hole)
			}
		}
	}
}

// The diary has a line for every part of the day and every souvenir, and
// each line says what it is about.
func TestTheDiaryIsWritten(t *testing.T) {
	files := Bundle()
	kit, diary := readKit(t, files), World().Words.Diary
	if len(kit.Slots) == 0 {
		t.Fatal("the kit has no parts of the day")
	}
	for _, slot := range kit.Slots {
		if len(diary.Slots[slot]) == 0 {
			t.Errorf("the diary has nothing for the %s", slot)
		}
		for _, line := range diary.Slots[slot] {
			if strings.TrimSpace(line) == "" {
				t.Errorf("an empty diary line for the %s", slot)
			}
		}
	}
	for kind := range souvenirItems {
		if item := SouvenirItem(kind); diary.Brought[item] == "" {
			t.Errorf("the diary cannot say it brought home a %s", item)
		}
	}
	if diary.Title == "" || diary.Quiet == "" {
		t.Error("the diary needs its title and a quiet line")
	}
	for line, hole := range map[string]string{diary.Days: "{n}", diary.Hobby: "{h}", diary.Out: "{p}", diary.Back: "{t}", diary.Learnt: "{h}"} {
		if !strings.Contains(line, hole) {
			t.Errorf("diary line %q must say %s", line, hole)
		}
	}
}

// The room draws the cat in layers: every pose the house uses must be drawn,
// and nothing of the old style picker is left.
func TestHouseHasEveryPose(t *testing.T) {
	files := Bundle()
	kit := readKit(t, files)
	exists := existsIn(t, files)
	for _, act := range kit.Acts {
		// a quick reaction is one pose; a place in the home is a day and a night picture
		for i, frame := range []string{act.A, act.B, act.N} {
			if i > 0 && frame == "" {
				continue
			}
			if frame == "" || kit.Meta[frame] == nil {
				t.Errorf("act %s names the frame %q the kit has no place for", act.ID, frame)
				continue
			}
			exists("a/cat_" + frame + ".webp")
		}
	}
	for _, yard := range kit.Yard {
		exists("a/" + yard.A + ".webp")
		if yard.B != "" {
			exists("a/" + yard.B + ".webp")
		}
	}
	exists("a/cat_" + kit.Sit + ".webp")
	exists("a/cat_" + kit.Ready + ".webp")
	for _, name := range []string{"index.html", "game.js", "game.css", "life.js", "guide.js", "card.js", "sound.js", "catalog.js", "lines.js",
		"hand.woff2", "a/room_day.jpg", "a/room_night.jpg", "a/yard_day.jpg", "a/yard_night.jpg", "a/sign_bubble.webp"} {
		exists(name)
	}
	for _, gone := range []string{"a/s", "a/pc_workshop.jpg", "a/home_nocat.jpg", "a/p_jar.webp", "a/mullion_day.webp"} {
		if _, err := fs.Stat(files, gone); err == nil {
			t.Errorf("%s should be gone", gone)
		}
	}
}

func TestEveryNameIsSpoken(t *testing.T) {
	files := Bundle()
	var words map[string]string
	script(t, files, "lines.js", "window.CAT_UI", &words)
	page, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`<button class="(?:hot|tapper|back)" id="([^"]+)"`).FindAllStringSubmatch(string(page), -1)
	if len(ids) < 10 {
		t.Fatalf("the page has too few buttons: %d", len(ids))
	}
	for _, match := range ids {
		if strings.TrimSpace(words[match[1]]) == "" {
			t.Errorf("%s has no name", match[1])
		}
	}
	for _, key := range []string{"home", "souvenir", "bowl", "yarn", "frame", "gift"} {
		if words[key] == "" {
			t.Errorf("no name for %s", key)
		}
	}
}

// The product speaks English only: no other language is left in the page.
func TestThePageIsEnglishOnly(t *testing.T) {
	files := Bundle()
	han := regexp.MustCompile(`\p{Han}`)
	for _, name := range []string{"index.html", "game.js", "life.js", "guide.js", "card.js", "catalog.js", "lines.js"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		if han.Match(data) {
			t.Errorf("%s still has Chinese text", name)
		}
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
