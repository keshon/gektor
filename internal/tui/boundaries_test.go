package tui

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
