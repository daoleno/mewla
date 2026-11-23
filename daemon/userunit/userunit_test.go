package userunit

import "testing"

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

func TestEnclosingSliceIsTheInnermostSessionSlice(t *testing.T) {
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
		if got := EnclosingSliceOf(text); got != want {
			t.Fatalf("EnclosingSliceOf(%q) = %q, want %q", text, got, want)
		}
	}
}
