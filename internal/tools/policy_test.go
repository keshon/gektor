package tools

import (
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/userdirs"
)

// Each permission scenario gets fresh tools constructed under that policy.
func policyCatalog(t *testing.T, dependencies Dependencies) *toolextension.Capabilities {
	t.Helper()
	if dependencies.StoreDir == "" {
		dependencies.StoreDir = userdirs.Sub("workflows")
	}
	if len(dependencies.Policy.Rules) == 0 {
		dependencies.Policy = permission.Default()
	}
	workflow, err := app.NewWorkflow(dependencies.WS, dependencies.StoreDir, dependencies.Policy, dependencies.Gate)
	if err != nil {
		t.Fatal(err)
	}
	dependencies.Workflow = workflow
	c := &toolextension.Capabilities{}
	r, err := Compose(dependencies, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	for _, p := range c.Native {
		if _, err := c.Enable(t.Context(), p.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	return c
}
