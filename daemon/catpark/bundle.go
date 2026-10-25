package catpark

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
)

// ArchiveName is where the whole bundle is served as one tar, so a phone on a
// slow link pays one round trip instead of one per file.
const ArchiveName = "bundle.tar"

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

// Archive packs every file of the manifest, in its order, into one tar. The
// phone still checks each entry against the manifest's digest, so the archive
// carries no authority of its own.
func Archive(files fs.FS, manifest Manifest) ([]byte, error) {
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for _, file := range manifest.Files {
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
