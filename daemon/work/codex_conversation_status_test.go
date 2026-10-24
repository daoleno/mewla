package work

import (
	"encoding/json"
	"testing"
)

func TestCodexErrorInfoCarriesItsStructuredStatus(t *testing.T) {
	for raw, want := range map[string]struct {
		code   string
		status int
	}{
		`{"http_connection_failed":{"http_status_code":502}}`:            {"http_connection_failed", 502},
		`{"response_too_many_failed_attempts":{"http_status_code":429}}`: {"response_too_many_failed_attempts", 429},
		`{"http_connection_failed":{"http_status_code":null}}`:           {"http_connection_failed", 0},
		`"server_overloaded"`: {"server_overloaded", 0},
		`"other"`:             {"other", 0},
		``:                    {"", 0},
	} {
		if code, status := codexErrorInfoCode(json.RawMessage(raw)), codexErrorInfoStatus(json.RawMessage(raw)); code != want.code || status != want.status {
			t.Errorf("%s: code %q status %d, want %q %d", raw, code, status, want.code, want.status)
		}
	}
}
