package settings

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/keshon/gektor/internal/tui/components/form"
)

func TestDirectoryScanCopiesExtensionFilter(t *testing.T) {
	root := t.TempDir()
	jsonPath := filepath.Join(root, "recipe.json")
	textPath := filepath.Join(root, "notes.txt")
	for _, path := range []string{jsonPath, textPath} {
		err := os.WriteFile(path, []byte("{}"), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel([]form.Field{{Key: "recipe", Value: root}}, textarea.New())
	extensions := []string{".json"}
	model.FileFields = map[string]FilePicker{"recipe": {Root: root, Extensions: extensions}}
	model.Focus(0, model.FieldVisible)

	command := model.OpenPicker(Props{Width: 80, Height: 24})
	extensions[0] = ".txt"
	result := command().(ScanMsg)
	if result.Err != nil || !slices.Contains(result.Items, jsonPath) || slices.Contains(result.Items, textPath) {
		t.Fatalf("scan retained mutable filter: %+v", result)
	}
}
