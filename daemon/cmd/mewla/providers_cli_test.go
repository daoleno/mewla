package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

// TestParseInterleavedAcceptsFlagsAfterModel: `set <model> --connection <id>`
// and `set --connection <id> <model>` mean the same thing.
func TestParseInterleavedAcceptsFlagsAfterModel(t *testing.T) {
	for _, args := range [][]string{
		{"gpt-image-2.5-sunburst", "--connection", "conn_x"},
		{"--connection", "conn_x", "gpt-image-2.5-sunburst"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		connection := fs.String("connection", "", "")
		positional, err := parseInterleaved(fs, args)
		if err != nil || *connection != "conn_x" || !reflect.DeepEqual(positional, []string{"gpt-image-2.5-sunburst"}) {
			t.Fatalf("%v: positional=%v connection=%q err=%v", args, positional, *connection, err)
		}
	}
}
