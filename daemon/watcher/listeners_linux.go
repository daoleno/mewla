//go:build linux

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// listListeningSockets lists the TCP sockets in LISTEN state and the process
// that holds each, from /proc: /proc/net/tcp{,6} for the sockets and the
// /proc/<pid>/fd symlinks ("socket:[inode]") for their owners. Like any
// unprivileged lister it sees owners only among processes whose fd directory
// it may read. A socket several processes share (a forked server) is reported
// once, for the lowest pid, which is the parent that opened it.
func listListeningSockets() ([]listeningSocket, error) {
	return procListeningSockets("/proc")
}

func procListeningSockets(root string) ([]listeningSocket, error) {
	byInode := map[string]listeningSocket{}
	read := 0
	for _, table := range []struct {
		name string
		ipv6 bool
	}{{"tcp", false}, {"tcp6", true}} {
		data, err := os.ReadFile(filepath.Join(root, "net", table.name))
		if err != nil {
			if os.IsNotExist(err) && table.ipv6 {
				continue // IPv6 disabled on this host
			}
			return nil, fmt.Errorf("listening sockets: %w", err)
		}
		read++
		for _, socket := range parseProcNetTCP(string(data), table.ipv6) {
			byInode[socket.inode] = socket
		}
	}
	if read == 0 || len(byInode) == 0 {
		return nil, nil
	}

	owners := map[string]int{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("listening sockets: %w", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fdDir := filepath.Join(root, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // another user's process, or one that just exited
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(target, "socket:[")
			if !ok {
				continue
			}
			inode = strings.TrimSuffix(inode, "]")
			if _, listening := byInode[inode]; !listening {
				continue
			}
			if current, seen := owners[inode]; !seen || pid < current {
				owners[inode] = pid
			}
		}
	}

	var sockets []listeningSocket
	for inode, socket := range byInode {
		pid, owned := owners[inode]
		if !owned {
			continue
		}
		socket.pid = pid
		sockets = append(sockets, socket)
	}
	sort.Slice(sockets, func(i, j int) bool {
		a, b := sockets[i], sockets[j]
		if a.port != b.port {
			return a.port < b.port
		}
		if a.bind != b.bind {
			return a.bind < b.bind
		}
		return a.pid < b.pid
	})
	return sockets, nil
}

// parseProcNetTCP reads the LISTEN rows (state 0A) of /proc/net/tcp or tcp6:
//
//	sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode ...
//
// The pid is left zero; the caller resolves it from the inode.
func parseProcNetTCP(table string, ipv6 bool) []listeningSocket {
	var sockets []listeningSocket
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		address, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil || port == 0 {
			continue
		}
		bind, ok := procBindAddress(address, ipv6)
		if !ok || fields[9] == "0" {
			continue
		}
		sockets = append(sockets, listeningSocket{inode: fields[9], port: int(port), bind: bind})
	}
	return sockets
}

// procBindAddress decodes a /proc/net address: each 32-bit word is printed as a
// host-order integer, so on the little-endian hosts Mewla runs on, every word's
// bytes are reversed.
func procBindAddress(hexAddress string, ipv6 bool) (string, bool) {
	want := 8
	if ipv6 {
		want = 32
	}
	if len(hexAddress) != want {
		return "", false
	}
	raw := make([]byte, want/2)
	for word := 0; word < len(raw); word += 4 {
		for i := 0; i < 4; i++ {
			value, err := strconv.ParseUint(hexAddress[(word+3-i)*2:(word+3-i)*2+2], 16, 8)
			if err != nil {
				return "", false
			}
			raw[word+i] = byte(value)
		}
	}
	return formatBindAddress(raw), true
}
