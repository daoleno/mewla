package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// removeTelegramState deletes what the Telegram channel to Brain left in the
// state directory: its folder (the bot token, the owner binding and chat ids,
// state.db) and the sibling backups an older migration made of it. The channel
// is gone, so nothing may keep a bot token or a chat id. A missing path is
// silent; a path that cannot be removed is said once and left.
func removeTelegramState(stateRoot string, stderr io.Writer) {
	if stateRoot == "" {
		return
	}
	paths := []string{filepath.Join(stateRoot, "telegram")}
	backups, _ := filepath.Glob(filepath.Join(stateRoot, "legacy-telegram-*"))
	for _, path := range append(paths, backups...) {
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			fmt.Fprintf(stderr, "Could not remove the old Telegram state %s: %v\n", path, err)
		}
	}
}
