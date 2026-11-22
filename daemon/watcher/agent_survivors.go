package watcher

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// What a dead agent left running goes into a prompt and into Brain's Work
// events, so its command line is told without what may be a secret: an
// environment assignment's value, a JSON blob (Mewla's own --settings carries
// an API key), the value of a key/token/secret/password/auth/settings option,
// and anything token-shaped.
const (
	survivorArgMax   = 60
	survivorLineMax  = 160
	survivorListMax  = 8
	survivorRedacted = "…"
)

var (
	survivorEnvAssign   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	survivorSecretName  = regexp.MustCompile(`(?i)(key|token|secret|passw|auth|cred|settings|cookie|bearer)`)
	survivorTokenShaped = regexp.MustCompile(`^[A-Za-z0-9_\-+/=.]{24,}$`)
	survivorTokenPrefix = regexp.MustCompile(`^(sk-|ghp_|gho_|github_pat_|xox[abp]-|AKIA|ya29\.|eyJ)`)
)

// survivorCommand is args as one redacted line.
func survivorCommand(args []string) string {
	out := make([]string, 0, len(args))
	secretNext := false
	for index, arg := range args {
		if index == 0 {
			out = append(out, filepath.Base(arg))
			continue
		}
		switch {
		case secretNext:
			arg = survivorRedacted
			secretNext = false
		case strings.HasPrefix(arg, "-") && strings.Contains(arg, "="):
			name, _, _ := strings.Cut(arg, "=")
			if survivorSecretName.MatchString(name) {
				arg = name + "=" + survivorRedacted
			}
		case strings.HasPrefix(arg, "-"):
			secretNext = survivorSecretName.MatchString(arg)
		case survivorEnvAssign.MatchString(arg):
			name, _, _ := strings.Cut(arg, "=")
			arg = name + "=" + survivorRedacted
		case strings.HasPrefix(arg, "{") || strings.HasPrefix(arg, "["):
			arg = survivorRedacted
		case strings.ContainsAny(arg, ":=") && survivorSecretName.MatchString(arg):
			// "Authorization: Bearer …", "password=…": a header or setting
			// that names a secret.
			arg = survivorRedacted
		case survivorTokenPrefix.MatchString(arg) || (survivorTokenShaped.MatchString(arg) && !strings.Contains(arg, "/") && hasLetterAndDigit(arg)):
			arg = survivorRedacted
		}
		if len([]rune(arg)) > survivorArgMax {
			arg = string([]rune(arg)[:survivorArgMax-1]) + survivorRedacted
		}
		out = append(out, arg)
	}
	line := strings.Join(out, " ")
	if len([]rune(line)) > survivorLineMax {
		line = string([]rune(line)[:survivorLineMax-1]) + survivorRedacted
	}
	return line
}

func hasLetterAndDigit(s string) bool {
	letter, digit := false, false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter = true
		}
	}
	return letter && digit
}

// survivorList says what is still running, one process a line, with how long
// it has run: "- python3 -m http.server 18473 (running 12m)".
func survivorList(survivors []agentSurvivor, now time.Time) string {
	var lines []string
	for index, survivor := range survivors {
		if index == survivorListMax {
			lines = append(lines, fmt.Sprintf("- and %d more", len(survivors)-survivorListMax))
			break
		}
		line := "- " + survivor.Command
		if !survivor.Started.IsZero() {
			line += " (running " + survivorAge(now.Sub(survivor.Started)) + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func survivorAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
