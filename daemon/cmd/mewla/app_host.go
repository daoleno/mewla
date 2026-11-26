package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
)

// The app's host (client/macos/src/host.rs) is the process macOS charges the
// daemon's permissions to. An update replaces the app and restarts the daemon
// in place (`mewla restart -if-older`: the same process, the new executable),
// so the host it runs under is still the old app's, from a bundle moved aside
// and deleted (the owner's Mac, 11-26: /Applications/.nsh-updater-old-…). macOS
// then answers every permission check of the daemon, and of any fresh process
// it starts, for that bundle: no grant counts until the host starts again from
// the installed app. The daemon finds that out by itself, says so (the grants
// read "restart") and restarts its process at a quiet moment, once per host:
// launchd starts the job again from the installed app, and the session host
// and its Sessions run on, as on any daemon restart.

// appHostPID is the app's host the daemon runs under (MEWLA_STOP_WITH_HOST),
// 0 when it runs under none.
var appHostPID atomic.Int64

const (
	appHostFirstCheck = 15 * time.Second
	appHostPoll       = time.Minute
	// appHostMarker keeps the host a restart was asked for, so a host that
	// still reads as replaced afterwards is never restarted in a loop.
	appHostMarker = "app-host-restart"
)

// replacedAppHost says whether host, the executable of the app's host, is not
// the app this daemon (helper, its executable) is a part of, and names the one
// it should be. gone: macOS has no path for the host's executable any more,
// which is so once the app it was started from is deleted (an update moves the
// old app aside and deletes it; proc_pidpath then answers nothing, the owner's
// Mac 11-26). A daemon outside an app's Contents/Helpers, a host outside an
// app's Contents/MacOS, an unknown host, and an installed app whose own
// executables are missing (an update half done) are never replaced: nothing is
// done until it reads right.
func replacedAppHost(host string, gone bool, helper string, exists func(string) bool) (replaced bool, want string) {
	if helper == "" || (host == "" && !gone) {
		return false, ""
	}
	helpers := filepath.Dir(helper)
	if !strings.EqualFold(filepath.Base(helpers), "Helpers") || !strings.EqualFold(filepath.Base(filepath.Dir(helpers)), "Contents") {
		return false, ""
	}
	macOS := filepath.Join(filepath.Dir(helpers), "MacOS")
	if gone {
		return exists(macOS), macOS
	}
	hostDir := filepath.Dir(host)
	if !strings.EqualFold(filepath.Base(hostDir), "MacOS") || !strings.EqualFold(filepath.Base(filepath.Dir(hostDir)), "Contents") {
		return false, ""
	}
	want = filepath.Join(macOS, filepath.Base(host))
	if strings.EqualFold(filepath.Clean(host), want) {
		return false, want
	}
	if !exists(want) {
		return false, want
	}
	return true, want
}

// appHostRestartGap: a second restart onto the installed app waits this long
// after the first, whatever host asks, so a host that still reads replaced
// after its restart never restarts Mewla in a loop.
const appHostRestartGap = 10 * time.Minute

// mayRestartAppHost: no restart was asked for this host, nor for any in the
// last appHostRestartGap (marker holds the host of the last one).
func mayRestartAppHost(marker string, host int, now time.Time) bool {
	info, err := os.Stat(marker)
	if err != nil {
		return true
	}
	if now.Sub(info.ModTime()) < appHostRestartGap {
		return false
	}
	tried, _ := os.ReadFile(marker)
	return strings.TrimSpace(string(tried)) != strconv.Itoa(host)
}

// noteAppHost keeps the host's pid from MEWLA_STOP_WITH_HOST (stopWithHost).
func noteAppHost(pid int) { appHostPID.Store(int64(pid)) }

// watchAppHost checks the app's host after start and every appHostPoll; once
// it runs a replaced app, the grants read "restart" and the daemon restarts
// its process when quiet says nothing would be cut, once for that host.
func watchAppHost(ctx context.Context, stateDir string, quiet func() string, restart func()) {
	host := int(appHostPID.Load())
	if host <= 1 || os.Getppid() != host {
		return
	}
	helper, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(helper); err == nil {
		helper = resolved
	}
	marker := filepath.Join(stateDir, "run", appHostMarker)
	exists := func(path string) bool { _, err := os.Stat(path); return err == nil }
	log.Printf("mewla: under the app's host (pid %d); checking it runs the installed app", host)
	go func() {
		wait := appHostFirstCheck
		logged, waited := false, ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = appHostPoll
			path, err := processPath(host)
			if err != nil {
				return // this system cannot tell
			}
			if os.Getppid() != host {
				return // the host is gone: stopWithHost stops the daemon
			}
			replaced, want := replacedAppHost(path, path == "", helper, exists)
			if !replaced {
				continue
			}
			screen.SetHostReplaced(true)
			if !logged {
				logged = true
				runs := path
				if runs == "" {
					runs = "an app that was deleted"
				}
				log.Printf("mewla: the app's host (pid %d) runs %s, not the installed %s: its permissions are not this Mewla's until it starts again", host, runs, want)
			}
			if !mayRestartAppHost(marker, host, time.Now()) {
				continue // asked once already: the person's Restart Mewla does it
			}
			if reason := quiet(); reason != "" {
				if waited != reason {
					waited = reason
					log.Printf("mewla: restarting onto the installed app once %s is done", reason)
				}
				continue
			}
			_ = os.MkdirAll(filepath.Dir(marker), 0o700)
			if err := os.WriteFile(marker, []byte(strconv.Itoa(host)+"\n"), 0o600); err != nil {
				log.Printf("mewla: not restarting onto the installed app: %v", err)
				continue
			}
			log.Printf("mewla: restarting so launchd starts the installed app's host")
			restart()
			return
		}
	}()
}
