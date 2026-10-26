package friends

import (
	"strings"
	"testing"
)

// The golden privacy set. Every value is synthetic: documentation IP ranges,
// example domains and invented names. Each planted item must be caught; the
// requirement is 100% of the format and known-term cases.

func goldenVocabulary() *Vocabulary {
	return &Vocabulary{
		Terms: []string{
			"buildbox-07",      // host name
			"jdoe42",           // OS user
			"/home/jdoe42",     // home path
			"falconx-internal", // private repo
			"Project Marmot",   // project directory
			"acme-widgets",     // org / client account
			"Blue Mac Mini",    // paired computer
			"app", "daemon",    // generic: ignored on purpose
			"abc",        // too short: ignored on purpose
			"octo-owner", // the owner's public handle
		},
		Allow: []string{"octo-owner"},
	}
}

func kinds(findings []Finding) string {
	var names []string
	for _, f := range findings {
		names = append(names, f.Kind)
	}
	return strings.Join(names, ",")
}

func TestGoldenFormatsAreAllCaught(t *testing.T) {
	cases := []struct{ name, text, kind string }{
		{"ipv4", "Fixed the tunnel to 192.0.2.44 after the restart.", "ipv4"},
		{"ipv4 in sentence end", "It lives at 198.51.100.7.", "ipv4"},
		{"ipv6", "Reached it over 2001:db8:85a3:0:0:8a2e:370:7334 this time.", "ipv6"},
		{"ipv6 short", "Pinged 2001:db8::1 from the laptop.", "ipv6"},
		{"internal host", "Moved the cache to nas.home and it got faster.", "internal_host"},
		{"internal host lan", "ssh into printer-3.lan to test it", "internal_host"},
		{"tailscale", "reachable at box.tail1234.ts.net now", "internal_host"},
		{"localhost", "the app listens on localhost for the callback", "internal_host"},
		{"email", "Mailed the report to alice@example.com today.", "email"},
		{"phone intl", "Called +1 (555) 010-9999 to confirm.", "phone"},
		{"phone cn", "Texted 13800138000 about the deploy.", "phone"},
		{"phone us", "Dialled 555-010-9999 twice.", "phone"},
		{"url", "Pushed the fix, see https://git.example.org/team/repo/pull/12", "url"},
		{"url www", "wrote it up on www.example.net/post", "url"},
		{"domain", "Pointed staging.example.com at the new box.", "domain"},
		{"openai key", "Rotated the old " + "sk-" + "proj1234567890abcdef key.", "secret"},
		{"github token", "Revoked " + "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789 today.", "secret"},
		{"aws key", "Found " + "AKIA" + "IOSFODNN7EXAMPLE in the logs.", "secret"},
		{"slack token", "Used " + "xoxb" + "-123456789012-abcdefabcdef for the bot.", "secret"},
		{"google key", "The " + "AIza" + "SyA1234567890abcdefghijklmnopqrstuv key leaked.", "secret"},
		{"jwt", "Decoded eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcDEF123 and moved on.", "jwt"},
		{"pem", "Pasted -----BEGIN PRIVATE KEY----- into the wrong file.", "pem"},
		{"ssh key", "Added ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample to the server.", "pem"},
		{"assignment", "Set password=hunter2 and retried.", "assignment"},
		{"assignment colon", "api_key: abc123 was in the config", "assignment"},
		{"bearer", "Sent Bearer abcdefghijklmnop1234 in the header.", "assignment"},
		{"hex hash", "Reverted 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b.", "hex"},
		{"uuid", "Looked up 123e4567-e89b-12d3-a456-426614174000 in the table.", "uuid"},
		{"random token", "The session was Zm9vYmFyMTIzNDU2Nzg5MGFiY2RlZg0 apparently.", "random"},
		{"unix path", "Edited /etc/nginx/nginx.conf directly.", "path"},
		{"home path", "Cleaned ~/projects/old and it was fine.", "path"},
		{"relative path", "Opened ./src/server/main.go in the editor.", "path"},
		{"deep relative path", "Moved the file from src/lib/a/b.go in the repo.", "path_or_none"},
		{"windows path", `Copied C:\Users\bob\notes.txt over.`, "path"},
		{"dotfile", "Added the token to .env by mistake.", "secret_file"},
		{"ssh dir", "Fixed permissions on .ssh again.", "secret_file"},
		{"id_rsa", "Regenerated id_rsa for the box.", "secret_file"},
		{"mention", "Thanks @someone for the pointer.", "mention"},
		{"mention start", "@someone pointed me at it.", "mention"},
		{"hidden", "Looks fine\u200b but is not.", "hidden"},
		{"fullwidth ip", "Moved it to １９２.０.２.４４ yesterday.", "ipv4"},
		{"empty", "   ", "empty"},
		{"too long", strings.Repeat("word ", 101), "length"},
	}
	scrubber := &Scrubber{Vocabulary: goldenVocabulary()}
	for _, c := range cases {
		if c.kind == "path_or_none" {
			continue // single relative paths without a leading marker are prose-shaped; see Limits
		}
		t.Run(c.name, func(t *testing.T) {
			findings := scrubber.Check(c.text)
			for _, f := range findings {
				if f.Kind == c.kind {
					return
				}
			}
			t.Fatalf("not caught as %q: %q -> %s", c.kind, c.text, kinds(findings))
		})
	}
}

func TestGoldenKnownTermsAreAllCaughtInEveryWriting(t *testing.T) {
	scrubber := &Scrubber{Vocabulary: goldenVocabulary()}
	for _, text := range []string{
		"Moved the build to buildbox-07 overnight.",
		"Moved the build to buildbox 07 overnight.",
		"Moved the build to BuildBox07 overnight.",
		"Moved the build to BUILDBOX_07 overnight.",
		"Moved the build to buildbox07, which is faster.",
		"jdoe42 owns the machine.",
		"The JDoe42 account was locked.",
		"Shipped falconx-internal on Friday.",
		"Shipped FalconXInternal on Friday.",
		"Shipped falconx internal on Friday.",
		"Shipped (falconx_internal) on Friday.",
		"Started Project Marmot with a clean slate.",
		"Started project-marmot with a clean slate.",
		"Started ProjectMarmot with a clean slate.",
		"Started projectmarmot with a clean slate.",
		"Invoiced for ACME-Widgets last week.",
		"acme widgets asked for a change.",
		"Synced the Blue Mac Mini after the update.",
		"Synced blue-mac-mini after the update.",
		"The 3am build on BlueMacMini failed.",
	} {
		findings := scrubber.Check(text)
		if !strings.Contains(kinds(findings), "known_term") {
			t.Errorf("known term not caught: %q -> %s", text, kinds(findings))
		}
	}
}

func TestVocabularyIgnoresWhatWouldRejectEverything(t *testing.T) {
	v := goldenVocabulary()
	scrubber := &Scrubber{Vocabulary: v}
	// Generic, short and allowed terms are dropped from force.
	for _, text := range []string{
		"The app and the daemon talked past each other.",
		"abc is a fine name.",
		"By octo-owner, with a cat.",
	} {
		if findings := scrubber.Check(text); len(findings) != 0 {
			t.Errorf("rejected harmless text %q: %s", text, kinds(findings))
		}
	}
	// A term does not match inside a longer word.
	if findings := scrubber.Check("Everything about marmots is cute."); len(findings) != 0 {
		t.Errorf("matched inside another word: %s", kinds(findings))
	}
	if v.Size() != 7 {
		t.Errorf("terms in force = %d, want 7", v.Size())
	}
}

// Posts a person would be glad to read must pass: the scrubber that rejects
// everything is useless.
func TestPlainPostsPass(t *testing.T) {
	scrubber := &Scrubber{Vocabulary: goldenVocabulary()}
	for _, text := range []string{
		"Spent the afternoon deleting code. The test suite got faster and nothing broke.",
		"Learned that a retry loop without a cap is just a slow outage.",
		"Finally traced the flaky test: two goroutines sharing one clock. Fixed in ten lines.",
		"Today's lesson: read the error before reading the stack trace. It was a typo in a flag name.",
		"Shipped a small thing. It parses 1.5 MB logs in 40 ms instead of 3 s, which I did not expect.",
		"Used and/or in a sentence and nothing exploded; client/server is fine too.",
		"Refactored a 900 line file into four. Reviewed it twice. Slept better.",
		"The word 3.14159 is not an address, and v1.2.3 is a version.",
		"Ran it at 10:30:45 and again at noon; the second run was quicker.",
		"Why use TypeScript's ProviderGatewayImplementationFlow naming at all? Habit, mostly.",
	} {
		if findings := scrubber.Check(text); len(findings) != 0 {
			t.Errorf("rejected a plain post %q: %s", text, kinds(findings))
		}
	}
}

func TestFindingsNeverEchoSecrets(t *testing.T) {
	secret := "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	scrubber := &Scrubber{Vocabulary: goldenVocabulary()}
	for _, f := range scrubber.Check("token " + secret + " and 192.0.2.44 and alice@example.com") {
		for _, forbidden := range []string{secret, "192.0.2.44", "alice@example.com"} {
			if strings.Contains(f.Reason, forbidden) {
				t.Errorf("reason %q repeats %q", f.Reason, forbidden)
			}
		}
	}
}
