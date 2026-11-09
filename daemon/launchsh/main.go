package launchsh

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// Main is `mewla sh -c SCRIPT`. It ignores Ctrl+C and Ctrl+Break: in a
// console every process gets them, and the program it started decides what
// they mean; this process ends when that program does.
func Main(args []string, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "-c" {
		fmt.Fprintln(stderr, "usage: mewla sh -c SCRIPT")
		return 2
	}
	interrupts := make(chan os.Signal, 4)
	signal.Notify(interrupts, os.Interrupt)
	go func() {
		for range interrupts {
		}
	}()
	code, err := Run(context.Background(), args[1], Options{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		fmt.Fprintf(stderr, "mewla sh: %v\n", err)
	}
	return code
}
