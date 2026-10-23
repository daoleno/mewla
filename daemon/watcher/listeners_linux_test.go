//go:build linux

package watcher

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const procNetTCPFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:2694 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31001 1 0000000000000000 100 0 0 10 0
   1: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31002 1 0000000000000000 100 0 0 10 0
   2: 0100007F:0BB8 0100007F:D2F0 01 00000000:00000000 00:00000000 00000000  1000        0 31003 1 0000000000000000 100 0 0 10 0
   3: 0100007F:0000 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31004 1 0000000000000000 100 0 0 10 0
   4: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 0 1 0000000000000000 100 0 0 10 0
   5: ZZZZZZZZ:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31005 1 0000000000000000 100 0 0 10 0
`

const procNetTCP6Fixture = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 32001 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000001000000:2695 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 32002 1 0000000000000000 100 0 0 10 0
   2: 0000000000000000FFFF00000100007F:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 32003 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCPKeepsOnlyListeningRows(t *testing.T) {
	got := parseProcNetTCP(procNetTCPFixture, false)
	want := []listeningSocket{
		{inode: "31001", port: 9876, bind: "127.0.0.1"},
		{inode: "31002", port: 3000, bind: "0.0.0.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tcp = %#v, want %#v", got, want)
	}
	got6 := parseProcNetTCP(procNetTCP6Fixture, true)
	want6 := []listeningSocket{
		{inode: "32001", port: 5173, bind: "::"},
		{inode: "32002", port: 9877, bind: "::1"},
		{inode: "32003", port: 80, bind: "127.0.0.1"},
	}
	if !reflect.DeepEqual(got6, want6) {
		t.Fatalf("tcp6 = %#v, want %#v", got6, want6)
	}
}

func TestProcListeningSocketsResolvesOwnersFromFDLinks(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(pid, fd, target string) {
		t.Helper()
		dir := filepath.Join(root, pid, "fd")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, fd)); err != nil {
			t.Fatal(err)
		}
	}
	write("net/tcp", procNetTCPFixture)
	write("net/tcp6", procNetTCP6Fixture)
	// pid 500 and its child 501 share the 3000 socket: the parent is reported.
	link("501", "3", "socket:[31002]")
	link("500", "3", "socket:[31002]")
	link("500", "4", "socket:[31001]")
	link("700", "9", "socket:[32001]")
	link("700", "10", "/dev/null")
	link("700", "11", "pipe:[1]")
	// 32002 has no readable owner, and 99999 holds a socket that is not listening.
	link("99999", "5", "socket:[31003]")
	write("self-notes.txt", "not a pid directory")

	got, err := procListeningSockets(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []listeningSocket{
		{inode: "31002", pid: 500, port: 3000, bind: "0.0.0.0"},
		{inode: "32001", pid: 700, port: 5173, bind: "::"},
		{inode: "31001", pid: 500, port: 9876, bind: "127.0.0.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sockets = %#v, want %#v", got, want)
	}
}

func TestProcListeningSocketsWithoutIPv6Table(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(procNetTCPFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := procListeningSockets(root); err != nil || len(got) != 0 {
		t.Fatalf("no owners: %#v %v", got, err)
	}
	if _, err := procListeningSockets(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing /proc/net/tcp must be an error")
	}
}

func TestProcListeningSocketsFindsALiveListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	sockets, err := listListeningSockets()
	if err != nil {
		t.Fatal(err)
	}
	for _, socket := range sockets {
		if socket.port == port {
			if socket.pid != os.Getpid() || socket.bind != "127.0.0.1" || strings.TrimSpace(socket.inode) == "" {
				t.Fatalf("socket = %#v", socket)
			}
			return
		}
	}
	t.Fatalf("listener on %d not found in %#v", port, sockets)
}
