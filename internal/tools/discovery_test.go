package tools

import (
	"encoding/json"
	"strings"
	"testing"

	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

func TestWorkflowDiscoveryMatchesNodeAndScheduleVocabulary(t *testing.T) {
	var description string
	for _, p := range builtinPackages(Dependencies{}) {
		if p.id == "native_workflows" {
			description = p.description
		}
	}
	catalog := &toolextension.Capabilities{Sources: []*toolextension.Capability{{ID: "native_workflows", Description: description}}}
	for _, query := range []string{"workflow schedule cron", "schedule node", "workflow nodes", "WORKFLOW   cron"} {
		args, _ := json.Marshal(map[string]string{"query": query})
		out, err := (toolextension.DiscoverCapabilities{Catalog: catalog}).Run(t.Context(), args)
		if err != nil || !strings.Contains(out, "native_workflows") {
			t.Fatal(query, out, err)
		}
	}
}
