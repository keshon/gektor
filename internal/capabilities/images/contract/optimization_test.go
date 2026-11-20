package contract

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/workspace"
)

func TestOptimizationContractRejectsResizeAndUnsafeOptions(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	op := Operation{Service: images.Service{WS: ws}, Kind: "optimize"}
	for _, valid := range []string{
		`{"paths":["image.png"],"out_dir":"optimized"}`,
		`{"paths":["image.png"],"format":"webp","out_dir":"optimized"}`,
		`{"paths":["image.png"],"format":"webp","color_profile":"srgb","out_dir":"optimized"}`,
		`{"paths":["image.png"],"color_profile":"preserve","out_dir":"optimized"}`,
	} {
		if err := op.Validate(json.RawMessage(valid)); err != nil {
			t.Fatalf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		`{"paths":["image.png"]}`,
		`{"paths":["image.png"],"widths":[128],"out_dir":"optimized"}`,
		`{"paths":["image.png"],"format":"png","out_dir":"optimized"}`,
		`{"paths":["image.png"],"overwrite":true,"out_dir":"optimized"}`,
		`{"paths":["image.png"],"out_dir":"../outside"}`,
		`{"paths":["image.png"],"color_profile":"strip","out_dir":"optimized"}`,
	} {
		if err := op.Validate(json.RawMessage(invalid)); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}
