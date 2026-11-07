package conventions

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var docIdentifier = regexp.MustCompile("`([a-z][a-z0-9]*\\.[A-Z][A-Za-z0-9]*|[A-Za-z][A-Za-z0-9]*[A-Z][A-Za-z0-9]*)`")

type declarations struct {
	all      map[string]bool
	packages map[string]map[string]bool
}

func (d *declarations) add(f *ast.File) {
	if d.all == nil {
		d.all = map[string]bool{}
		d.packages = map[string]map[string]bool{}
	}
	pkg := d.packages[f.Name.Name]
	if pkg == nil {
		pkg = map[string]bool{}
		d.packages[f.Name.Name] = pkg
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			d.all[n.Name.Name] = true
			if n.Recv == nil {
				pkg[n.Name.Name] = true
			}
			return false // Function bodies and comments are not declarations in docs.
		case *ast.TypeSpec:
			d.all[n.Name.Name], pkg[n.Name.Name] = true, true
		case *ast.ValueSpec:
			for _, name := range n.Names {
				d.all[name.Name], pkg[name.Name] = true, true
			}
		case *ast.Field:
			for _, name := range n.Names {
				d.all[name.Name] = true
			}
		}
		return true
	})
}

func (d declarations) has(name string) bool {
	pkg, member, qualified := strings.Cut(name, ".")
	if !qualified {
		return d.all[name]
	}
	if names, ok := d.packages[pkg]; ok {
		return names[member]
	}
	// Only top-level standard-library package names fit the recognized syntax.
	p, err := importer.Default().Import(pkg)
	return err == nil && p.Scope().Lookup(member) != nil && ast.IsExported(member)
}

func checkDocIdentifiers(t *testing.T, r *repo) {
	var names declarations
	for _, path := range r.goFiles {
		f, err := parser.ParseFile(token.NewFileSet(), path, r.read(t, path), 0)
		if err != nil {
			t.Fatalf("parse %s: %v", r.rel(path), err)
		}
		names.add(f)
	}
	for _, doc := range []string{"ARCHITECTURE.md", "README.md", "docs/conventions.md"} {
		for _, match := range docIdentifier.FindAllStringSubmatch(r.readPath(t, doc), -1) {
			if !names.has(match[1]) {
				t.Errorf("%s names undeclared Go name %q%s", doc, match[1], r.rule("doc-identifiers"))
			}
		}
	}
}

func checkPackagePath(root, name string) error {
	files, err := filepath.Glob(filepath.Join(root, "internal", filepath.FromSlash(name), "*.go"))
	if err != nil {
		return fmt.Errorf("invalid package path %q: %w", name, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("internal/%s has no Go source", name)
	}
	return nil
}

// Only explicit root-relative source references are checked. This excludes
// commands, example workspace files, placeholders and paths outside the repo.
var sourcePath = regexp.MustCompile(`^(?:internal/|cmd/)[A-Za-z0-9_./*?\[\]-]+$|^[A-Za-z0-9_./-]+\.go$`)
var inlineCode = regexp.MustCompile("`([^`\r\n]+)`")

func missingDocPaths(root, text string) []string {
	var missing []string
	for _, match := range inlineCode.FindAllStringSubmatch(text, -1) {
		path := match[1]
		if !sourcePath.MatchString(path) || filepath.IsAbs(path) || strings.HasPrefix(path, "../") {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || len(matches) == 0 {
			missing = append(missing, path)
		}
	}
	return missing
}

func maintainedDoc(path string) bool {
	if !strings.HasSuffix(path, ".md") {
		return false
	}
	if !strings.Contains(path, "/") || strings.HasPrefix(path, "docs/") && !strings.HasPrefix(path, "docs/internal/") {
		return true
	}
	return path == "skills/README.md" || path == "eval/README.md" ||
		(strings.HasPrefix(path, "examples/") || strings.HasPrefix(path, "demos/")) && filepath.Base(path) == "README.md"
}

func checkDocPaths(t *testing.T, r *repo) {
	err := filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := r.rel(path)
		if entry.IsDir() {
			if rel != "." && !strings.Contains(rel, "/") && rel != "docs" && rel != "skills" && rel != "eval" && rel != "examples" && rel != "demos" {
				return filepath.SkipDir
			}
			if rel == "docs/internal" || rel == "eval/results" {
				return filepath.SkipDir
			}
			return nil
		}
		if maintainedDoc(rel) {
			for _, missing := range missingDocPaths(r.root, r.read(t, path)) {
				t.Errorf("%s names missing path %q%s", rel, missing, r.rule("doc-paths"))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk documentation: %v", err)
	}
}

func TestDocumentationRejectsMissingSourcePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "example"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "example", "real.go"), []byte("package example"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"internal/example", "internal/example/*.go", "internal/example/real.go", "C:/outside/main.go", "../workspace/main.go", "go run ./cmd/gektor", "internal/<name>/main.go"} {
		if missing := missingDocPaths(root, "`"+path+"`"); len(missing) != 0 {
			t.Errorf("%q rejected: %v", path, missing)
		}
	}
	for _, path := range []string{"internal/harness", "cmd/missing/main.go", "absent.go", "internal/example/*.txt"} {
		if missing := missingDocPaths(root, "`"+path+"`"); len(missing) != 1 {
			t.Errorf("%q not rejected: %v", path, missing)
		}
	}
	if err := checkPackagePath(root, "harness"); err == nil {
		t.Fatal("missing package accepted by layer rule")
	}
	if err := checkPackagePath(root, "example"); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentationUsesDeclarationsInsteadOfSourceMentions(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "example.go", `package example
// GhostName is only a comment.
const Text = "StringOnly"
type ActualType struct { FieldName string }
func ActualFunction() {}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names declarations
	names.add(f)
	for _, name := range []string{"ActualType", "FieldName", "example.ActualFunction", "os.Root"} {
		if !names.has(name) {
			t.Errorf("declaration %q missing", name)
		}
	}
	for _, name := range []string{"GhostName", "StringOnly", "example.FieldName", "example.Root"} {
		if names.has(name) {
			t.Errorf("non-declaration %q accepted", name)
		}
	}
}

func TestMaintainedDocumentationScope(t *testing.T) {
	for _, path := range []string{"README.md", "SECURITY.md", "docs/design/tui/README.md", "docs/conventions.md", "skills/README.md", "eval/README.md", "examples/workflows/README.md", "demos/README.md"} {
		if !maintainedDoc(path) {
			t.Errorf("guide %q excluded", path)
		}
	}
	for _, path := range []string{"docs/internal/history.md", "eval/results/old/README.md", "eval/prompts/task.md", "skills/go/SKILL.md", "sandbox/README.md"} {
		if maintainedDoc(path) {
			t.Errorf("non-guide %q included", path)
		}
	}
}
