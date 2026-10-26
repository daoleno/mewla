package friends

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// MaxPostRunes is the post length limit shared with the hosted service.
const MaxPostRunes = 500

// Limits is what the scrubber cannot catch, in words a person can read.
var Limits = []string{
	"names of people, unless they are in the private vocabulary",
	"clients, companies and projects that never appear on this computer",
	"anything private by meaning rather than by form (a plan, a dispute, a diagnosis)",
	"identifiers written to dodge pattern matching, such as an address spelled out in words",
	"private terms shorter than four letters and generic words such as app, daemon or work",
	"directories written as plain words without a leading ./ or / and without a file name (src/lib/a)",
}

// Finding is one reason a draft was refused.
type Finding struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// Vocabulary is this computer's private terms.
type Vocabulary struct {
	// Terms are names that must not appear: host, user, projects, repos,
	// accounts, paired computers.
	Terms []string
	// Allow are terms that are public by the owner's choice, such as their
	// Friends handle; they are never matched even if listed in Terms.
	Allow []string

	compiled []term
}

type term struct {
	text   string
	tokens []string
	squash string
}

// minTermLetters keeps short terms from rejecting almost every draft.
const minTermLetters = 4

// genericTerms are common directory and product words. A project called
// "daemon" or "app" would otherwise refuse nearly every draft about code.
var genericTerms = stringSet(
	"app", "apps", "application", "api", "build", "bin", "cache", "client", "clients", "code", "common",
	"config", "core", "daemon", "data", "dist", "docs", "doc", "documents", "downloads", "home", "lib",
	"libs", "main", "master", "mobile", "node", "modules", "note", "notes", "project", "projects", "repo",
	"repos", "scripts", "server", "servers", "service", "services", "shared", "site", "sites", "source",
	"src", "temp", "test", "tests", "tmp", "tools", "user", "users", "utils", "web", "work", "workspace",
	"worker", "workers", "desktop", "public", "private", "local", "default", "example", "examples",
	"github", "gitlab", "mewla", "claude", "codex", "brain", "root", "admin", "linux", "macos", "windows",
	"android", "docker", "design", "scratch", "backup", "backups", "archive", "archives", "release",
	"releases", "plugin", "plugins", "script", "state", "store", "stores", "images", "assets", "static",
)

func stringSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// Compile prepares the vocabulary. It is called by Check when needed.
func (v *Vocabulary) compile() {
	if v.compiled != nil {
		return
	}
	allowed := map[string]bool{}
	for _, word := range v.Allow {
		allowed[squash(word)] = true
	}
	seen := map[string]bool{}
	v.compiled = []term{}
	for _, raw := range v.Terms {
		tokens := tokenize(raw)
		joined := strings.Join(tokens, "")
		if len([]rune(joined)) < minTermLetters || seen[joined] || allowed[joined] || genericTerms[joined] {
			continue
		}
		seen[joined] = true
		v.compiled = append(v.compiled, term{text: strings.TrimSpace(raw), tokens: tokens, squash: joined})
	}
}

// Size is how many terms are in force after the short and generic ones go.
func (v *Vocabulary) Size() int {
	v.compile()
	return len(v.compiled)
}

func squash(s string) string { return strings.Join(tokenize(s), "") }

// tokenize lowercases and splits on anything that is not a letter or digit,
// and at camelCase and letter-digit boundaries, so FooBar, foo-bar and
// "foo bar" give the same tokens.
func tokenize(s string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, strings.ToLower(string(current)))
			current = current[:0]
		}
	}
	var prev rune
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			prev = 0
			continue
		}
		if len(current) > 0 {
			switch {
			case unicode.IsLower(prev) && unicode.IsUpper(r):
				flush()
			case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
				flush()
			case unicode.IsDigit(prev) != unicode.IsDigit(r):
				flush()
			}
		}
		current = append(current, r)
		prev = r
	}
	flush()
	return tokens
}

// matchTerms finds vocabulary terms in text on token boundaries, whatever the
// separators or capitalisation.
func (v *Vocabulary) matchTerms(text string) []string {
	v.compile()
	if len(v.compiled) == 0 {
		return nil
	}
	tokens := tokenize(text)
	var hits []string
	for _, t := range v.compiled {
		if containsRun(tokens, t.squash) {
			hits = append(hits, t.text)
		}
	}
	return hits
}

// containsRun reports whether some run of consecutive tokens concatenates to want.
func containsRun(tokens []string, want string) bool {
	for start := range tokens {
		var built strings.Builder
		for end := start; end < len(tokens); end++ {
			built.WriteString(tokens[end])
			if built.Len() > len(want) {
				break
			}
			if built.String() == want {
				return true
			}
		}
	}
	return false
}

type rule struct {
	kind   string
	reason string
	re     *regexp.Regexp
	accept func(match string) bool
}

const topLevelDomains = `com|net|org|io|ai|dev|app|co|cn|me|xyz|info|biz|cloud|tech|so|sh|gg|tv|us|uk|de|jp|ru|fr|eu|in|edu|gov|site|online|top|vip|ly|run|page|pro`

var rules = []rule{
	{kind: "pem", reason: "contains a key block", re: regexp.MustCompile(`-----BEGIN [A-Z ]+-----|\bssh-(?:rsa|ed25519|dss)\s+AAAA`)},
	{kind: "secret", reason: "contains something shaped like an API key or token", re: regexp.MustCompile(
		`\b(?:sk-[A-Za-z0-9_-]{8,}|[sp]k_(?:live|test)_\w+|gh[pousr]_[A-Za-z0-9]{16,}|github_pat_\w{16,}|(?:AKIA|ASIA)[0-9A-Z]{12,}|xox[abprs]-[\w-]{8,}|AIza[\w-]{20,}|glpat-[\w-]{12,}|npm_[A-Za-z0-9]{20,}|hf_[A-Za-z0-9]{20,}|ya29\.[\w-]+|shpat_\w+|SG\.[\w-]{10,}\.[\w-]{10,}|key-[0-9a-f]{20,})`)},
	{kind: "jwt", reason: "contains a JWT", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)},
	{kind: "assignment", reason: "contains a credential assignment", re: regexp.MustCompile(
		`(?i)\b(?:password|passwd|pwd|secret|token|api[_-]?key|apikey|credential)s?\b\s*[:=]\s*\S+|\bbearer\s+[\w.~+/=-]{8,}`)},
	{kind: "email", reason: "contains an email address", re: regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)},
	{kind: "url", reason: "contains a URL", re: regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|www\.|mailto:)\S+`)},
	{kind: "internal_host", reason: "contains an internal host name", re: regexp.MustCompile(
		`(?i)\b(?:localhost|[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:local|lan|internal|intranet|home|corp|localdomain|ts\.net|svc|cluster\.local))\b`)},
	{kind: "domain", reason: "contains a domain name", re: regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+(?:` + topLevelDomains + `)\b`)},
	{kind: "ipv4", reason: "contains an IP address", re: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)},
	{kind: "ipv6", reason: "contains an IPv6 address", re: regexp.MustCompile(`(?i)(?:^|[^\w:])[0-9a-f:]{5,}`), accept: func(m string) bool {
		m = strings.TrimLeft(m, " \t\n(\"'")
		return (strings.Count(m, ":") >= 3 || strings.Contains(m, "::")) && strings.ContainsAny(strings.ToLower(m), "0123456789abcdef") && strings.Trim(m, ":") != ""
	}},
	{kind: "phone", reason: "contains a phone number", re: regexp.MustCompile(`\+\d[\d\s().-]{7,}\d|\b\d{3}[\s.-]\d{3,4}[\s.-]\d{4}\b|\b1[3-9]\d{9}\b`)},
	{kind: "uuid", reason: "contains an identifier (UUID)", re: regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)},
	{kind: "hex", reason: "contains a long hex string (hash or key)", re: regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`)},
	{kind: "path", reason: "contains a file path", re: regexp.MustCompile(
		`(?:^|[\s("'\x60=])(?:~|\.{1,2})?/(?:[^\s/"'\x60)]+/)+[^\s/"'\x60)]*|(?:^|[\s("'\x60=])(?:~|\.{1,2})/[^\s"'\x60)]*|(?:^|[\s("'\x60=])/(?:etc|usr|var|home|tmp|opt|mnt|root|Users|proc|dev|srv|bin|sbin|lib|Volumes)\b[^\s]*|\b[A-Za-z]:\\\S+`)},
	{kind: "path", reason: "contains a file path", re: regexp.MustCompile(`\b[\w.-]+(?:/[\w.-]+)+\.[A-Za-z][A-Za-z0-9]{0,4}\b`)},
	{kind: "secret_file", reason: "mentions a credentials file", re: regexp.MustCompile(`(?i)(?:^|[\s/])\.(?:env|ssh|aws|npmrc|netrc|gnupg)\b|\bid_(?:rsa|ed25519|ecdsa)\b|\bcredentials\.json\b`)},
	{kind: "mention", reason: "contains an @mention", re: regexp.MustCompile(`(?:^|[^\w@.])@[A-Za-z0-9_][\w-]*`)},
}

var tokenCandidate = regexp.MustCompile(`[A-Za-z0-9_\-+/=]{20,}`)

// Scrubber checks drafts against the rules and a private vocabulary.
type Scrubber struct {
	Vocabulary *Vocabulary
}

// Check returns every reason text may not be posted. An empty result means it
// passed the rules; it does not mean it is safe (see Limits).
func (s *Scrubber) Check(text string) []Finding {
	var findings []Finding
	add := func(kind, reason string) {
		for _, f := range findings {
			if f.Kind == kind && f.Reason == reason {
				return
			}
		}
		findings = append(findings, Finding{kind, reason})
	}
	if strings.TrimSpace(text) == "" {
		return []Finding{{"empty", "the draft is empty"}}
	}
	if n := len([]rune(text)); n > MaxPostRunes {
		add("length", fmt.Sprintf("is %d characters; the limit is %d", n, MaxPostRunes))
	}
	clean, hidden := normalize(text)
	if hidden {
		add("hidden", "contains hidden or direction-changing characters")
	}
	for _, r := range rules {
		for _, loc := range r.re.FindAllStringIndex(clean, -1) {
			if r.accept != nil && !r.accept(clean[loc[0]:loc[1]]) {
				continue
			}
			add(r.kind, r.reason)
		}
	}
	for _, token := range tokenCandidate.FindAllString(clean, -1) {
		if looksRandom(token) {
			add("random", "contains a long random-looking string")
		}
	}
	if s != nil && s.Vocabulary != nil {
		for _, hit := range s.Vocabulary.matchTerms(clean) {
			add("known_term", fmt.Sprintf("contains the private name %q", hit))
		}
	}
	return findings
}

// normalize folds full-width ASCII to ASCII and drops zero-width and
// bidirectional controls, reporting whether any were there.
func normalize(text string) (string, bool) {
	hidden := false
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064, r == 0xFEFF, r >= 0x2066 && r <= 0x2069:
			hidden = true
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		case r == 0x3000:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), hidden
}

// looksRandom flags long tokens that mix digits with letters at high entropy.
// Plain identifiers and words have no digits and pass.
func looksRandom(token string) bool {
	var upper, lower, digit int
	counts := map[rune]int{}
	for _, r := range token {
		counts[r]++
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		case r >= '0' && r <= '9':
			digit++
		}
	}
	if digit == 0 || upper+lower == 0 {
		return false
	}
	entropy := 0.0
	for _, n := range counts {
		p := float64(n) / float64(len(token))
		entropy -= p * math.Log2(p)
	}
	return entropy >= 3.3
}
