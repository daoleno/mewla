package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// Docker stops the old account container after drain_timeout (docker stop -t)
// seconds; it must outlast shutdownGrace, or a deploy kills requests that the
// process would still have finished.
func TestDrainTimeoutOutlastsShutdownGrace(t *testing.T) {
	config, err := os.ReadFile("../../../config/deploy.yml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^drain_timeout: (\d+)$`).FindSubmatch(config)
	if match == nil {
		t.Fatal("config/deploy.yml has no top-level drain_timeout")
	}
	seconds, _ := strconv.Atoi(string(match[1]))
	drain := time.Duration(seconds) * time.Second
	if drain < shutdownGrace+5*time.Second {
		t.Fatalf("drain_timeout %v must be at least shutdownGrace %v + 5s", drain, shutdownGrace)
	}
	if shutdownGrace < 30*time.Second {
		t.Fatalf("shutdownGrace %v is shorter than an ACME TXT wait (20 s) plus its Cloudflare calls", shutdownGrace)
	}
}
