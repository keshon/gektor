package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func workflowFixture(kind string) (map[string][]byte, error) {
	switch kind {
	case "workflow_success", "workflow_icc", "workflow_invalid", "workflow_existing":
	default:
		return nil, fmt.Errorf("unknown generated fixture %q", kind)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 1024, 768))
	for y := 0; y < 768; y++ {
		for x := 0; x < 1024; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 4), G: uint8(y / 3), B: uint8((x + y) / 7), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		return nil, err
	}
	data := encoded.Bytes()
	if kind == "workflow_icc" {
		// A generated ICC marker exercises the metadata gate, not colour
		// conversion accuracy. No photographic asset or external profile is used.
		var profile bytes.Buffer
		z := zlib.NewWriter(&profile)
		if _, err := z.Write([]byte("generated evaluation profile marker")); err != nil {
			return nil, err
		}
		if err := z.Close(); err != nil {
			return nil, err
		}
		payload := append([]byte("eval\x00\x00"), profile.Bytes()...)
		chunk := make([]byte, len(payload)+12)
		binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
		copy(chunk[4:], "iCCP")
		copy(chunk[8:], payload)
		binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
		data = append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
	}
	files := map[string][]byte{"image.png": data}
	if kind == "workflow_invalid" || kind == "workflow_existing" {
		recipe := `{"version":4,"name":"Image variants","steps":[{"id":"write","tool":"image_variants","args":{"paths":["image.png"],"widths":[320,800],"out_dir":"output"}}]}`
		if kind == "workflow_invalid" {
			recipe = strings.Replace(recipe, `"version":4`, `"version":4,"input":"image.png"`, 1)
		}
		files["recipe.json"] = []byte(recipe + "\n")
	}
	if kind == "workflow_existing" {
		files["output/keep.txt"] = []byte("existing file must survive\n")
	}
	return files, nil
}

func checkWorkflow(kind string, ws *workspace.Workspace) error {
	files, err := workflowFixture(kind)
	if err != nil {
		return err
	}
	for path, want := range files {
		got, err := ws.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("fixture changed: %s", path)
		}
	}
	store, err := store.OpenStore(workflowStoreDir(ws), ws.Root())
	if err != nil {
		return err
	}
	defer store.Close()
	entries, err := store.Runs()
	if err != nil {
		return err
	}
	if kind == "workflow_invalid" {
		if len(entries) != 0 {
			return fmt.Errorf("invalid recipe executed")
		}
		return nil
	}
	var records []workflow.Run
	for _, entry := range entries {
		r, err := store.Run(entry.ID)
		if err != nil {
			return err
		}
		records = append(records, r.Record)
	}
	if len(records) != 1 {
		return fmt.Errorf("expected one execution record, got %d", len(records))
	}
	r := records[0]
	source, err := ws.ReadFile(r.RecipePath)
	if err != nil {
		return err
	}
	if r.RecipeSource != string(source) || r.RecipeSHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) {
		return fmt.Errorf("record differs from saved recipe")
	}
	compiler, err := app.NewWorkflow(ws, workflowStoreDir(ws), permission.Policy{}, nil)
	if err != nil {
		return err
	}
	expected, _, err := compiler.Compile(context.Background(), source, r.RecipePath)
	if err != nil {
		return fmt.Errorf("invalid saved recipe: %w", err)
	}
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(r.Definition)
	canonical := func(raw []byte) []byte {
		var value any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if d.Decode(&value) != nil {
			return nil
		}
		out, _ := json.Marshal(value)
		return out
	}
	if !bytes.Equal(canonical(a), canonical(b)) {
		return fmt.Errorf("executed definition differs from file")
	}
	if len(r.Steps) != len(r.Definition.Steps) {
		return fmt.Errorf("execution record has an incomplete step list")
	}
	var publicSteps []workflow.StepResult
	var publicDefinition []workflow.Step
	for i, step := range r.Definition.Steps {
		if r.Steps[i].ID != step.ID {
			return fmt.Errorf("execution step ID differs from its definition")
		}
		node, err := compiler.Registry.Node(step.Tool)
		if err != nil {
			return err
		}
		if !node.Internal {
			publicSteps = append(publicSteps, r.Steps[i])
			publicDefinition = append(publicDefinition, step)
		}
	}
	if kind == "workflow_success" {
		if r.Status != "completed" || len(publicSteps) != 4 {
			return fmt.Errorf("expected four completed steps, got %s (%d)", r.Status, len(publicSteps))
		}
		for _, s := range r.Steps {
			if s.Status != "completed" {
				return fmt.Errorf("step %s did not complete", s.ID)
			}
		}
		for i, want := range []string{"image_inspect", "image_preview", "image_variants", "image_compare"} {
			if publicDefinition[i].Tool != want {
				return fmt.Errorf("wrong operation in step %d", i)
			}
		}
		for _, w := range []int{320, 800} {
			data, err := ws.ReadFile(fmt.Sprintf("output/image-%dw.png", w))
			if err != nil {
				return err
			}
			im, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				return err
			}
			if im.Bounds().Dx() != w || im.Bounds().Dy() != w*3/4 {
				return fmt.Errorf("wrong output dimensions for %d", w)
			}
		}
	} else if kind == "workflow_icc" {
		if r.Status != "failed" || len(publicSteps) != 3 || publicSteps[0].Status != "completed" || publicSteps[1].Status != "failed" || !strings.Contains(publicSteps[1].Error, "unsupported_metadata") || publicSteps[2].Status != "skipped" {
			return fmt.Errorf("ICC recipe did not stop at preview")
		}
		if _, err := ws.Stat("output"); !os.IsNotExist(err) {
			return fmt.Errorf("ICC recipe created output directory")
		}
		for i, want := range []string{"image_inspect", "image_preview", "image_variants"} {
			if publicDefinition[i].Tool != want {
				return fmt.Errorf("wrong ICC operation in step %d", i)
			}
		}
	} else {
		if r.Status != "failed" || len(publicSteps) != 1 || !strings.Contains(publicSteps[0].Error, "reserve new output directory") {
			return fmt.Errorf("existing destination was not rejected")
		}
		entries, err := ws.ReadDir("output")
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].Name() != "keep.txt" {
			return fmt.Errorf("existing directory changed")
		}
	}
	// Match the returned hashes to independently generated source bytes.
	for _, s := range publicSteps {
		if s.Output == "" {
			continue
		}
		var out struct {
			Sources []struct {
				Path      string `json:"path"`
				Before    string `json:"sha256_before"`
				After     string `json:"sha256_after"`
				Unchanged *bool  `json:"unchanged"`
			} `json:"sources"`
		}
		if err := json.Unmarshal([]byte(s.Output), &out); err != nil {
			return err
		}
		if len(out.Sources) == 0 {
			return fmt.Errorf("step %s lacks source evidence", s.ID)
		}
		for _, e := range out.Sources {
			data, err := ws.ReadFile(e.Path)
			if err != nil {
				return err
			}
			want := fmt.Sprintf("%x", sha256.Sum256(data))
			if e.Unchanged == nil || !*e.Unchanged || e.Before != want || e.After != want {
				return fmt.Errorf("step %s has incorrect source evidence", s.ID)
			}
		}
	}
	return nil
}

// Match store records to captured results and the recipe selected by the call.
func checkWorkflowTrace(calls []call, ws *workspace.Workspace) error {
	for _, c := range calls {
		if c.tool != "workflow_run" {
			continue
		}
		var result struct {
			ID     string `json:"run_id"`
			Digest string `json:"recipe_sha256"`
			Status string `json:"status"`
		}
		body := strings.SplitN(c.output, "\nerror:", 2)[0]
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			return fmt.Errorf("workflow result is not a recorded execution: %w", err)
		}
		store, err := store.OpenStore(workflowStoreDir(ws), ws.Root())
		if err != nil {
			return err
		}
		state, err := store.Run(result.ID)
		store.Close()
		if err != nil {
			return err
		}
		r := state.Record
		if r.RecipeSHA256 != result.Digest || r.Status != result.Status {
			return fmt.Errorf("workspace record differs from captured tool result")
		}
		var args struct {
			Path string `json:"recipe_path"`
		}
		if err := json.Unmarshal([]byte(c.args), &args); err != nil {
			return err
		}
		path, err := ws.Relative(args.Path)
		if err != nil {
			return err
		}
		recordPath, err := ws.Relative(r.RecipePath)
		if err != nil {
			return err
		}
		if path != recordPath {
			return fmt.Errorf("record names a different recipe from the tool call")
		}
	}
	return nil
}

func workflowStoreDir(ws *workspace.Workspace) string {
	return filepath.Join(filepath.Dir(ws.Root()), "workflow-store")
}
