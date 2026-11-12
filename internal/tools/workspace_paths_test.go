package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toolfiles "github.com/keshon/gektor/internal/tools/files"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkspacePathsLoadsBothMoveScopesOnce(t *testing.T) {
	root := t.TempDir()
	for _, scope := range []string{"source", "target"} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("RULE_"+scope), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	paths := WorkspacePaths{WS: ws}
	text, err := paths.Instructions(json.RawMessage(`{"path":"source/a.txt","from":"source/b.txt","to":"target/b.txt"}`))
	if err != nil || strings.Count(text, "RULE_source") != 1 || strings.Count(text, "RULE_target") != 1 {
		t.Fatal(text, err)
	}
	for _, raw := range []string{`{`, `{"path":"attachment:1"}`, `{}`} {
		text, err := paths.Instructions(json.RawMessage(raw))
		if err != nil || text != "" {
			t.Fatal(raw, text, err)
		}
	}
	if _, err := paths.Instructions(json.RawMessage(`{"from":"source/a.txt","to":"../outside/a.txt"}`)); err == nil {
		t.Fatal("outside destination accepted")
	}
}

func TestWorkspaceResourcesKeepCanonicalPathsAndImageReferences(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	resolver := WorkspacePaths{WS: ws}

	encodedPath := func(name string) string {
		absolute := filepath.Join(ws.Root(), name)
		raw, encodeErr := json.Marshal(absolute)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return string(raw)
	}
	input := encodedPath("input.png")
	output := encodedPath("output")
	document := encodedPath("input.pdf")

	cases := []struct {
		name string
		args string
		want []string
	}{
		{"read_file", `{"path":` + input + `}`, []string{"input.png"}},
		{"move_file", `{"from":` + input + `,"to":` + output + `}`, []string{"input.png", "output"}},
		{"pdf_read", `{"path":` + document + `}`, []string{"input.pdf"}},
		{"archive_create", `{"paths":[` + input + `],"path":` + encodedPath("output.zip") + `}`, []string{"input.png", "output.zip"}},
		{"video_download", `{"url":"https://youtu.be/abcdefghijk","path":` + encodedPath("video.webm") + `}`, []string{"https://youtu.be/abcdefghijk", "video.webm"}},
		{"image_variants", `{"paths":["attachment:1",` + input + `],"widths":[8],"out_dir":` + output + `}`, []string{"attachment:1", "input.png", "output"}},
		{"image_view", `{"path":"attachment:1"}`, []string{"attachment:1"}},
		{"workflow_run", `{"recipe_path":` + encodedPath("recipe.json") + `}`, []string{"recipe.json"}},
		{"image_unknown", `{}`, nil},
		{"not_image_inspect", `{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, resolveErr := resolver.Resources(tc.name, json.RawMessage(tc.args))
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resources = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWorkspaceResourcesRejectInvalidArgumentsAndOutsideDestinations(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	resolver := WorkspacePaths{WS: ws}

	for _, name := range []string{"pdf_read", "archive_create", "video_download", "image_variants", "move_file", "workflow_run"} {
		_, err = resolver.Resources(name, json.RawMessage(`{"path":`))
		if err == nil {
			t.Errorf("%s accepted malformed arguments", name)
		}
	}
	for name, args := range map[string]string{
		"move_file":       `{"from":"input.png","to":"../outside.png"}`,
		"archive_extract": `{"path":"input.zip","out_dir":"../outside"}`,
		"image_variants":  `{"paths":["attachment:1"],"widths":[8],"out_dir":"../outside"}`,
		"video_download":  `{"url":"https://youtu.be/abcdefghijk","path":"../outside.webm"}`,
	} {
		_, err = resolver.Resources(name, json.RawMessage(args))
		if err == nil {
			t.Errorf("%s accepted an outside destination", name)
		}
	}
}

func TestAbsoluteFilePathsShareRelativeAccessAndPermissions(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	path := filepath.Join(root, "calculator.html")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "calculator"})
	if _, err := (toolfiles.Write{WS: ws}).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if data, err := ws.ReadFile("calculator.html"); err != nil || string(data) != "calculator" {
		t.Fatal("absolute write did not use the workspace", err)
	}
	resources, err := (WorkspacePaths{WS: ws}).Resources("write_file", args)
	if err != nil || !reflect.DeepEqual(resources, []string{"calculator.html"}) {
		t.Fatal("absolute path bypasses relative permission rules", resources, err)
	}
	args, _ = json.Marshal(map[string]string{"from": path, "to": filepath.Join(root, "private", "target.html")})
	resources, err = (WorkspacePaths{WS: ws}).Resources("move_file", args)
	if err != nil || !reflect.DeepEqual(resources, []string{"calculator.html", "private/target.html"}) {
		t.Fatal("move permissions omit a path", resources, err)
	}
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "outside.txt"), "content": "bad"})
	if _, err := (toolfiles.Write{WS: ws}).Run(t.Context(), args); err == nil {
		t.Fatal("accepted external absolute write")
	}
}

func TestImageWorkspaceResources(t *testing.T) {
	ws, _ := testWS(t)
	args := json.RawMessage(`{"path":"attachment:1","crop":{"x":10,"y":0,"width":10,"height":10},"width":40}`)
	resources, err := (WorkspacePaths{WS: ws}).Resources("image_view", args)
	if err != nil || len(resources) != 1 || resources[0] != "attachment:1" {
		t.Fatal(resources, err)
	}
}
