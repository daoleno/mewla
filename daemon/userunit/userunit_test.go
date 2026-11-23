package userunit

import (
	"strings"
	"testing"
)

const (
	owner = "b1d37f5b0839c9fa8d5ccdc1dc8f6f56"
	tokA  = "cdd59611720143a9ad517642bbf329af"
	tokB  = "0123456789abcdef0123456789abcdef"
)

func TestTokenNamesOnlyAResourceUnit(t *testing.T) {
	unit := "mewla-worker-" + owner + "-" + tokA + ".scope"
	if got := Token(unit); got != tokA {
		t.Fatalf("Token(%q) = %q", unit, got)
	}
	for _, bad := range []string{
		"",
		"zen-worker-" + owner + "-" + tokA + ".scope",
		"mewla-worker-" + owner + "-" + tokA + ".slice",
		"mewla-worker-" + owner + "-" + tokA[:31] + ".scope",
		"mewla-worker-" + owner + "-" + "CDD59611720143A9AD517642BBF329AF" + ".scope",
		"mewla-worker--" + tokA + ".scope",
	} {
		if got := Token(bad); got != "" {
			t.Fatalf("Token(%q) = %q, want none", bad, got)
		}
	}
}

func TestSessionSliceSitsInsideTheEnclosingSession(t *testing.T) {
	unit := "mewla-worker-" + owner + "-" + tokB + ".scope"
	if got, want := SessionSlice(unit, ""), "mewla-worker-"+owner+"-"+tokB+".slice"; got != want {
		t.Fatalf("top level: %q, want %q", got, want)
	}
	outer := "mewla-worker-" + owner + "-" + tokA + ".slice"
	if got, want := SessionSlice(unit, outer), "mewla-worker-"+owner+"-"+tokA+"-"+tokB+".slice"; got != want {
		t.Fatalf("nested: %q, want %q", got, want)
	}
	if SessionSlice("mewla-session-host-1.service", "") != "" {
		t.Fatal("a slice for a non-resource unit")
	}
	for _, slice := range []string{SessionSlice(unit, ""), SessionSlice(unit, outer)} {
		if !isSessionSlice(slice) {
			t.Fatalf("%q is not recognised as a Session slice", slice)
		}
	}
}

func TestNestingSliceIsTheInnermostSessionSlice(t *testing.T) {
	base := "0::/user.slice/user-1000.slice/user@1000.service/mewla.slice/mewla-worker.slice/mewla-worker-" + owner + ".slice/"
	outer := "mewla-worker-" + owner + "-" + tokA + ".slice"
	inner := "mewla-worker-" + owner + "-" + tokA + "-" + tokB + ".slice"
	cases := map[string]string{
		base + outer + "/mewla-worker-" + owner + "-" + tokA + "-p0a0b0c0d.scope\n":                                               outer,
		base + outer + "/" + inner + "/mewla-worker-" + owner + "-" + tokA + "-" + tokB + "-p1.scope":                             inner,
		base + outer + "/mewla-session-host-574b65fe-6f83.service\n":                                                              outer,
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/mewla-session-host-1.service\n":                               "",
		"0::" + "/user.slice/user-1000.slice/user@1000.service/mewla.slice/mewla-worker.slice/mewla-worker-" + owner + ".slice\n": "",
		"12:pids:/" + outer + "\n": "",
	}
	for text, want := range cases {
		if got := NestingSlice(text, nil); got != want {
			t.Fatalf("NestingSlice(%q) = %q, want %q", text, got, want)
		}
	}
}

// A daemon started from a shell in one of its own Sessions does not nest in
// it: its own Sessions are skipped, outward, to the first one that is not.
func TestNestingSliceSkipsTheMewlasOwnSessions(t *testing.T) {
	base := "0::/user.slice/user-1000.slice/user@1000.service/mewla.slice/mewla-worker.slice/mewla-worker-" + owner + ".slice/"
	outer := "mewla-worker-" + owner + "-" + tokA + ".slice"
	inner := "mewla-worker-" + owner + "-" + tokA + "-" + tokB + ".slice"
	ownsA := func(token string) bool { return token == tokA }
	ownsB := func(token string) bool { return token == tokB }
	if got := NestingSlice(base+outer+"/x-p1.scope\n", ownsA); got != "" {
		t.Fatalf("in its own top-level Session: nests in %q", got)
	}
	if got := NestingSlice(base+outer+"/"+inner+"/y-p2.scope\n", ownsB); got != outer {
		t.Fatalf("a nested daemon in its own Session: nests in %q, want the outer %q", got, outer)
	}
	if got := NestingSlice(base+outer+"/"+inner+"/y-p2.scope\n", ownsA); got != inner {
		t.Fatalf("another daemon's Session: nests in %q, want %q", got, inner)
	}
	if got := sliceToken(inner); got != tokB {
		t.Fatalf("sliceToken(%q) = %q", inner, got)
	}
}

func TestSessionTokensListsEverySessionSliceOutermostFirst(t *testing.T) {
	outer, inner := strings.Repeat("a", 32), strings.Repeat("b", 32)
	cgroup := "0::/user.slice/user-1000.slice/user@1000.service/mewla.slice/mewla-worker.slice/" +
		"mewla-worker-o1.slice/mewla-worker-o1-" + outer + ".slice/mewla-worker-o1-" + outer + "-" + inner + ".slice/" +
		"mewla-worker-o1-" + outer + "-" + inner + "-p0000beef.scope\n"
	if got := strings.Join(SessionTokens(cgroup), ","); got != outer+","+inner {
		t.Fatalf("tokens = %s", got)
	}
	if got := SessionTokens("0::/user.slice/user-1000.slice/session-2.scope\n"); len(got) != 0 {
		t.Fatalf("tokens outside a Session = %v", got)
	}
}
