package roles

import (
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

func TestRecordsDenySameResourceInAgentAndWorkflow(t *testing.T) {
	e := testEnv(t)
	e.WorkflowDir = t.TempDir()
	e.Policy = permission.Default().WithExtra([]permission.Rule{
		{Tool: "record_write", Pattern: "notes/state", Effect: permission.Deny},
	})
	s, err := store.OpenStore(e.WorkflowDir, e.WS.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
	args := `{"kind":"notes","id":"state","expected_revision":0,"data":{}}`
	e.Client = &seqStub{steps: []llm.ChatResponse{
		extensionCall("enable", "enable_capability", `{"id":"native_workflows"}`),
		extensionCall("write", "invoke_tool", `{"name":"record_write","arguments":`+args+`}`),
		extensionAnswer(),
	}}
	blocked := false
	e.OnResult = func(result agent.ToolResult) {
		if result.CallID == "write" {
			blocked = result.Failed && strings.Contains(result.Output, "blocked by policy")
		}
	}
	if _, err := Interactive(e, "", "", nil, nil).Run(t.Context(), "write the test record"); err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("Agent did not deny record_write on notes/state")
	}
	svc, err := app.NewAutomation(e.WS, e.WorkflowDir, e.Policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	recipe := []byte(`{"version":3,"name":"Denied record","steps":[{"id":"write","tool":"record_write","args":` + args + `}]}`)
	if _, err := svc.RunSource(t.Context(), recipe, "denied.json", nil); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("workflow did not deny record_write on notes/state", err)
	}
	if _, err := s.Entry("notes", "state"); !errors.Is(err, store.ErrEntryNotFound) {
		t.Fatal("denied calls changed record state", err)
	}
}
