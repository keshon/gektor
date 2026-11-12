package app_test

import (
	"context"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/extension"
)

func TestToolsetInspectionPreservesCatalogAndNeverLoadsSources(t *testing.T) {
	calls := 0
	source := &extension.Capability{ID: "remote", Source: "https://example.com/mcp", Load: func(context.Context, func(string)) ([]agent.Tool, error) { calls++; return nil, nil }}
	sets, err := app.Toolsets(tools.Packages(), []*extension.Capability{source}, map[string]extension.Exposure{tools.NativeWeb: extension.Lazy}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("inspection connected an extension")
	}
	foundMedia, foundRemote := false, false
	for _, set := range sets {
		if set.ID == "native_media" {
			foundMedia = true
			if len(set.Agent) != 9 || len(set.Workflow) != 9 || set.Exposure != "On demand" {
				t.Fatal(set)
			}
		}
		if set.ID == "source:remote" {
			foundRemote = true
			if len(set.Agent) != 0 || set.Group != "Extension" {
				t.Fatal(set)
			}
		}
		if set.ID == tools.NativeWeb && set.Exposure != "On demand" {
			t.Fatal("exposure override lost")
		}
	}
	if !foundMedia || !foundRemote {
		t.Fatal("missing toolsets")
	}
}
