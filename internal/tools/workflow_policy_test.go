package tools

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowCannotBypassNestedToolDeny(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &Capabilities{}
	r, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	if r.Has("image_inspect") || r.Has("workflow_run") {
		t.Fatal("optional schemas loaded eagerly")
	}
	BindWorkflowPolicy(c, ws, permission.Policy{Rules: []permission.Rule{{Tool: "image_inspect", Pattern: "*", Effect: permission.Deny}}}, nil, t.TempDir())
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":3,"name":"denied","steps":[{"id":"read","tool":"image_inspect","args":{"paths":["input.png"]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`)); err == nil {
		t.Fatal("workflow bypassed image_inspect deny")
	}
	if _, err := ws.Stat(".gektor/workflow-runs"); !os.IsNotExist(err) {
		t.Fatal("denied workflow had effects")
	}
	paths, err := PathResources(ws, "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || len(paths) != 1 || paths[0] != "recipe.json" {
		t.Fatalf("missing nested resources: %v %v", paths, err)
	}
}

func TestGeneratedOutputFilenameCannotBypassPermission(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.png", data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Capabilities{}
	r, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	BindWorkflowPolicy(c, ws, permission.Policy{Rules: []permission.Rule{{Tool: "image_variants", Pattern: "out/input-8w.png", Effect: permission.Deny}}}, nil, t.TempDir())
	for _, id := range []string{"native_images", "native_workflows"} {
		if _, err := c.Enable(t.Context(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
	args := json.RawMessage(`{"paths":["input.png"],"widths":[8],"out_dir":"out"}`)
	if _, err := r.Run(t.Context(), "image_variants", args); err == nil {
		t.Fatal("direct call bypassed filename deny")
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":3,"name":"denied output","steps":[{"id":"write","tool":"image_variants","args":{"paths":["input.png"],"widths":[8],"out_dir":"out"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`)); err == nil {
		t.Fatal("workflow bypassed filename deny")
	}
	if _, err := ws.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("denied outputs created a directory")
	}
}
