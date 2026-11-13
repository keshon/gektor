package app_test

import (
	"context"
	"slices"
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
			if len(set.Agent) != 10 || len(set.Workflow) != 10 || set.Loading != "On demand" {
				t.Fatal(set)
			}
		}
		if set.ID == "source:remote" {
			foundRemote = true
			if len(set.Agent) != 0 || set.Group != "External" {
				t.Fatal(set)
			}
		}
		if set.ID == tools.NativeWeb && set.Loading != "On demand" {
			t.Fatal("exposure override lost")
		}
	}
	if !foundMedia || !foundRemote {
		t.Fatal("missing toolsets")
	}
}

func TestToolsetOrderGroupsTasksAndAlphabetizesOtherIntegrations(t *testing.T) {
	sets, err := app.Toolsets(tools.Packages(), []*extension.Capability{{ID: "zebra"}, {ID: "browser"}, {ID: "alpha"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(sets))
	for i, set := range sets {
		ids[i] = set.ID
	}
	want := []string{"core", "workspace", "native_files", "native_documents", "native_images", "native_web", "source:browser", "native_media", "git", "execution", "processes", "native_ssh", "native_workflows", "source:alpha", "source:zebra"}
	if !slices.Equal(ids, want) {
		t.Fatal("unstable task order", ids)
	}
}
