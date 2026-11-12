package agenttools

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

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow"
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

func TestRunRejectsMalformedArgumentsBeforeAuthorization(t *testing.T) {
	tool := Tool{
		Registry: func() (*workflow.Registry, error) { return workflow.NewRegistry() },
		Authorize: func(context.Context, string, json.RawMessage) error {
			t.Fatal("invalid arguments reached authorization")
			return nil
		},
	}
	for _, args := range []string{
		`{}`,
		`{"recipe_path":""}`,
		`{"recipe_path":"recipe.json","unknown":true}`,
		`{"recipe_path":"recipe.json","inputs":null}`,
		`{"recipe_path":"recipe.json"} {}`,
		`{"recipe_path":"recipe.json","inputs":{"value":"` + strings.Repeat("x", 64<<10) + `"}}`,
	} {
		_, err := tool.Run(t.Context(), json.RawMessage(args))
		if err == nil || strings.Contains(err.Error(), "workspace unavailable") {
			t.Fatalf("expected argument rejection, got %v", err)
		}
	}
}

func TestVersionTwoBindsInputsAndImageOutput(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.png", imageBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"version":3,"name":"Reusable variants","inputs":{"source":{"type":"string","required":true},"destination":{"type":"string","default":"web"}},"steps":[{"id":"write","tool":"image_variants","args":{"paths":[{"$input":"source"}],"widths":[16],"out_dir":{"$input":"destination"}}},{"id":"compare","tool":"image_compare","args":{"paths":[{"$input":"source"},{"$step":"write","pointer":"/items/0/output"}]}}]}`)
	recipeArgs(t, ws, source)
	args := json.RawMessage(`{"recipe_path":"recipe.json","inputs":{"source":"input.png"}}`)
	tool := Tool{WS: ws, StoreDir: t.TempDir(), ValidateOnly: true, Authorize: allow, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), allow, nil)
	},
	}
	if _, err := tool.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Stat("web"); !os.IsNotExist(err) {
		t.Fatal("validation wrote outputs")
	}
	tool.ValidateOnly = false
	out, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var result struct {
		Path string `json:"record_path"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	var record workflow.Run
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || string(record.Inputs["destination"]) != `"web"` || strings.Contains(string(record.Steps[1].Args), "$step") || !strings.Contains(string(record.Steps[1].Args), "web/input-16w.png") {
		t.Fatalf("wrong binding/evidence: %s", data)
	}
	if record.RecipeSource != string(source) {
		t.Fatal("source recipe changed")
	}
}

func TestOutputDerivedPathsAreAuthorizedAndValidated(t *testing.T) {
	for _, mode := range []string{"deny", "wrong-type", "literal-escape", "forbidden-tool", "string-interpolation"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			var imageBytes bytes.Buffer
			if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
				t.Fatal(err)
			}
			if err := ws.WriteFile("input.png", imageBytes.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			source := `{"version":3,"name":"Dynamic paths","steps":[{"id":"write","tool":"image_variants","args":{"paths":["input.png"],"widths":[16],"out_dir":"web"}},{"id":"read","tool":"image_inspect","args":{"paths":[{"$step":"write","pointer":"/items/0/output"}]}}]}`
			switch mode {
			case "string-interpolation":
				source = strings.Replace(source, `"input.png"`, `"{$input:source}"`, 1)
			case "wrong-type":
				source = strings.Replace(source, "/items/0/output", "/items", 1)
			case "literal-escape":
				source = strings.Replace(source, `"paths":[{"$step"`, `"paths":["../escape.png",{"$step"`, 1)
			case "forbidden-tool":
				source = strings.Replace(source, `"tool":"image_inspect"`, `"tool":"run_shell"`, 1)
			}
			args := recipeArgs(t, ws, []byte(source))
			dynamicAuthorized := false
			tool := Tool{WS: ws, StoreDir: t.TempDir(), Authorize: func(_ context.Context, name string, args json.RawMessage) error {
				if name == "image_inspect" && strings.Contains(string(args), "web/input-16w.png") {
					dynamicAuthorized = true
					if mode == "deny" {
						return fmt.Errorf("denied resolved path")
					}
				}
				return nil
			}, Registry: func() (*workflow.Registry, error) {
				return app.WorkflowRegistry(ws, t.TempDir(), allow, nil)
			},
			}
			out, err := tool.Run(t.Context(), args)
			if err == nil {
				t.Fatalf("accepted %s: %s", mode, out)
			}
			if mode == "string-interpolation" && !strings.Contains(err.Error(), "JSON object") {
				t.Fatalf("unhelpful reference diagnostic: %v", err)
			}
			if mode == "literal-escape" || mode == "forbidden-tool" || mode == "string-interpolation" {
				if _, err := ws.Stat("web"); !os.IsNotExist(err) {
					t.Fatal("invalid template had effects")
				}
			} else {
				if _, err := ws.Stat("web/input-16w.png"); err != nil {
					t.Fatal("earlier step did not complete", err)
				}
				if !strings.Contains(out, `"status":"failed"`) {
					t.Fatalf("missing failure record: %s", out)
				}
			}
			if mode == "deny" && !dynamicAuthorized {
				t.Fatal("resolved path skipped authorization")
			}
		})
	}
}

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
	args := json.RawMessage(`{"version":3,"name":"web images","steps":[{"id":"inspect","tool":"image_inspect","args":{"paths":["input.png"]}},{"id":"preview","tool":"image_preview","args":{"paths":["input.png"],"widths":[16]}},{"id":"write","tool":"image_variants","args":{"paths":["input.png"],"widths":[16],"out_dir":"web"}},{"id":"compare","tool":"image_compare","args":{"paths":["input.png","web/input-16w.png"]}}]}`)
	source := append([]byte(nil), args...)
	args = recipeArgs(t, ws, source)
	if _, err := (Tool{WS: ws, StoreDir: t.TempDir(), ValidateOnly: true, Authorize: allow, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	},
	}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Stat(".gektor"); !os.IsNotExist(err) {
		t.Fatal("validation wrote state")
	}
	authorized := 0
	out, err := (Tool{WS: ws, StoreDir: t.TempDir(), Authorize: func(context.Context, string, json.RawMessage) error { authorized++; return nil }, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), allow, nil)
	},
	}).Run(t.Context(), args)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var result struct {
		Path   string       `json:"record_path"`
		Record workflow.Run `json:"record"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
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
	if _, err := os.ReadFile(result.Path); err != nil {
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
	tool := Tool{WS: ws, StoreDir: t.TempDir(), Authorize: func(context.Context, string, json.RawMessage) error { return nil }, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	},
	}
	for _, args := range []string{
		`{"version":3,"name":"bad","steps":[{"id":"one","tool":"image_inspect","args":{"paths":["a.png"]}},{"id":"two","tool":"run_shell","args":{"command":"echo nope"}}]}`,
		`{"version":3,"name":"bad","steps":[{"id":"one","tool":"image_variants","args":{"paths":["a.png"],"widths":[8],"out_dir":"../escape"}}]}`,
	} {
		if _, err := tool.Run(t.Context(), recipeArgs(t, ws, []byte(args))); err == nil {
			t.Fatal("accepted invalid workflow")
		}
	}
	if _, err := ws.Stat(".gektor/workflow-runs"); !os.IsNotExist(err) {
		t.Fatal("invalid workflow created records")
	}
	out, err := tool.Run(t.Context(), recipeArgs(t, ws, []byte(`{"version":3,"name":"missing image","steps":[{"id":"read","tool":"image_inspect","args":{"paths":["missing.png"]}},{"id":"later","tool":"image_inspect","args":{"paths":["missing.png"]}}]}`)))
	if err == nil {
		t.Fatal("missing image succeeded")
	}
	var result struct {
		Path   string       `json:"record_path"`
		Record workflow.Run `json:"record"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	var saved workflow.Run
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
	source := `{"version":3,"name":"test","steps":[{"id":"read","tool":"image_inspect","args":{"paths":["input.png"]}}]}`
	args := recipeArgs(t, ws, []byte(source))
	if _, err := (Tool{WS: ws, StoreDir: t.TempDir(), ValidateOnly: true, Authorize: allow, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	},
	}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	recipeArgs(t, ws, []byte(strings.Replace(source, `"version":3`, `"version":3,"input":"bad"`, 1)))
	for _, validate := range []bool{true, false} {
		_, err := (Tool{WS: ws, StoreDir: t.TempDir(), ValidateOnly: validate, Authorize: allow, Registry: func() (*workflow.Registry, error) {
			return app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
		},
		}).Run(t.Context(), args)
		if err == nil || !strings.Contains(err.Error(), "input") || !strings.Contains(err.Error(), "recipe.json") {
			t.Fatalf("saved invalid recipe accepted: %v", err)
		}
	}
	if _, err := ws.Stat(".gektor"); !os.IsNotExist(err) {
		t.Fatal("invalid recipe created state")
	}
	_, err = (Tool{WS: ws, StoreDir: t.TempDir(), ValidateOnly: true, Authorize: func(_ context.Context, name string, _ json.RawMessage) error {
		if name == "read_file" {
			return fmt.Errorf("denied")
		}
		return nil
	}, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	},
	}).Run(t.Context(), args)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal("read permission bypassed")
	}
}
