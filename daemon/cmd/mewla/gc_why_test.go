package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/gc"
)

func TestGCWhyPrintsTheChainNearestFirst(t *testing.T) {
	var out bytes.Buffer
	writeGCWhy(&out, gc.Why{Mewla: true, Chain: []gc.WhyLink{
		{Kind: "process", ID: "4242", Name: "mewla"},
		{Kind: "scratch", ID: "/s/t/zjD2o6", By: "its executable is there"},
		{Kind: "session", ID: "mh-1", Name: "Release gate", State: "running", By: "its marker names the Session"},
		{Kind: "work", ID: "w-1", Name: "Release 0.4.26", State: "open"},
	}})
	writeGCWhy(&out, gc.Why{Chain: []gc.WhyLink{{Kind: "path", ID: "/x"}, {Kind: "outside", Name: "outside Mewla's state directory"}}})
	want := `process 4242 mewla
  scratch /s/t/zjD2o6 (its executable is there)
  Session mh-1 Release gate, running (its marker names the Session)
  Work w-1 Release 0.4.26, open
path /x
  not Mewla's: outside Mewla's state directory
`
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestGCFlagsFirstKeepsABoolFlagFromTakingTheNextArgument(t *testing.T) {
	fs := flag.NewFlagSet("why", flag.ContinueOnError)
	fs.Bool("json", false, "")
	fs.String("state-dir", "", "")
	got := strings.Join(gcFlagsFirst(fs, []string{"4242", "--json", "-state-dir", "/s"}), " ")
	if got != "--json -state-dir /s 4242" {
		t.Fatalf("got %q", got)
	}
}
