package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

func generatedFixture(kind string) (map[string][]byte, error) {
	switch kind {
	case "pipeline_compose", "pipeline_schedule":
		return map[string][]byte{"double.json": []byte(doubleRecipe)}, nil
	case "visual_attachment":
		im := image.NewNRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				c := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				if (x-180)*(x-180)+(y-170)*(y-170) < 90*90 {
					c = color.NRGBA{R: 220, A: 255}
				}
				if x >= 650 && x < 710 && y >= 450 && y < 510 {
					c = color.NRGBA{B: 220, A: 255}
				}
				im.SetNRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			return nil, err
		}
		// Deliberately wrong suffix, as in the Ornith reference-image session.
		return map[string][]byte{"picture.jpg": b.Bytes()}, nil
	default:
		return workflowFixture(kind)
	}
}

const doubleRecipe = `{"version":3,"name":"Double a number","inputs":{"n":{"type":"integer","required":true}},"steps":[{"id":"calc","tool":"script_transform","args":{"input":{"$input":"n"},"code":"return input * 2;"}}],"outputs":{"value":{"$step":"calc"}}}`

func checkPipeline(ctx context.Context, kind string, ws *workspace.Workspace) error {
	data, err := ws.ReadFile("double.json")
	if err != nil || !bytes.Equal(data, []byte(doubleRecipe)) {
		return fmt.Errorf("reusable child recipe changed or missing")
	}
	db, err := store.OpenStore(workflowStoreDir(ws), ws.Root())
	if err != nil {
		return err
	}
	defer db.Close()
	runs, err := db.Runs()
	if err != nil {
		return err
	}
	if kind == "pipeline_schedule" {
		all, err := db.Schedules(ctx)
		if err != nil {
			return err
		}
		if len(all) != 1 {
			return fmt.Errorf("expected one schedule, got %d", len(all))
		}
		s := all[0]
		if s.ID != "double-hourly" || s.RecipePath != "double.json" || s.Cron != "0 * * * *" || s.Timezone != "UTC" || !s.Paused || string(s.Inputs["n"]) != "7" || !bytes.Equal(s.Source, data) {
			return fmt.Errorf("schedule does not match requested paused trigger and frozen inputs")
		}
		jobs, err := db.Jobs(ctx)
		if err != nil {
			return err
		}
		if len(runs) != 0 || len(jobs) != 0 {
			return fmt.Errorf("schedule task executed or queued work")
		}
		return nil
	}
	if kind != "pipeline_compose" {
		return fmt.Errorf("unknown scenario %q", kind)
	}
	if len(runs) != 1 {
		return fmt.Errorf("expected one parent run, got %d", len(runs))
	}
	run, err := db.Run(runs[0].ID)
	if err != nil {
		return err
	}
	if run.Record.Status != "completed" || run.Record.RecipePath != "recipe.json" {
		return fmt.Errorf("parent recipe did not complete")
	}
	childCalls := 0
	var bundle struct {
		Root    json.RawMessage            `json:"root"`
		Recipes map[string]json.RawMessage `json:"recipes"`
	}
	if err := json.Unmarshal([]byte(run.Record.RecipeSource), &bundle); err != nil {
		return err
	}
	parent, err := ws.ReadFile("recipe.json")
	if err != nil {
		return err
	}
	compact := func(b []byte) string {
		var out bytes.Buffer
		if json.Compact(&out, b) != nil {
			return ""
		}
		return out.String()
	}
	if compact(bundle.Root) != compact(parent) || compact(bundle.Recipes["double.json"]) != compact(data) {
		return fmt.Errorf("run snapshot differs from saved parent or child")
	}
	var source struct {
		Steps []struct {
			ID   string `json:"id"`
			Tool string `json:"tool"`
			Args struct {
				Path   string         `json:"recipe_path"`
				Inputs map[string]int `json:"inputs"`
			} `json:"args"`
		} `json:"steps"`
		Outputs map[string]struct {
			Step    string `json:"$step"`
			Pointer string `json:"pointer"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(bundle.Root, &source); err != nil {
		return err
	}
	for _, step := range source.Steps {
		if step.Tool == "workflow_call" && step.Args.Path == "double.json" {
			childCalls++
			key := map[int]string{3: "six", 5: "ten"}[step.Args.Inputs["n"]]
			ref, ok := source.Outputs[key]
			if !ok || ref.Step != step.ID || ref.Pointer != "/value" {
				return fmt.Errorf("parent output must reference each requested child result")
			}
		}
	}
	if childCalls != 2 {
		return fmt.Errorf("parent must call reusable child twice")
	}
	for _, step := range run.Record.Steps {
		if step.ID != "@outputs" || step.Status != "completed" {
			continue
		}
		var got map[string]int
		if err := json.Unmarshal([]byte(step.Output), &got); err != nil {
			return err
		}
		if len(got) == 2 && got["six"] == 6 && got["ten"] == 10 {
			return nil
		}
	}
	return fmt.Errorf("declared parent outputs must be six=6 and ten=10")
}
