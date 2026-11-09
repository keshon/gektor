package agenttools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowContractsAndRunnableExample(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	out, err := (Schema{WS: ws, StoreDir: base}).Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Contracts map[string]automation.Contract `json:"operation_contracts"`
		Examples  []json.RawMessage              `json:"argument_examples"`
	}
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatal(err)
	}
	script := catalog.Contracts["script_transform"]
	if !strings.Contains(script.Description, "JavaScript") || !script.ReadOnly || !script.RetrySafe || string(script.OutputSchema) != "true" {
		t.Fatal(script)
	}
	for _, name := range []string{"artifact_write", "artifact_read_text", "http_request", "model_generate", "record_read", "run_program", "image_inspect"} {
		c := catalog.Contracts[name]
		if len(c.OutputSchema) == 0 || string(c.OutputSchema) == "true" {
			t.Fatal("missing output contract", name)
		}
	}
	args := recipeArgs(t, ws, catalog.Examples[0])
	for _, tc := range []struct {
		args json.RawMessage
		want string
	}{{args, `"doubled":6`}, {json.RawMessage(`{"recipe_path":"recipe.json","inputs":{"count":11}}`), `"doubled":22`}} {
		out, err := (Tool{WS: ws, StoreDir: base, Authorize: allow}).Run(t.Context(), tc.args)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatal(out, err)
		}
	}
}

const draftRecipe = `{"version":3,"name":"Draft","inputs":{"exact":{"type":"integer","default":9007199254740993}},"steps":[{"id":"inspect","tool":"image_inspect","args":{"paths":["image.png"]}}]}`

func TestSaveRecipeIsScopedAndPreservesNumbers(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	save := &Save{WS: ws, Path: "recipes/chosen.json"}
	args := json.RawMessage(`{"recipe":` + draftRecipe + `}`)
	if _, err := save.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if !save.Saved {
		t.Fatal("save was not recorded")
	}
	data, err := ws.ReadFile(save.Path)
	if err != nil || !strings.Contains(string(data), "9007199254740993") {
		t.Fatal("integer precision lost", err, string(data))
	}
	for _, bad := range []string{
		`{"path":"other.json","recipe":` + draftRecipe + `}`,
		`{"recipe":` + strings.Replace(draftRecipe, "image_inspect", "run_shell", 1) + `}`,
		`{"recipe":{}}`,
	} {
		if _, err := save.Run(t.Context(), json.RawMessage(bad)); err == nil {
			t.Fatal("accepted unsafe save", bad)
		}
	}
	if _, err := ws.Stat("other.json"); !os.IsNotExist(err) {
		t.Fatal("save was redirected")
	}
	if _, err := (&Save{WS: ws, Path: "../outside.json"}).Run(t.Context(), args); err == nil {
		t.Fatal("save escaped workspace")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "recipes"))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked", entries, err)
	}
}

func TestSaveReplacesLinkWithoutChangingOtherFile(t *testing.T) {
	for _, symbolic := range []bool{false, true} {
		t.Run(map[bool]string{false: "hardlink", true: "symlink"}[symbolic], func(t *testing.T) {
			dir := t.TempDir()
			other := filepath.Join(dir, "other.json")
			if err := os.WriteFile(other, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "recipe.json")
			var err error
			if symbolic {
				err = os.Symlink(other, link)
			} else {
				err = os.Link(other, link)
			}
			if err != nil {
				t.Skipf("link unavailable: %v", err)
			}
			ws, err := workspace.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if _, err := (&Save{WS: ws, Path: "recipe.json"}).Run(t.Context(), json.RawMessage(`{"recipe":`+draftRecipe+`}`)); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(other)
			if err != nil || string(data) != "original" {
				t.Fatal("save followed a link", string(data), err)
			}
		})
	}
}

func TestWorkflowSchemaAdvertisesOnlySupportedOperations(t *testing.T) {
	out, err := (Schema{}).Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Recipe     json.RawMessage
		Operations map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"media_status", "video_info", "video_transcript", "video_download", "media_inspect", "media_convert", "media_clip", "media_frame", "media_merge"} {
		if result.Operations[name] == nil {
			t.Fatal("missing media operation", name)
		}
	}
	if len(result.Operations) != 31 || result.Operations["docx_read"] == nil || result.Operations["xlsx_read"] == nil || result.Operations["pdf_read"] == nil || result.Operations["archive_create"] == nil || result.Operations["archive_extract"] == nil || result.Operations["download_file"] == nil || result.Operations["archive_list"] == nil || result.Operations["workflow_call"] == nil || result.Operations["workflow_bind"] != nil || result.Operations["run_program"] == nil || result.Operations["artifact_import"] == nil || result.Operations["image_variants"] == nil || result.Operations["run_shell"] != nil || !strings.Contains(string(result.Recipe), `"inputs"`) {
		t.Fatal(out)
	}
}
