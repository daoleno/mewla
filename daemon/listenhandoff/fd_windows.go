//go:build windows

package listenhandoff

import "os"

// Windows: net.FileListener is not implemented, so a listening socket cannot
// be handed on; see the package comment.
const supported = false

func clearCloseOnExec(*os.File) bool { return false }

func markCloseOnExec(*os.File) {}
