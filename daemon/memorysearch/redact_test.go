package memorysearch

import (
	"strings"
	"testing"
)

// The fake credentials below are shaped like real ones but are not valid.
func TestRedactMasksCredentials(t *testing.T) {
	cases := map[string]string{
		"openai":       "key sk-proj-AbCdEf0123456789xyzXYZ0123456789 end",
		"anthropic":    "sk-ant-api03-abcdefghijklmnopqrstuvwx_yz-0123456789",
		"github":       "token ghp_0123456789abcdefghijABCDEFGHIJ012345",
		"github pat":   "github_pat_11ABCDEFG0abcdefghijkl_mnopqrstuvwxyz0123456789",
		"slack":        "xoxb-1234567890-abcdefghijkl",
		"aws":          "AKIAIOSFODNN7EXAMPLE",
		"google":       "AIzaSyA-1234567890abcdefghijklmnopqrstuv",
		"jwt":          "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r",
		"bearer":       "Authorization: Bearer abcdef0123456789ABCDEFghijkl",
		"url password": "postgres://admin:hunter2hunter2@db.internal:5432/app",
		"env line":     "ARCAI_DB_PASSWORD=Sup3rS3cretValue",
		"export":       "export API_TOKEN=abc123def456",
		"json":         `{"client_secret": "zzzz1111yyyy"}`,
		"yaml random":  "access_key: Zx81Qw93Lm20Pa77Rb55",
		"base64":       "blob Zm9vYmFyQmF6MTIzNDU2Nzg5MEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFla end",
		"hex":          "hash 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef end",
		"pem":          "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\nabcd\n-----END RSA PRIVATE KEY-----",
	}
	for name, input := range cases {
		out := Redact(input)
		if !strings.Contains(out, redacted) {
			t.Errorf("%s: nothing redacted: %q", name, out)
		}
	}

	// The secret value itself must be gone, not just flagged.
	for input, secret := range map[string]string{
		"ARCAI_DB_PASSWORD=Sup3rS3cretValue":                           "Sup3rS3cretValue",
		"postgres://admin:hunter2hunter2@db.internal":                  "hunter2hunter2",
		"Authorization: Bearer abcdef0123456789ABCDEFghijkl":           "abcdef0123456789",
		`{"client_secret": "zzzz1111yyyy"}`:                            "zzzz1111yyyy",
		"AKIAIOSFODNN7EXAMPLE and more":                                "AKIAIOSFODNN7EXAMPLE",
		"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----": "AAAA",
	} {
		if out := Redact(input); strings.Contains(out, secret) {
			t.Errorf("secret %q survived in %q", secret, out)
		}
	}
	if got := Redact("ARCAI_DB_PASSWORD=Sup3rS3cretValue"); got != "ARCAI_DB_PASSWORD="+redacted {
		t.Errorf("assignment keeps its key: %q", got)
	}
	if got := Redact("postgres://admin:hunter2hunter2@db.internal"); got != "postgres://admin:"+redacted+"@db.internal" {
		t.Errorf("url keeps user and host: %q", got)
	}
}

func TestRedactLeavesOrdinaryTextAlone(t *testing.T) {
	keep := []string{
		"07ac686b-8805-4402-87bb-8f5fed88e002",
		"session 3300a0be-65e7-433c-9780-bf0f4eba81b8 in /home/daoleno/.mewla/brain/workspace/worklog/mewla-memory-search/RESULT.md",
		"/home/daoleno/.claude/projects/-home-daoleno--mewla-brain-workspace/07ac686b-8805-4402-87bb-8f5fed88e002.jsonl",
		"commit dc1e480c9f3a1b2c3d4e5f60718293a4b5c6d7e8 (40-char git sha)",
		"v0.2.27-beta.1 and build 66",
		"2026-10-05T17:50:12.174734986Z",
		"我发现 zen 在 handoff 的时候这 token 效率不高，请不要加这种 AI slope。",
		"the token budget: we should lower it",
		"password: 需要换掉",
		"set the secret rotation to monthly",
		"ERR_ENTITY_UNPROCESSABLE.ANOTHER_BUILD_IN_REVIEW",
		"https://claude.ai/artifact/abcdef and https://example.com/a/b/c/d/e/f/g/h/i",
		"daemon/memorysearch/redact_test.go:42 and docs/brain-and-work.md",
	}
	for _, text := range keep {
		if got := Redact(text); got != text {
			t.Errorf("changed ordinary text:\n  in:  %q\n  out: %q", text, got)
		}
	}
}
