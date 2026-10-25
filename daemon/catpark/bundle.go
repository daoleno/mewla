package catpark

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// MaxArchiveParts bounds how finely a client may slice the bundle.
const MaxArchiveParts = 32

var archivePartName = regexp.MustCompile(`^bundle-(\d{1,2})-(\d{1,2})\.tar$`)

// ArchivePartName is where one slice of the bundle is served as a tar, so a
// phone on a slow link pays a few round trips instead of one per file, and a
// dropped connection costs one slice, not the whole bundle.
func ArchivePartName(part, of int) string {
	return fmt.Sprintf("bundle-%d-%d.tar", part, of)
}

// ParseArchivePart reads a name made by ArchivePartName.
func ParseArchivePart(name string) (part, of int, ok bool) {
	match := archivePartName.FindStringSubmatch(name)
	if match == nil {
		return 0, 0, false
	}
	part, _ = strconv.Atoi(match[1])
	of, _ = strconv.Atoi(match[2])
	return part, of, of >= 1 && of <= MaxArchiveParts && part < of
}

//go:embed all:web
var embedded embed.FS

// Bundle is the game page and its art, one implementation for every client:
// the browser's web UI frames it, and the phone app syncs it into its cache.
func Bundle() fs.FS {
	files, err := fs.Sub(embedded, "web")
	if err != nil {
		panic(err)
	}
	return files
}

// File is one entry of the bundle manifest.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest lists every file with its digest. Version changes whenever any
// file does, so a client that holds the same version holds the same page.
type Manifest struct {
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// BuildManifest hashes a bundle.
func BuildManifest(files fs.FS) (Manifest, error) {
	var manifest Manifest
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, File{Path: path, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	whole := sha256.New()
	for _, file := range manifest.Files {
		whole.Write([]byte(file.Path + "\x00" + file.SHA256 + "\n"))
	}
	manifest.Version = hex.EncodeToString(whole.Sum(nil))[:16]
	return manifest, nil
}

// ArchivePart packs the manifest's files whose position is part modulo of into
// one tar, in manifest order: slices are about the same size, and a client
// that holds the manifest knows which slice carries which file. The phone still
// checks each entry against the manifest's digest, so a slice carries no
// authority of its own.
func ArchivePart(files fs.FS, manifest Manifest, part, of int) ([]byte, error) {
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for i, file := range manifest.Files {
		if i%of != part {
			continue
		}
		data, err := fs.ReadFile(files, file.Path)
		if err != nil {
			return nil, err
		}
		if err := writer.WriteHeader(&tar.Header{Name: file.Path, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
