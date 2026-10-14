package account

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/daoleno/mewla/daemon/linkproto"
)

// Hostnames are public in Certificate Transparency logs, so a computer gets
// a random readable label unless its owner picks a name.
var (
	labelAdjectives = []string{
		"amber", "bold", "brave", "bright", "calm", "clever", "cool", "crisp",
		"eager", "fair", "fast", "gentle", "glad", "golden", "grand", "green",
		"happy", "keen", "kind", "light", "lively", "lucky", "merry", "mild",
		"misty", "noble", "polite", "proud", "quick", "quiet", "rapid", "ready",
		"rosy", "royal", "silent", "silver", "sleek", "smart", "snowy", "solid",
		"steady", "sunny", "swift", "tidy", "true", "vivid", "warm", "wise",
	}
	labelNouns = []string{
		"badger", "beacon", "birch", "brook", "canyon", "cedar", "comet", "coral",
		"crane", "dawn", "delta", "dune", "falcon", "fern", "field", "finch",
		"fjord", "forest", "glade", "harbor", "heron", "hill", "island", "lake",
		"lantern", "maple", "meadow", "mesa", "moon", "otter", "owl", "panda",
		"pebble", "pine", "planet", "prairie", "raven", "reef", "river", "robin",
		"sparrow", "spruce", "star", "summit", "tiger", "valley", "willow", "wren",
	}
)

func randomLabel() (string, error) {
	adjective, err := randomWord(labelAdjectives)
	if err != nil {
		return "", err
	}
	noun, err := randomWord(labelNouns)
	if err != nil {
		return "", err
	}
	suffix, err := randomHex(2)
	if err != nil {
		return "", err
	}
	label := adjective + "-" + noun + "-" + suffix
	if !linkproto.IsHostLabel(label) {
		return "", fmt.Errorf("generated label %q is invalid", label)
	}
	return label, nil
}

func randomWord(words []string) (string, error) {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		return "", err
	}
	return words[index.Int64()], nil
}
