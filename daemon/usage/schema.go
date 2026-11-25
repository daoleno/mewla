// Package usage is Mewla's anonymous usage counts: the one schema both the
// sender (the daemon) and the receiver (the account service) check.
//
// Every property is an enum, a bucket, a version or a random id. Nothing free
// form is accepted, so content, prompts, paths, file names, host names and
// error text cannot travel even by mistake: a value outside the schema is
// refused on both ends. docs/security-and-privacy.md#usage-counts lists the
// same events for people.
package usage

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Event is one count. Props hold only schema values.
type Event struct {
	Name  string            `json:"name"`
	Props map[string]string `json:"props,omitempty"`
}

// Batch is what a computer sends: its random install id, the UTC day the
// events happened on and the events. There is no time of day.
type Batch struct {
	InstallID string  `json:"install_id"`
	Day       string  `json:"day"`
	Events    []Event `json:"events"`
}

// Limits on what one request may carry.
const (
	MaxEventsPerBatch = 200
	MaxBatchBytes     = 64 << 10
)

type check func(string) bool

func oneOf(values ...string) check {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return func(v string) bool { return set[v] }
}

// setOf accepts a sorted, comma-separated subset of values without repeats
// ("" is the empty set).
func setOf(values ...string) check {
	one := oneOf(values...)
	return func(v string) bool {
		if v == "" {
			return true
		}
		parts := strings.Split(v, ",")
		if len(parts) > len(values) || !sort.StringsAreSorted(parts) {
			return false
		}
		for i, p := range parts {
			if !one(p) || (i > 0 && parts[i-1] == p) {
				return false
			}
		}
		return true
	}
}

var (
	versionPattern = regexp.MustCompile(`^(dev|[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,4}(-[0-9A-Za-z.]{1,20})?)$`)
	idPattern      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	dayPattern     = regexp.MustCompile(`^20[0-9]{2}-[01][0-9]-[0-3][0-9]$`)
	osMajorPattern = regexp.MustCompile(`^[0-9]{1,2}$`)
)

func version(v string) bool { return versionPattern.MatchString(v) }

// ValidID reports whether id is a random install or app id (32 hex).
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ValidDay reports whether day is a UTC calendar day (YYYY-MM-DD).
func ValidDay(day string) bool { return dayPattern.MatchString(day) }

// Enums shared by the sender and the receiver.
var (
	OSes          = []string{"darwin", "linux", "windows", "other"}
	Arches        = []string{"amd64", "arm64", "other"}
	InstallVia    = []string{"script", "mac_app", "source", "other"}
	AgentKinds    = []string{"antigravity", "claude", "codex", "cursor", "gemini", "grok", "opencode", "pi", "other"}
	Features      = []string{"brain", "browser", "calendar", "computer_use", "files", "friends", "phone", "relay", "screen", "skills", "terminal", "triggers", "watch", "work"}
	Transports    = []string{"lan", "relay", "tailscale", "tunnel", "loopback", "other"}
	Platforms     = []string{"android", "ios", "mac", "web", "other"}
	LoginFailures = []string{"denied", "expired", "network", "quota", "conflict", "other"}
	PairFailures  = []string{"expired", "admission", "pin", "network", "denied", "timeout", "other"}
	ReplyFailures = []string{"no_agent", "provider_auth", "model", "timeout", "quota", "other"}
	Components    = []string{"daemon", "brain", "session", "terminal", "screen", "update", "relay", "app"}
	ErrorClasses  = []string{"panic", "crash", "start_failed", "lost_connection", "other"}
	Results       = []string{"ok", "failed"}
	Buckets       = []string{"0", "1", "2-3", "4-9", "10+"}
	Durations     = []string{"<1m", "1-5m", "5-30m", "30m-2h", "2h-1d", ">1d"}
	ModelFamilies = []string{"claude-opus", "claude-sonnet", "claude-haiku", "claude-fable", "gpt", "gemini", "grok", "glm", "kimi", "qwen", "deepseek", "local", "other"}
	Referrers     = []string{"direct", "github", "google", "bing", "duckduckgo", "baidu", "x", "hackernews", "reddit", "v2ex", "producthunt", "linkedin", "youtube", "mewla", "other"}
	Pages         = []string{"home", "docs", "download", "privacy", "support", "other"}
	Bools         = []string{"true", "false"}
)

// Schema is every event and its properties. A property not listed, or a
// value its check refuses, makes the event invalid.
var Schema = map[string]map[string]check{
	// The first start of this install.
	"install": {"os": oneOf(OSes...), "arch": oneOf(Arches...), "version": version, "via": oneOf(InstallVia...)},
	// Once a UTC day while Mewla runs.
	"daily": {
		"os": oneOf(OSes...), "arch": oneOf(Arches...), "version": version,
		"signed_in": oneOf(Bools...), "devices": oneOf(Buckets...),
		"agents": setOf(AgentKinds...), "features": setOf(Features...),
	},
	"login_started":  {},
	"login_finished": {},
	"login_failed":   {"reason": oneOf(LoginFailures...)},
	"pair_shown":     {"transport": oneOf(Transports...)},
	"pair_ok":        {"transport": oneOf(Transports...), "platform": oneOf(Platforms...)},
	"pair_failed":    {"reason": oneOf(PairFailures...)},
	// The first Brain reply this install ever got, and how long after install.
	"first_reply":     {"after": oneOf(Durations...), "agent": oneOf(AgentKinds...)},
	"reply_failed":    {"class": oneOf(ReplyFailures...), "agent": oneOf(AgentKinds...)},
	"session_started": {"agent": oneOf(AgentKinds...), "model": oneOf(ModelFamilies...)},
	"update":          {"from": version, "to": version, "result": oneOf(Results...)},
	"error":           {"component": oneOf(Components...), "class": oneOf(ErrorClasses...)},
	// A paired app's day, sent through its computer.
	"app_daily": {"app": ValidID, "platform": oneOf(Platforms...), "os_major": osMajorPattern.MatchString, "version": version},
}

// Validate checks one event against the schema.
func Validate(e Event) error {
	props, ok := Schema[e.Name]
	if !ok {
		return fmt.Errorf("unknown event %q", clip(e.Name))
	}
	for key, value := range e.Props {
		valid, ok := props[key]
		if !ok {
			return fmt.Errorf("%s: unknown property %q", e.Name, clip(key))
		}
		if !valid(value) {
			return fmt.Errorf("%s: %s is not a schema value", e.Name, key)
		}
	}
	return nil
}

// ValidateBatch checks a whole batch; one bad event refuses all of it.
func ValidateBatch(b Batch) error {
	if !ValidID(b.InstallID) {
		return fmt.Errorf("install id is not 32 hex")
	}
	if !ValidDay(b.Day) {
		return fmt.Errorf("day is not YYYY-MM-DD")
	}
	if len(b.Events) == 0 || len(b.Events) > MaxEventsPerBatch {
		return fmt.Errorf("a batch holds 1 to %d events", MaxEventsPerBatch)
	}
	for _, e := range b.Events {
		if err := Validate(e); err != nil {
			return err
		}
	}
	return nil
}

// Visit is one page view of mewla.ai: the page kind and where the visitor
// came from, as enums. No address, cookie, user agent or time of day.
type Visit struct {
	Page     string `json:"page"`
	Referrer string `json:"ref"`
}

// ValidateVisit checks a site visit.
func ValidateVisit(v Visit) error {
	if !oneOf(Pages...)(v.Page) || !oneOf(Referrers...)(v.Referrer) {
		return fmt.Errorf("visit is not schema values")
	}
	return nil
}

// Bucket puts a count in its bucket ("0", "1", "2-3", "4-9", "10+").
func Bucket(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n == 1:
		return "1"
	case n <= 3:
		return "2-3"
	case n <= 9:
		return "4-9"
	default:
		return "10+"
	}
}

// Set joins enum values into a schema set: sorted, without repeats or
// values the enum does not know.
func Set(values []string, allowed []string) string {
	known := oneOf(allowed...)
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if known(v) && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// OneOf returns v when allowed lists it and "other" otherwise.
func OneOf(v string, allowed []string) string {
	if oneOf(allowed...)(v) {
		return v
	}
	return "other"
}

func clip(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}
