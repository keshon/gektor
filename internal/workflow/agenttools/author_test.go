package agenttools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowSchemaSelectsOperations(t *testing.T) {
	registry, err := app.WorkflowRegistry(nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := Schema{Registry: func() (*workflow.Registry, error) { return registry, nil }}
	full, err := tool.Run(t.Context(), json.RawMessage(`{"all_operations":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var fullFields map[string]json.RawMessage
	err = json.Unmarshal([]byte(full), &fullFields)
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{{"run_program"}, {"run_program", "artifact_read_text"}, {"run_program", "run_program"}} {
		args, err := json.Marshal(struct {
			Operations []string `json:"operations"`
		}{Operations: names})
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := tool.Run(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		err = json.Unmarshal([]byte(filtered), &fields)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"operations", "operation_contracts"} {
			var selected, all map[string]json.RawMessage
			err = json.Unmarshal(fields[field], &selected)
			if err != nil {
				t.Fatal(err)
			}
			err = json.Unmarshal(fullFields[field], &all)
			if err != nil {
				t.Fatal(err)
			}
			want := make(map[string]json.RawMessage)
			for _, name := range names {
				want[name] = all[name]
			}
			if !reflect.DeepEqual(selected, want) {
				t.Fatalf("%s does not contain exactly the requested original contracts", field)
			}
		}
		for _, field := range []string{"recipe", "references", "argument_examples", "schedule_trigger"} {
			if string(fields[field]) != string(fullFields[field]) || len(fields[field]) == 0 {
				t.Fatalf("filter changed shared guidance: %s", field)
			}
		}
		if len(filtered) >= len(full) {
			t.Fatal("filter did not reduce the catalog")
		}
		t.Logf("operations=%v full=%d bytes filtered=%d bytes reduction=%.1f%%", names, len(full), len(filtered), 100*(1-float64(len(filtered))/float64(len(full))))
	}
	after, err := tool.Run(t.Context(), json.RawMessage(`{"all_operations":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if after != full {
		t.Fatal("filtered lookup mutated the full registry")
	}
}

func TestWorkflowSchemaRejectsInvalidOperationSelection(t *testing.T) {
	tool := Schema{Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(nil, "", nil, nil)
	}}
	tooMany := `{"operations":[` + strings.Repeat(`"run_program",`, 32) + `"run_program"]}`
	for _, args := range []string{
		`{"operations":["run_program"],"all_operations":true}`,
		`{"record_kind":"assets","all_operations":true}`,
		`{"operations":["not_registered"]}`,
		`{"operations":["run_program","not_registered"]}`,
		`{"operations":["workflow_bind"]}`,
		`{"operations":["run_program"],"record_kind":"assets"}`,
		`{"operations":[]}`,
		`{"operations":null}`,
		`{"operations":"run_program"}`,
		`{"operations":[""]}`,
		`{"operations":[1]}`,
		`{"operations":["` + strings.Repeat("a", 129) + `"]}`,
		tooMany,
	} {
		out, err := tool.Run(t.Context(), json.RawMessage(args))
		if err == nil || out != "" {
			t.Fatalf("invalid filter returned catalog: args=%s out=%s err=%v", args, out, err)
		}
	}
}

func TestWorkflowSchemaDefaultCatalogIsCompactAndComplete(t *testing.T) {
	registry, err := app.WorkflowRegistry(nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := Schema{Registry: func() (*workflow.Registry, error) { return registry, nil }}
	out, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Names     []string        `json:"operation_names"`
		Next      string          `json:"next"`
		Contracts json.RawMessage `json:"operation_contracts"`
	}
	err = json.Unmarshal([]byte(out), &catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(catalog.Names, registry.Names()) || len(catalog.Contracts) != 0 || !strings.Contains(catalog.Next, "operations") {
		t.Fatalf("default discovery omitted names or exposed full contracts: %s", out)
	}
	if len(out) > 2048 {
		t.Fatalf("default catalog needs paging: %d bytes", len(out))
	}
	t.Logf("default discovery: %d bytes for %d operations", len(out), len(catalog.Names))
}

func TestWorkflowSchemaUnknownOperationListsPublicNames(t *testing.T) {
	registry, err := app.WorkflowRegistry(nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := Schema{Registry: func() (*workflow.Registry, error) { return registry, nil }}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"operations":["run_program","read_file"]}`))
	if err == nil || out != "" {
		t.Fatalf("unknown operation returned partial success: out=%q err=%v", out, err)
	}
	_, names, found := strings.Cut(err.Error(), "; available operations: ")
	if !found || !reflect.DeepEqual(strings.Split(names, ", "), registry.Names()) {
		t.Fatalf("diagnostic omitted sorted public operation names: %v", err)
	}
	if !strings.Contains(names, "artifact_read_text") || strings.Contains(names, "workflow_bind") || strings.Contains(names, "input_schema") {
		t.Fatalf("diagnostic omitted useful name or exposed private schema: %v", err)
	}
	t.Logf("unknown-operation diagnostic: %d bytes for %d public names", len(err.Error()), len(registry.Names()))
}

func TestWorkflowContractsAndRunnableExample(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	out, err := (Schema{WS: ws, StoreDir: base, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, base, nil, nil)
	},
	}).Run(t.Context(), json.RawMessage(`{"all_operations":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Contracts map[string]workflow.Contract `json:"operation_contracts"`
		Examples  []json.RawMessage            `json:"argument_examples"`
	}
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatal(err)
	}
	script := catalog.Contracts["script_transform"]
	if !strings.Contains(script.Description, "JavaScript") || !script.ReadOnly || !script.RetrySafe || !script.Concurrent || string(script.OutputSchema) != "true" {
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
		out, err := (Tool{WS: ws, StoreDir: base, Authorize: allow, Registry: func() (*workflow.Registry, error) {
			return app.WorkflowRegistry(ws, base, nil, nil)
		},
		}).Run(t.Context(), tc.args)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatal(out, err)
		}
	}
}

const draftRecipe = `{"version":4,"name":"Draft","inputs":{"exact":{"type":"integer","default":9007199254740993}},"steps":[{"id":"inspect","tool":"image_inspect","args":{"paths":["image.png"]}}]}`

func TestSaveRecipeIsScopedAndPreservesNumbers(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	save := &Save{WS: ws, Path: "recipes/chosen.json", Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, "", nil, nil)
	},
	}
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
	if _, err := (&Save{WS: ws, Path: "../outside.json", Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, "", nil, nil)
	},
	}).Run(t.Context(), args); err == nil {
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
			if _, err := (&Save{WS: ws, Path: "recipe.json", Registry: func() (*workflow.Registry, error) {
				return app.WorkflowRegistry(ws, "", nil, nil)
			},
			}).Run(t.Context(), json.RawMessage(`{"recipe":`+draftRecipe+`}`)); err != nil {
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
	schema := Schema{Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(nil, "", nil, nil)
	}}
	out, err := schema.Run(t.Context(), json.RawMessage(`{"all_operations":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Recipe     json.RawMessage
		Operations map[string]json.RawMessage
	}
	err = json.Unmarshal([]byte(out), &result)
	if err != nil {
		t.Fatal(err)
	}

	required := []string{
		"media_status", "video_info", "video_playlist", "video_transcript", "video_download",
		"media_inspect", "media_convert", "media_clip", "media_frame", "media_merge",
		"ssh_exec", "ssh_upload", "ssh_download", "docx_read", "xlsx_read", "pdf_read",
		"archive_create", "archive_extract", "download_file", "archive_list",
		"workflow_call", "workflow_join", "workflow_wait", "workflow_wait_callback",
		"run_program", "artifact_import", "image_variants", "image_optimize",
		"social_read_post", "social_read_thread", "social_read_timeline", "social_read_trends",
	}
	for _, name := range required {
		if result.Operations[name] == nil {
			t.Error("missing operation", name)
		}
	}
	for _, name := range []string{"workflow_bind", "run_shell"} {
		if result.Operations[name] != nil {
			t.Error("unexpected operation", name)
		}
	}
	if len(result.Operations) != 42 {
		t.Error("unexpected operation count", len(result.Operations))
	}
	if !strings.Contains(string(result.Recipe), `"inputs"`) {
		t.Error("recipe schema omitted inputs")
	}
}
