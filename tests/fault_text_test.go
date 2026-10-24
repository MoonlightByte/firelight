package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// gluedNumber is a bound word run into its number, as in "at most128": text a
// fault reports to hosts must read "at most 128".
var gluedNumber = regexp.MustCompile(`\b(most|least|and|than|of|to|minus)\d`)

// No string literal in the engine's source glues a number to its word.
func TestFaultTextSpacing(t *testing.T) {
	files := 0
	for _, dir := range []string{"../core", "../compiler", "../cmd", "../livestate", "../nativesave"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			set := token.NewFileSet()
			f, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return err
			}
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err == nil && gluedNumber.MatchString(s) {
					t.Errorf("%s: %q glues a number to its word", set.Position(lit.Pos()), s)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files == 0 {
		t.Fatal("no source scanned")
	}
}
