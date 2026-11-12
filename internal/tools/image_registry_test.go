package tools

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	toolimages "github.com/keshon/gektor/internal/tools/images"
)

func TestImageToolsStayLazy(t *testing.T) {
	ws, _ := testWS(t)
	catalog := &toolextension.Capabilities{}
	reg, err := Compose(Dependencies{WS: ws, Vision: true}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Registry = reg
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if reg.Has(name) {
			t.Fatal("image schema added to startup", name)
		}
	}
	if _, err := catalog.Enable(t.Context(), "native_images", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if !reg.Has(name) {
			t.Fatal("missing lazy image tool", name)
		}
	}
	if err := agent.NewRegistry(&toolimages.View{WS: ws, Vision: true}, &toolimages.Download{Vision: true}, toolimages.Attachments{}).Err(); err != nil {
		t.Fatal(err)
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
