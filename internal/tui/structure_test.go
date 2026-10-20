package tui

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

func TestPaletteLivesInStyles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if name == "styles.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			if len(value) == 7 && value[0] == '#' {
				if _, err := strconv.ParseUint(value[1:], 16, 24); err == nil {
					t.Errorf("%s contains a palette literal outside styles.go", name)
				}
			}
			return true
		})
	}
}

// Direct model writes are forbidden in the application render helpers. Calls
// into helpers or third-party widgets still require behavioral/race coverage.
func TestRenderHelpersDoNotAssignModelFields(t *testing.T) {
	methods := map[string]bool{"View": true, "screenContent": true, "badgeLine": true, "headerGeometry": true, "sidebarView": true, "sidebarDetails": true, "searchView": true, "dialogView": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !methods[fn.Name.Name] {
				continue
			}
			receiver := fn.Recv.List[0].Names[0].Name
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if assignment, ok := node.(*ast.AssignStmt); ok {
					for _, left := range assignment.Lhs {
						if writesReceiver(left, receiver) {
							t.Errorf("%s.%s writes model state", name, fn.Name.Name)
						}
					}
				}
				if change, ok := node.(*ast.IncDecStmt); ok && writesReceiver(change.X, receiver) {
					t.Errorf("%s.%s changes model state", name, fn.Name.Name)
				}
				return true
			})
		}
	}
}

func writesReceiver(expr ast.Expr, receiver string) bool {
	switch expr := expr.(type) {
	case *ast.SelectorExpr:
		return writesReceiver(expr.X, receiver)
	case *ast.IndexExpr:
		return writesReceiver(expr.X, receiver)
	case *ast.Ident:
		return expr.Name == receiver
	}
	return false
}

func TestRenderWriteCheckRecognizesNestedAssignments(t *testing.T) {
	for _, source := range []string{"m.header.row", "m.nav.entries[0]", "m.dialog.offset"} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if !writesReceiver(expr, "m") {
			t.Fatalf("check missed %s", source)
		}
	}
	expr, _ := parser.ParseExpr("local.offset")
	if writesReceiver(expr, "m") {
		t.Fatal("check rejected local rendering state")
	}
}

func TestComponentsDoNotImportApplicationPackages(t *testing.T) {
	err := filepath.WalkDir("components", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(name, "github.com/keshon/gektor/") && !strings.HasPrefix(name, "github.com/keshon/gektor/internal/tui/components/") {
				t.Errorf("%s imports application package %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSettingsDoesNotOwnRuntimeServices(t *testing.T) {
	err := filepath.WalkDir("settings", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, owner := range []string{"app"} {
				prefix := "github.com/keshon/gektor/internal/" + owner
				if name == prefix || strings.HasPrefix(name, prefix+"/") {
					t.Errorf("%s imports runtime owner %s", path, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
