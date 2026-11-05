package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
)

func recipeArgs(t *testing.T, ws *workspace.Workspace, source []byte) json.RawMessage {
	t.Helper()
	if err := ws.WriteFile("recipe.json", source, 0600); err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(`{"recipe_path":"recipe.json"}`)
}
func allow(context.Context, string, json.RawMessage) error { return nil }

func TestManualImageRecipeCompletes(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.png", b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"version":1,"name":"web images","steps":[{"id":"inspect","tool":"image_inspect","args":{"paths":["input.png"]}},{"id":"preview","tool":"image_preview","args":{"paths":["input.png"],"widths":[16]}},{"id":"write","tool":"image_variants","args":{"paths":["input.png"],"widths":[16],"out_dir":"web"}},{"id":"compare","tool":"image_compare","args":{"paths":["input.png","web/input-16w.png"]}}]}`)
	source := append([]byte(nil), args...)
	args = recipeArgs(t, ws, source)
	if _, err := (Tool{WS: ws, ValidateOnly: true, Authorize: allow}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Stat(".gektor"); !os.IsNotExist(err) {
		t.Fatal("validation wrote state")
	}
	authorized := 0
	out, err := (Tool{WS: ws, AuthorizeOutput: allow, Authorize: func(context.Context, string, json.RawMessage) error { authorized++; return nil }}).Run(t.Context(), args)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var result struct {
		Path   string            `json:"record_path"`
		Record automation.Record `json:"record"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	data, err := ws.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result.Record); err != nil {
		t.Fatal(err)
	}
	if result.Record.RecipeSource != string(source) || result.Record.RecipeSHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) {
		t.Fatal("recipe source or digest mismatch")
	}
	if authorized != 5 || result.Record.Status != "completed" || result.Record.Finished == nil {
		t.Fatalf("bad completion: %s", out)
	}
	if _, err := ws.ReadFile(result.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Stat("web/input-16w.png"); err != nil {
		t.Fatal(err)
	}
	for _, s := range result.Record.Steps {
		if s.Status != "completed" {
			t.Fatalf("step not completed: %+v", s)
		}
		var evidence struct {
			Sources []struct {
				Unchanged *bool  `json:"unchanged"`
				Before    string `json:"sha256_before"`
				After     string `json:"sha256_after"`
			} `json:"sources"`
		}
		if err := json.Unmarshal([]byte(s.Output), &evidence); err != nil {
			t.Fatal(err)
		}
		if len(evidence.Sources) == 0 || evidence.Sources[0].Unchanged == nil || !*evidence.Sources[0].Unchanged || evidence.Sources[0].Before != evidence.Sources[0].After {
			t.Fatalf("missing hash evidence: %s", s.Output)
		}
	}
}

func TestManualRunRecordsFailureAndRejectsHiddenCommands(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	tool := Tool{WS: ws, Authorize: func(context.Context, string, json.RawMessage) error { return nil }}
	for _, args := range []string{
		`{"version":1,"name":"bad","steps":[{"id":"one","tool":"image_inspect","args":{"paths":["a.png"]}},{"id":"two","tool":"run_shell","args":{"command":"echo nope"}}]}`,
		`{"version":1,"name":"bad","steps":[{"id":"one","tool":"image_variants","args":{"paths":["a.png"],"widths":[8],"out_dir":"../escape"}}]}`,
	} {
		if _, err := tool.Run(t.Context(), recipeArgs(t, ws, []byte(args))); err == nil {
			t.Fatal("accepted invalid workflow")
		}
	}
	if _, err := ws.Stat(".gektor/workflow-runs"); !os.IsNotExist(err) {
		t.Fatal("invalid workflow created records")
	}
	out, err := tool.Run(t.Context(), recipeArgs(t, ws, []byte(`{"version":1,"name":"missing image","steps":[{"id":"read","tool":"image_inspect","args":{"paths":["missing.png"]}},{"id":"later","tool":"image_inspect","args":{"paths":["missing.png"]}}]}`)))
	if err == nil {
		t.Fatal("missing image succeeded")
	}
	var result struct {
		Path   string            `json:"record_path"`
		Record automation.Record `json:"record"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	data, err := ws.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	var saved automation.Record
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status != "failed" || saved.Steps[0].Status != "failed" || saved.Steps[1].Status != "skipped" {
		t.Fatalf("wrong record: %s", data)
	}
}

func TestRunRevalidatesSavedFile(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	source := `{"version":1,"name":"test","steps":[{"id":"read","tool":"image_inspect","args":{"paths":["input.png"]}}]}`
	args := recipeArgs(t, ws, []byte(source))
	if _, err := (Tool{WS: ws, ValidateOnly: true, Authorize: allow}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	recipeArgs(t, ws, []byte(strings.Replace(source, `"version":1`, `"version":1,"input":"bad"`, 1)))
	for _, validate := range []bool{true, false} {
		_, err := (Tool{WS: ws, ValidateOnly: validate, Authorize: allow}).Run(t.Context(), args)
		if err == nil || !strings.Contains(err.Error(), "input") || !strings.Contains(err.Error(), "recipe.json") {
			t.Fatalf("saved invalid recipe accepted: %v", err)
		}
	}
	if _, err := ws.Stat(".gektor"); !os.IsNotExist(err) {
		t.Fatal("invalid recipe created state")
	}
	_, err = (Tool{WS: ws, ValidateOnly: true, Authorize: func(_ context.Context, name string, _ json.RawMessage) error {
		if name == "read_file" {
			return fmt.Errorf("denied")
		}
		return nil
	}}).Run(t.Context(), args)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal("read permission bypassed")
	}
}
