package screen

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

// Notify posts a Notification Center banner on this Mac. macOS also shows
// its own Screen Sharing indicator in the menu bar while a viewer is attached.
func Notify(title, body string) {
	script := "display notification " + strconv.Quote(body) + " with title " + strconv.Quote(title)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "osascript", "-e", script).Run()
	}()
}
