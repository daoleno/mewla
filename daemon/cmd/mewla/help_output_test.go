package main

import (
	"bytes"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The commands run dispatches, read from its switch, so a new command is
// covered without being listed here.
func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					name, _ := strconv.Unquote(lit.Value)
					names = append(names, name)
				}
			}
			return true
		})
	}
	if len(names) < 30 {
		t.Fatalf("found %d commands in run's switch: %q", len(names), names)
	}
	sort.Strings(names)
	return names
}

// Help goes to stdout with exit 0, for every command, so PowerShell 5.1's
// `2>&1` capture reads text instead of turning each line into an error
// record. It used to go to stderr everywhere.
func TestEveryCommandsHelpGoesToStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var invocations [][]string
	for _, flagName := range []string{"--help", "-h", "-help"} {
		invocations = append(invocations, []string{flagName})
	}
	invocations = append(invocations, []string{"help"})
	for _, name := range dispatchedCommands(t) {
		switch name {
		case "sh", "dsh-session", "agent-event":
			continue // internal: arguments are a script or a payload
		case "gcloud", "aws", "gws":
			invocations = append(invocations, []string{name, "--help"}, []string{name})
			continue
		}
		for _, flagName := range []string{"--help", "-h", "-help"} {
			invocations = append(invocations, []string{name, flagName})
		}
	}
	for group := range commandGroups {
		words := strings.Fields(group)
		invocations = append(invocations, words, append(words, "help"), append(append([]string{}, words...), "--help"))
	}
	invocations = append(invocations, []string{"worker", "spawn", "--help"}, []string{"brain", "workspace", "-h"}, []string{"boot", "status", "-help"})

	for _, args := range invocations {
		var stdout, stderr bytes.Buffer
		err := runCLI(args, &stdout, &stderr)
		if err != nil && !errors.Is(err, flag.ErrHelp) {
			t.Errorf("mewla %s: %v (help must exit 0)", strings.Join(args, " "), err)
		}
		if stdout.Len() == 0 || stderr.Len() != 0 {
			t.Errorf("mewla %s: %d bytes of help on stdout, %d on stderr:\n%s", strings.Join(args, " "), stdout.Len(), stderr.Len(), stderr.String())
		}
	}
}

// A mistake is an error: its message and the usage shown with it stay on
// stderr.
func TestAMistakeStaysOnStderr(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"worker", "spawn", "-bogus"},
		{"doctor", "-x"},
		{"agents", "foo"},
		{"--bogus"},
	} {
		var stdout, stderr bytes.Buffer
		err := runCLI(args, &stdout, &stderr)
		if err == nil || errors.Is(err, flag.ErrHelp) {
			t.Errorf("mewla %s: err = %v, want an error", strings.Join(args, " "), err)
		}
		if stdout.Len() != 0 {
			t.Errorf("mewla %s wrote to stdout:\n%s", strings.Join(args, " "), stdout.String())
		}
	}
}

func TestHelpRequested(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--help"}, true},
		{[]string{"help"}, true},
		{[]string{"worker"}, true},
		{[]string{"worker", "help"}, true},
		{[]string{"brain", "work"}, true},
		{[]string{"worker", "spawn", "-name", "x", "-h"}, true},
		{[]string{"pair"}, false},
		{[]string{"worker", "list"}, false},
		{[]string{"worker", "send", "-id", "x", "-text", "help"}, false},
		{[]string{"attach", "--", "sh", "-h"}, false},
		{[]string{"gcloud", "compute", "--help"}, false},
		{[]string{"gcloud", "--help"}, true},
		{[]string{"sh", "-c", "x", "-h"}, false},
	} {
		if got := helpRequested(tc.args); got != tc.want {
			t.Errorf("helpRequested(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
