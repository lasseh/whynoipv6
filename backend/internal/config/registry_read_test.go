package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRegistryKeysAreRead asserts every registry key appears as a string
// literal somewhere under internal/ or cmd/ — i.e. some binder reads it. A
// registered key nothing reads is a knob an operator sets, watches echo in
// the startup summary, and gets silently ignored. AST-based, not grep:
// a key naming its reader only in a comment must still fail.
func TestRegistryKeysAreRead(t *testing.T) {
	// The key set is identical for every binary — only default values vary.
	keys := registryDefaults("crawler")

	lits := map[string]bool{}
	fset := token.NewFileSet()
	for _, root := range []string{"..", "../../cmd"} { // internal/ and cmd/
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						lits[s] = true
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	for k := range keys {
		if !lits[k] {
			t.Errorf("registry key %q is read by nothing under internal/ or cmd/", k)
		}
	}
}
