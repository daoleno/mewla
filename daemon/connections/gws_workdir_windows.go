package connections

import "os"

func ownedByUs(os.FileInfo) bool { return true }
