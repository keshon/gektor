package tools

import (
	"fmt"
	"os"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/documents"
	"github.com/keshon/gektor/internal/permission"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "workflow" && os.Args[2] == "internal-document-worker" {
		if err := documents.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// Spilled tool output lands in the user's GEKTOR directory. A test that
	// exercises spilling must not write into the developer's real one.
	home, err := os.MkdirTemp("", "gektor-home")
	if err != nil {
		panic(err)
	}
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	os.Setenv("USERPROFILE", home)
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func testWS(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	return ws, dir
}

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
