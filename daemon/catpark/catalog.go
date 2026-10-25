package catpark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

// Catalog is the cat's world: levels, foods, hobbies, places and who may turn
// up on a postcard. It lives in the page's web/catalog.js so the page and the
// daemon read one file; the daemon only chooses ids, the page owns the words
// and the pictures.
type Catalog struct {
	Levels   []int            `json:"levels"`
	Foods    []string         `json:"foods"`
	Hobbies  []string         `json:"hobbies"`
	Kinds    map[Kind]string  `json:"kinds"`
	Places   map[string]Place `json:"places"`
	Rare     []string         `json:"rare"`
	Friends  []string         `json:"friends"`
	Weathers []string         `json:"weathers"`
	Words    Words            `json:"words"`
}

// Place is where a postcard happens and what the cat can be doing there.
type Place struct {
	// Hour pins a place that is only ever painted at one time of day.
	Hour string `json:"hour,omitempty"`
	// Indoor places show the weather only in the card's words.
	Indoor bool      `json:"indoor,omitempty"`
	Spot   []float64 `json:"spot"`
	Acts   []string  `json:"acts"`
}

// Words are the cat's lines; the daemon only checks they exist.
type Words struct {
	Places   map[string]string   `json:"places"`
	Acts     map[string][]string `json:"acts"`
	Friends  map[string]string   `json:"friends"`
	Weathers map[string]string   `json:"weathers"`
	Foods    map[string]string   `json:"foods"`
	Hobbies  map[string]string   `json:"hobbies"`
	Times    map[string]string   `json:"times"`
	Treat    string              `json:"treat"`
	Welcome  string              `json:"welcome"`
	// Away is the note the room leaves while the cat is out: where, and when back.
	Away   map[string]string `json:"away"`
	Letter []string          `json:"letter"`
}

var (
	catalogOnce sync.Once
	catalog     Catalog
	catalogErr  error
)

// World is the catalog the page carries. A broken catalog is a build error,
// so it panics: tests load it first.
func World() *Catalog {
	catalogOnce.Do(func() { catalog, catalogErr = ParseCatalog(Bundle()) })
	if catalogErr != nil {
		panic(catalogErr)
	}
	return &catalog
}

// ParseCatalog reads catalog.js from a bundle.
func ParseCatalog(files fs.FS) (Catalog, error) {
	data, err := fs.ReadFile(files, "catalog.js")
	if err != nil {
		return Catalog{}, err
	}
	start, end := bytes.IndexByte(data, '{'), bytes.LastIndexByte(data, '}')
	if start < 0 || end < start {
		return Catalog{}, fmt.Errorf("catpark: catalog.js holds no object")
	}
	var parsed Catalog
	if err := json.Unmarshal(data[start:end+1], &parsed); err != nil {
		return Catalog{}, fmt.Errorf("catpark: catalog.js: %w", err)
	}
	return parsed, nil
}

// HobbyOf is the hobby a trip of this kind teaches.
func (c *Catalog) HobbyOf(kind Kind) string { return c.Kinds[kind] }

// LevelOf turns stars into a level (1-based) and the stars into and needed
// for the next level; at the top the next need is zero.
func (c *Catalog) LevelOf(stars int) (level, into, need int) {
	level = 1
	for index := 1; index < len(c.Levels); index++ {
		if stars < c.Levels[index] {
			return level, stars - c.Levels[index-1], c.Levels[index] - c.Levels[index-1]
		}
		level = index + 1
	}
	return level, stars - c.Levels[len(c.Levels)-1], 0
}

func (c *Catalog) isFood(food string) bool { return contains(c.Foods, food) }
func (c *Catalog) isHobby(id string) bool  { return contains(c.Hobbies, id) }

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
