package watcher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// hostBackendStorageIdentifiers are the session host backend's label and data
// keys and its id scheme. Only that backend may name them.
var hostBackendStorageIdentifiers = map[string]bool{
	"hostLabelName":          true,
	"hostLabelOrigin":        true,
	"hostLabelHidden":        true,
	"hostLabelDelegated":     true,
	"hostLabelExecutor":      true,
	"hostLabelAgentControl":  true,
	"hostLabelPiSession":     true,
	"hostLabelResourceUnit":  true,
	"hostLabelResourceOwner": true,
	"hostDataInputReceipts":  true,
	"hostSessionIDPattern":   true,
	"newHostSessionID":       true,
}

const sessionHostImport = "github.com/daoleno/mewla/daemon/sessionhost"

// TestSessionBackendBoundary keeps Session labels, input receipts and Session
// ids inside the backend that owns them: no watcher code outside
// watcher/host_*.go imports the session host or names its label, data or id
// helpers. Tests that drive a backend through a fake are part of that
// backend's coverage and are not checked. It also keeps Mewla from running
// tmux: no production code names a "tmux" program.
func TestSessionBackendBoundary(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("daemon module root %s: %v", root, err)
	}
	hostHits := 0
	var violations []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "testdata", "vendor", "node_modules", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		inHost := strings.HasPrefix(rel, "watcher/host_")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		inWatcher := file.Name.Name == "watcher"
		ast.Inspect(file, func(node ast.Node) bool {
			var what string
			host := false
			switch n := node.(type) {
			case *ast.ImportSpec:
				if value, err := strconv.Unquote(n.Path.Value); err == nil && value == sessionHostImport && inWatcher {
					what, host = "import of "+value, true
				}
				return false
			case *ast.Ident:
				if hostBackendStorageIdentifiers[n.Name] && inWatcher {
					what, host = "identifier "+n.Name, true
				}
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(n.Value)
				if err != nil {
					value = n.Value
				}
				if value == "tmux" {
					what = "the program " + strconv.Quote(value)
				}
			}
			if what == "" {
				return true
			}
			switch {
			case host && inHost:
				hostHits++
			case host:
				violations = append(violations, fset.Position(node.Pos()).String()+": "+what+" is session host backend storage; reach it through sessionBackend")
			default:
				violations = append(violations, fset.Position(node.Pos()).String()+": "+what+": Mewla never runs tmux")
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	// The scan must see the backend's own storage, or it proves nothing.
	if hostHits == 0 {
		t.Fatal("scan found no session host backend storage in watcher/host_*.go")
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}
