package modelprofiles

import (
	"fmt"
	"regexp"
	"strings"
)

var slugWordRE = regexp.MustCompile(`[^a-z0-9]+`)

const (
	// maxProviderSlugLength mirrors the providerSlugRE bound in validate.go
	// (`^[a-z][a-z0-9-]{0,47}$`).
	maxProviderSlugLength = 48
	// providerSlugIDSuffixLimit bounds how many identity characters a slug
	// keeps when the connection id is unusually long. Generated connection
	// ids are "conn-<16 hex>"; 24 characters retain the full random tail
	// while leaving room for a readable name.
	providerSlugIDSuffixLimit = 24
)

// ProviderSlugFor creates a readable, collision-resistant slug. The canonical
// connection ID is included so deletion never permits a later connection to
// silently inherit the old public identity.
//
// The identity suffix is derived from the full connection id and is never the
// part that gets truncated: when the combined slug is too long the display
// name is shortened first. Two ids that share a prefix (for example
// "conn_fb0aaa…" and "conn_fb0bbb…") therefore keep distinct slugs, which is
// what prevents the "model profile revision conflict: provider slug" error
// real users hit when two connections happened to share their first eight
// characters.
//
// Existing persisted slugs are unaffected: Store.load keeps any non-empty
// slug and MigrateProviderSlugs only fills missing ones, so previously issued
// slugs keep resolving.
func ProviderSlugFor(name, id string) string {
	namePart := slugToken(name)
	if namePart == "" {
		namePart = "provider"
	}
	idPart := slugIDSuffix(id)
	if len(idPart) > maxProviderSlugLength-1 {
		idPart = strings.TrimLeft(idPart[len(idPart)-(maxProviderSlugLength-1):], "-")
	}
	// Reserve room for the separator and the full identity suffix; shorten
	// the display name, never the identity, when the slug would overflow.
	if limit := maxProviderSlugLength - len(idPart) - 1; len(namePart) > limit {
		namePart = strings.TrimRight(namePart[:limit], "-")
	}
	slug := strings.Trim(strings.Join([]string{namePart, idPart}, "-"), "-")
	if err := ValidateProviderSlug(slug); err != nil {
		return providerSlugFallback(idPart)
	}
	return slug
}

// slugToken lowercases value and folds every run of non-slug characters into
// a single hyphen, then trims edge hyphens.
func slugToken(value string) string {
	token := slugWordRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "-")
	return strings.Trim(token, "-")
}

// slugIDSuffix keeps the tail of the connection id so ids that share a long
// prefix still differ in the slug; the tail also carries the random identity
// of generated "conn-<hex>" ids.
func slugIDSuffix(id string) string {
	token := slugToken(id)
	if token == "" {
		return "provider"
	}
	if len(token) > providerSlugIDSuffixLimit {
		token = strings.TrimLeft(token[len(token)-providerSlugIDSuffixLimit:], "-")
	}
	// A slug must start with a letter; a tail cut mid-token may start with a
	// digit, so anchor it with a letter.
	if token == "" || token[0] < 'a' || token[0] > 'z' {
		return "p" + token
	}
	return token
}

// providerSlugFallback keeps the identity suffix when the readable name
// cannot form a valid slug (for example a name that starts with a digit).
func providerSlugFallback(idPart string) string {
	fallback := "provider"
	if idPart != "" {
		fallback = fallback + "-" + idPart
	}
	fallback = strings.Trim(fallback, "-")
	if len(fallback) > maxProviderSlugLength {
		fallback = strings.TrimRight(fallback[:maxProviderSlugLength], "-")
	}
	return fallback
}

func ensureProviderSlug(profile Profile, profiles map[string]Profile) (Profile, error) {
	profile = normalizeProfile(profile)
	if profile.Slug == "" {
		profile.Slug = ProviderSlugFor(profile.Name, profile.ID)
	}
	if err := ValidateProviderSlug(profile.Slug); err != nil {
		return Profile{}, err
	}
	for id, existing := range profiles {
		if id != profile.ID && strings.EqualFold(existing.Slug, profile.Slug) {
			return Profile{}, fmt.Errorf("%w: provider slug %q", ErrConflict, profile.Slug)
		}
	}
	return profile, nil
}
