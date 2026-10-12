package watch

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Redaction runs on every text field before a signal is stored and before
// any truncation, so a cut secret can never slip past its pattern.

type secretPattern struct {
	re      *regexp.Regexp
	replace func(match []string) string
}

const redacted = "[REDACTED]"

func whole(string) func([]string) string {
	return func([]string) string { return redacted }
}

var secretPatterns = []secretPattern{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), whole("private key")},
	// Provider API keys: sk-ant-…, sk-proj-…, sk-<hex>. A word boundary keeps
	// words like "task-notification" intact; a digit keeps slugs intact.
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), func(m []string) string {
		if strings.ContainsAny(m[0], "0123456789") {
			return redacted
		}
		return m[0]
	}},
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`), whole("github")},
	{regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), whole("slack")},
	{regexp.MustCompile(`\bA(?:KIA|SIA)[0-9A-Z]{16}\b`), whole("aws")},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`), whole("google")},
	{regexp.MustCompile(`\blin_(?:api|oauth)_[A-Za-z0-9]{20,}`), whole("linear")},
	{regexp.MustCompile(`\bntn_[A-Za-z0-9]{30,}|\bsecret_[A-Za-z0-9]{40,}`), whole("notion")},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{8,}`), whole("jwt")},
	// fal keys: <uuid>:<32 hex>.
	{regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:[0-9a-f]{32}\b`), whole("fal")},
	{regexp.MustCompile(`(?i)\b(bearer|token|basic)(\s+)([A-Za-z0-9._~+/=-]{20,})`), func(m []string) string {
		return m[1] + m[2] + redacted
	}},
	// URL credentials: https://user:pass@host or https://token@host.
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@:]+(?::[^/\s@]*)?@`), func(m []string) string {
		return m[1] + redacted + "@"
	}},
	// Assignments: api_key=…, "client_secret": "…", PASSWORD: ….
	{regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:api[_-]?key|apikey|secret|token|password|passwd|access[_-]?key|credential)[a-z0-9_]*)(["']?\s*[:=]\s*["']?)([A-Za-z0-9._~+/=-]{12,})`), func(m []string) string {
		return m[1] + m[2] + redacted
	}},
}

// Redact replaces secrets in text with [REDACTED].
func Redact(text string) string {
	if text == "" {
		return text
	}
	for _, pattern := range secretPatterns {
		re, replace := pattern.re, pattern.replace
		text = re.ReplaceAllStringFunc(text, func(match string) string {
			return replace(re.FindStringSubmatch(match))
		})
	}
	return text
}

// excerpt redacts, flattens whitespace and truncates text to max runes.
func excerpt(text string, max int) string {
	text = strings.Join(strings.Fields(Redact(text)), " ")
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}

// redactSignal applies Redact to every text field of s.
func redactSignal(s Signal) Signal {
	s.Excerpt = Redact(s.Excerpt)
	s.Pointer = Redact(s.Pointer)
	if len(s.Refs) > 0 {
		refs := make(map[string]string, len(s.Refs))
		for key, value := range s.Refs {
			refs[key] = Redact(value)
		}
		s.Refs = refs
	}
	return s
}
