package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// versionTail is how much of the end of a transcript AgentVersion reads.
const versionTail = 256 << 10

// AgentVersion is the Claude Code version that wrote the Worker's transcript
// (the binding's Ref, from its hooks): the "version" of the last record that
// has one. Empty when the transcript is unknown or has none.
func (Adapter) AgentVersion(binding agentevents.Binding) string {
	path := strings.TrimSpace(binding.Ref)
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > versionTail {
		if _, err := file.Seek(info.Size()-versionTail, io.SeekStart); err != nil {
			return ""
		}
	}
	tail, err := io.ReadAll(io.LimitReader(file, versionTail))
	if err != nil {
		return ""
	}
	return lastTranscriptVersion(tail)
}

func lastTranscriptVersion(tail []byte) string {
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var record struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(lines[i], &record) == nil && strings.TrimSpace(record.Version) != "" {
			return strings.TrimSpace(record.Version)
		}
	}
	return ""
}
