//go:build linux

package browser

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"golang.org/x/net/publicsuffix"
	_ "modernc.org/sqlite"
)

// signedInSitesFile keeps, beside the profile, the sites that had a sign-in the
// last time Chrome was not running, each with when its last one expires.
const signedInSitesFile = "signed-in-sites.json"

// sessionCookieExpiry stands for a cookie that lasts as long as the browser:
// the profile keeps those across a restart (see keepSessionCookies).
var sessionCookieExpiry = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// chromeEpochUnixMicros is 1601-01-01, where Chrome counts cookie times from,
// in Unix microseconds (a time.Duration from there would overflow).
const chromeEpochUnixMicros = -11644473600 * 1_000_000

// signedInSites reads, from a profile Chrome is not running on, each site
// signed in and when its last sign-in cookie expires. A sign-in is an HttpOnly
// cookie (what sites keep a session in, out of reach of their pages' scripts;
// analytics and preference cookies are not), and a site is its registrable
// domain, so accounts.google.com and .google.com are one google.com. Local
// and IP hosts are a Worker's own apps under test, not the person's sign-ins.
func signedInSites(profile string) (map[string]time.Time, error) {
	path := filepath.Join(profile, "Default", "Cookies")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return map[string]time.Time{}, nil
	} else if err != nil {
		return nil, err
	}
	// Read only: Chrome's own database keeps its own journal and locks.
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT host_key, expires_utc, is_persistent FROM cookies WHERE is_httponly != 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := map[string]time.Time{}
	for rows.Next() {
		var host string
		var expires int64
		var persistent int64
		if err := rows.Scan(&host, &expires, &persistent); err != nil {
			return nil, err
		}
		host = siteOf(host)
		if host == "" {
			continue
		}
		until := sessionCookieExpiry
		if persistent != 0 && expires > 0 {
			until = time.UnixMicro(expires + chromeEpochUnixMicros).UTC()
		}
		if until.After(sites[host]) {
			sites[host] = until
		}
	}
	return sites, rows.Err()
}

// siteOf is the registrable domain of a cookie's host, or "" for a local or
// IP host.
func siteOf(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "."), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(strings.Trim(host, "[]")) != nil {
		return ""
	}
	if !strings.Contains(host, ".") {
		return ""
	}
	if site, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return site
	}
	return host
}

// lostSignIns is each site of before that had a cookie still valid at now and
// has none in after.
func lostSignIns(before, after map[string]time.Time, now time.Time) []string {
	var lost []string
	for host, until := range before {
		if _, kept := after[host]; kept || !until.After(now) {
			continue
		}
		lost = append(lost, host)
	}
	sort.Strings(lost)
	return lost
}

// checkSignIns compares the sites with cookies in a profile Chrome is not
// running on with those it had the last time, returns each that lost every
// cookie before it expired, and keeps what the profile has now for the next
// check. The person signed in to those sites for the agents; each would
// otherwise be found signed out by a Worker that then asks the person again.
func checkSignIns(root, profile string, now time.Time) ([]string, error) {
	after, err := signedInSites(profile)
	if err != nil {
		return nil, fmt.Errorf("the agents' sign-ins could not be read: %w", err)
	}
	recordPath := filepath.Join(root, signedInSitesFile)
	before := map[string]time.Time{}
	if raw, err := os.ReadFile(recordPath); err == nil {
		if err := json.Unmarshal(raw, &before); err != nil {
			log.Printf("browser: %s is unreadable (%v); starting it again", recordPath, err)
			before = map[string]time.Time{}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	raw, err := json.Marshal(after)
	if err != nil {
		return nil, err
	}
	if err := atomicfile.Write(recordPath, raw, 0o600); err != nil {
		return nil, err
	}
	return lostSignIns(before, after, now), nil
}

func signInsLostDetail(lost []string) string {
	return fmt.Sprintf("the agents' browser lost the sign-in cookies of %d site(s) before they expired, so the person is signed out there: %s", len(lost), strings.Join(lost, ", "))
}
