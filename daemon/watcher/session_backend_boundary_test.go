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

// tmuxBackendStorageIdentifiers are the tmux backend's storage and identity
// helpers: pane options hold Session labels and the input receipt ledger, and
// a pane id is the tmux Session id. Only the backend may name them.
var tmuxBackendStorageIdentifiers = map[string]bool{
	"isPaneID":                   true,
	"tmuxPaneUserOption":         true,
	"setTmuxPaneUserOption":      true,
	"markCreatedSession":         true,
	"sessionInputReceiptOption":  true,
	"sessionInputPaneGeneration": true,
	"ownershipOption":            true,
	"originOption":               true,
	"tmuxBoolOption":             true,
	"tmuxDelegatedResource":      true,
}

// tmuxBackendOptionPrefixes are the pane option names the tmux backend stores
// Session labels and receipts in.
var tmuxBackendOptionPrefixes = []string{"@mewla_", "mewla_worker_", "mewla_session_input_", "mewla_origin"}

// TestSessionBackendBoundary keeps Session labels, input receipts and Session
// ids inside the backend that owns them: no production code outside
// watcher/tmux_*.go names a tmux pane option or a tmux identity helper. When
// the tmux backend is deleted its storage goes with its files and nothing
// else changes. Tests that drive the tmux backend through a fake tmux are
// part of that backend's coverage and are not checked.
func TestSessionBackendBoundary(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("daemon module root %s: %v", root, err)
	}
	backendHits := 0
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
		inBackend := strings.HasPrefix(rel, "watcher/tmux_")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var what string
			switch n := node.(type) {
			case *ast.Ident:
				if tmuxBackendStorageIdentifiers[n.Name] && file.Name.Name == "watcher" {
					what = "identifier " + n.Name
				}
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(n.Value)
				if err != nil {
					value = n.Value
				}
				for _, prefix := range tmuxBackendOptionPrefixes {
					if strings.Contains(value, prefix) {
						what = "pane option " + strconv.Quote(value)
						break
					}
				}
			}
			if what == "" {
				return true
			}
			if inBackend {
				backendHits++
				return true
			}
			violations = append(violations, fset.Position(node.Pos()).String()+": "+what)
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	// The scan must see the backend's own storage, or it proves nothing.
	if backendHits == 0 {
		t.Fatal("scan found no tmux backend storage in watcher/tmux_*.go")
	}
	for _, violation := range violations {
		t.Errorf("%s is tmux backend storage; reach it through sessionBackend", violation)
	}
}
