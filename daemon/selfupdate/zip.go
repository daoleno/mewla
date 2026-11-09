package selfupdate

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
)

// extractZipBinary reads mewla.exe from the Windows release zip. The zip
// holds exactly LICENSE, NOTICE, TRADEMARKS.md and mewla.exe at its root,
// the same layout install.ps1 checks.
func extractZipBinary(archive []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open daemon archive: %w", err)
	}
	var binary []byte
	for _, file := range reader.File {
		if path.Clean(file.Name) != WindowsBinary {
			continue
		}
		if binary != nil {
			return nil, errors.New("daemon archive has duplicate mewla.exe entries")
		}
		if !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > maxDaemonArchive {
			return nil, errors.New("daemon archive has invalid mewla.exe entry")
		}
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("read daemon archive: %w", err)
		}
		binary, err = io.ReadAll(io.LimitReader(opened, int64(file.UncompressedSize64)+1))
		opened.Close()
		if err != nil || uint64(len(binary)) != file.UncompressedSize64 {
			return nil, errors.New("read mewla.exe from archive")
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("daemon archive does not contain mewla.exe")
	}
	return binary, nil
}
