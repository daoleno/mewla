package memorysearch

import (
	"regexp"
	"strings"
)

const redacted = "[redacted]"

// Redact masks text that looks like a credential. Chat is stored for years, so
// the index keeps only redacted text; the original stays in the chat log.
//
// The rules are deliberately shaped: they catch known key prefixes, JWTs, PEM
// blocks, Bearer headers, URL passwords, secret-named assignments and long
// random runs, while UUIDs, paths, versions and 40-character git SHAs pass
// through. Hex runs are masked from 41 characters (a SHA-1 is 40; a SHA-256 is
// masked). A change here must bump schemaVersion so the index is rebuilt.
func Redact(text string) string {
	if text == "" {
		return text
	}
	text = pemBlock.ReplaceAllString(text, redacted)
	text = urlPassword.ReplaceAllString(text, "${1}"+redacted+"@")
	text = bearerToken.ReplaceAllString(text, "${1}"+redacted)
	text = secretAssignment.ReplaceAllStringFunc(text, redactAssignment)
	text = keyedPrefix.ReplaceAllString(text, redacted)
	text = jwtToken.ReplaceAllString(text, redacted)
	text = hexRun.ReplaceAllString(text, redacted)
	text = randomRun.ReplaceAllStringFunc(text, func(run string) string {
		if looksRandom(run) {
			return redacted
		}
		return run
	})
	return text
}

var (
	pemBlock    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	urlPassword = regexp.MustCompile(`(://[^\s/:@]+:)[^\s/@]+@`)
	bearerToken = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]{16,}`)
	keyedPrefix = regexp.MustCompile(
		`\b(?:sk-[A-Za-z0-9_-]{16,}` +
			`|gh[pousr]_[A-Za-z0-9]{20,}` +
			`|github_pat_[A-Za-z0-9_]{20,}` +
			`|xox[abeprs]-[A-Za-z0-9-]{10,}` +
			`|(?:AKIA|ASIA)[0-9A-Z]{16}` +
			`|AIza[0-9A-Za-z_-]{35}` +
			`|glpat-[A-Za-z0-9_-]{20,}` +
			`|npm_[A-Za-z0-9]{30,}` +
			`|hf_[A-Za-z0-9]{30,}` +
			`|pk_(?:live|test)_[A-Za-z0-9]{16,}` +
			`|sk_(?:live|test)_[A-Za-z0-9]{16,})`)
	jwtToken = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	hexRun   = regexp.MustCompile(`\b[0-9a-fA-F]{41,}\b`)
	// randomRun finds candidates for base64-like secrets; looksRandom decides.
	randomRun = regexp.MustCompile(`[A-Za-z0-9+/_]{40,}={0,2}`)

	// secretAssignment matches KEY=value, KEY: value and "key": "value" where
	// the key name says it is a secret. redactAssignment keeps only the shapes
	// that look like a value rather than prose.
	secretAssignment = regexp.MustCompile(
		`(?i)(["']?[A-Za-z0-9_.-]*(?:secret|token|passw(?:or)?d|passwd|pwd|api[_-]?key|access[_-]?key|private[_-]?key|credential|auth[_-]?key)[A-Za-z0-9_.-]*["']?)(\s*[=:]\s*)("[^"\n]*"|'[^'\n]*'|[^\s"',;]+)`)
)

func redactAssignment(match string) string {
	parts := secretAssignment.FindStringSubmatch(match)
	if len(parts) != 4 {
		return match
	}
	key, sep, value := parts[1], parts[2], parts[3]
	bare := strings.Trim(value, `"'`)
	if len([]rune(bare)) < 6 || bare == redacted {
		return match
	}
	if strings.Contains(sep, ":") && !strings.Contains(sep, "=") {
		// "token: foo" is usually prose; mask only values that look random or
		// are quoted JSON/YAML-style strings.
		quoted := value != bare
		if !quoted && !looksRandom(bare) {
			return match
		}
	}
	if value != bare {
		return key + sep + string(value[0]) + redacted + string(value[0])
	}
	return key + sep + redacted
}

// looksRandom reports whether s is a long mixed-case, digit-bearing run, the
// shape of base64 keys. Paths, words and identifiers lack one of the classes.
func looksRandom(s string) bool {
	if len(s) < 16 {
		return false
	}
	var upper, lower, digit bool
	slashes := 0
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		case r == '/':
			slashes++
		}
	}
	if slashes >= 4 {
		return false
	}
	return upper && lower && digit
}
