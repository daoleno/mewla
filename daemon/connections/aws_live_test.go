package connections

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestAWSCommandNamesMatchCLI checks, against the real AWS CLI on this
// computer, that every operation the model index names is a command the CLI
// has. It needs the CLI and about three minutes: MEWLA_AWS_AUDIT=1.
func TestAWSCommandNamesMatchCLI(t *testing.T) {
	if os.Getenv("MEWLA_AWS_AUDIT") == "" {
		t.Skip("set MEWLA_AWS_AUDIT=1 to compare operation names with the installed AWS CLI")
	}
	dir, err := findAWSModels()
	if err != nil {
		t.Fatal(err)
	}
	idx, err := loadAWSIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m|.\b")
	var mu sync.Mutex
	var missing []string
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, name := range idx.names {
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := exec.CommandContext(context.Background(), "aws", name, "help")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "AWS_PAGER=", "TERM=dumb", "COLUMNS=200"}
			out, _ := cmd.CombinedOutput()
			text := ansi.ReplaceAllString(string(out), "")
			has := map[string]bool{}
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "* ") {
					has[strings.TrimSpace(line[2:])] = true
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(has) == 0 {
				missing = append(missing, name+": no command list")
				return
			}
			for _, op := range idx.services[name].ops {
				if !has[op.Command] && !op.Deprecated {
					shapes, err := idx.loadShapes(idx.services[name])
					if err == nil && (shapes.streams(op.input) || shapes.streams(op.output)) {
						continue
					}
					missing = append(missing, name+" "+op.Name)
				}
			}
		}(name)
	}
	wg.Wait()
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d operations the CLI does not list:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}
