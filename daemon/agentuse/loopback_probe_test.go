package agentuse

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Can this computer's Chrome load a page from a program listening on
// 127.0.0.1? On the Mac acceptance it could not load the test's own server
// (and not reach Playwright's relay), with the application firewall on. This
// tries the same page from four servers (this test's Go binary, with and
// without a proxy for Chrome, Apple's python3, and node) and says which load,
// so the acceptance can be built around one that does.
//
//	MEWLA_PROBE_CHROME=<chrome binary> go test ./agentuse -run LoopbackProbe -v -count=1
func TestLoopbackProbe(t *testing.T) {
	chromeBin := os.Getenv("MEWLA_PROBE_CHROME")
	if chromeBin == "" {
		t.Skip("set MEWLA_PROBE_CHROME")
	}
	if runtime.GOOS == "darwin" {
		for _, args := range [][]string{{"/usr/sbin/scutil", "--proxy"}, {"/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate"}} {
			out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
			t.Logf("%s: %s %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
	}
	type server struct {
		name  string
		extra []string // for Chrome
		start func(t *testing.T, port int) func()
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>ok-loopback</title>ok"), 0o644)
	servers := []server{
		{name: "go", start: func(t *testing.T, port int) func() { return goServer(t, port) }},
		{name: "go, Chrome without a proxy", extra: []string{"--no-proxy-server"}, start: func(t *testing.T, port int) func() { return goServer(t, port) }},
		{name: "python3", start: func(t *testing.T, port int) func() {
			py, err := exec.LookPath("python3")
			if err != nil {
				t.Logf("no python3: %v", err)
				return nil
			}
			cmd := exec.Command(py, "-m", "http.server", fmt.Sprint(port), "--bind", "127.0.0.1")
			cmd.Dir = dir
			if cmd.Start() != nil {
				return nil
			}
			time.Sleep(700 * time.Millisecond)
			return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
		}},
		{name: "node", start: func(t *testing.T, port int) func() {
			node, err := exec.LookPath("node")
			if err != nil {
				t.Logf("no node: %v", err)
				return nil
			}
			cmd := exec.Command(node, "-e", fmt.Sprintf(`require('http').createServer((q,r)=>{r.setHeader('content-type','text/html');r.end('<title>ok-loopback</title>ok')}).listen(%d,'127.0.0.1')`, port))
			if cmd.Start() != nil {
				return nil
			}
			time.Sleep(700 * time.Millisecond)
			return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
		}},
	}
	for i, s := range servers {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		stop := s.start(t, port)
		if stop == nil {
			t.Logf("%-28s not run", s.name)
			continue
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/", port)
		// This process itself can reach every one of them.
		resp, err := http.Get(url)
		self := "ok"
		if err != nil {
			self = err.Error()
		} else {
			resp.Body.Close()
		}
		debugPort := 9360 + i
		args := append([]string{"--user-data-dir=" + filepath.Join(dir, fmt.Sprintf("p%d", i)), "--no-first-run", "--no-default-browser-check",
			fmt.Sprintf("--remote-debugging-port=%d", debugPort), "--window-size=600,400"}, s.extra...)
		if runtime.GOOS == "linux" {
			args = append(args, "--no-sandbox")
		}
		chrome := exec.Command(chromeBin, append(args, url)...)
		if err := chrome.Start(); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		title := ""
		for time.Since(started) < 12*time.Second && title != "ok-loopback" {
			time.Sleep(300 * time.Millisecond)
			title = pageTitle(debugPort, url)
		}
		t.Logf("%-28s %s: this process reaches it: %s; Chrome loaded it: %v (title %q, %s)", s.name, url, self, title == "ok-loopback", title, time.Since(started).Round(100*time.Millisecond))
		_ = chrome.Process.Kill()
		_ = chrome.Wait()
		stop()
	}
}

func goServer(t *testing.T, port int) func() {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprint(w, "<title>ok-loopback</title>ok")
	}))
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	return srv.Close
}

// pageTitle is the title of the page Chrome has open at url, read over DevTools,
// or "" while there is none or it has not loaded.
func pageTitle(debugPort int, url string) string {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", debugPort))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var targets []map[string]any
	if json.NewDecoder(resp.Body).Decode(&targets) != nil {
		return ""
	}
	for _, tg := range targets {
		if tg["type"] == "page" && strings.HasPrefix(fmt.Sprint(tg["url"]), url) {
			return fmt.Sprint(tg["title"])
		}
	}
	return ""
}
