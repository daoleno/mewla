package work

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestClaudePasteAdmissionExactEnvelope(t *testing.T) {
	for _, payload := range []string{"<mewla_work_event>\n{\"summary\":\"large\"}\n</mewla_work_event>", " \t你好\r\n\n\n", "trailing \t", ""} {
		wrap := func(id string) string {
			return "\n\n<pasted_content id=\"" + id + "\">\n" + payload + "\n</pasted_content id=\"" + id + "\">\n"
		}
		raw := wrap("4f28")
		for _, tc := range []struct {
			name, raw string
			valid     bool
		}{
			{"real shape", raw, true},
			{"mismatched ID", strings.Replace(raw, "</pasted_content id=\"4f28\">", "</pasted_content id=\"4f29\">", 1), false},
			{"prefix", "text" + raw, false}, {"suffix", raw + "text", false},
			{"missing leading newlines", strings.TrimPrefix(raw, "\n\n"), false},
			{"missing trailing newline", strings.TrimSuffix(raw, "\n"), false},
			{"multiple", raw + raw, false}, {"invalid ID", wrap("oops"), false},
			{"nested", "\n\n<pasted_content id=\"4f28\">\n" + raw + "\n</pasted_content id=\"4f28\">\n", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				event := providerAdmissionFixtureEvent(t, WorkerProviderClaude, tc.raw)
				if event.AdmissionSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.raw))) {
					t.Fatal("raw literal digest changed")
				}
				want := ""
				if tc.valid {
					want = fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
				}
				if event.AdmissionUnwrappedSHA256 != want {
					t.Fatalf("inner digest=%q want %q", event.AdmissionUnwrappedSHA256, want)
				}
			})
		}
	}
}
