package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
	"strings"
	"testing"
)

func TestCustomNodeNeedsNoCentralDispatchChanges(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	calls := 0
	registry, err := automation.NewRegistry(automation.Node{Name: "custom.echo", Version: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object","required":["value"]}`), Resources: func(args json.RawMessage) ([]string, error) {
		var in struct{ Value string }
		err := json.Unmarshal(args, &in)
		return []string{in.Value}, err
	}, Execute: func(_ context.Context, args json.RawMessage) (string, error) { calls++; return string(args), nil }})
	if err != nil {
		t.Fatal(err)
	}
	source := `{"version":3,"name":"Extensible","steps":[{"id":"first","tool":"custom.echo","args":{"value":"allowed"}},{"id":"second","tool":"custom.echo","args":{"value":{"$step":"first","pointer":"/value"}}}]}`
	if err := ws.WriteFile("recipe.json", []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	checked := 0
	svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: registry, Authorize: func(context.Context, string, json.RawMessage) error { return nil }, AuthorizeResource: func(_ context.Context, name, resource string, args json.RawMessage) error {
		checked++
		if name != "custom.echo" || resource != "allowed" {
			return fmt.Errorf("unexpected permission %s/%s", name, resource)
		}
		return nil
	}}
	if !strings.Contains(string(svc.DefinitionSchema()), "custom.echo") {
		t.Fatal("schema omitted registered node")
	}
	out, err := svc.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || calls != 2 || checked != 2 || !strings.Contains(out, `"status":"completed"`) {
		t.Fatal(out, err, calls, checked)
	}
	calls = 0
	svc.AuthorizeResource = func(context.Context, string, string, json.RawMessage) error { return fmt.Errorf("denied") }
	if _, err := svc.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`)); err == nil || calls != 0 {
		t.Fatal("permission bypass")
	}
}

func TestIterationPermissionsRetryAndVersions(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	calls := 0
	permissions := []string{}
	node := automation.Node{Name: "echo", Version: 1, RetrySafe: true, InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`), Resources: func(raw json.RawMessage) ([]string, error) { return []string{string(raw)}, nil }, Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("temporary")
		}
		return string(raw), nil
	}}
	registry, err := automation.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: registry, AuthorizeResource: func(_ context.Context, _, resource string, _ json.RawMessage) error {
		permissions = append(permissions, resource)
		return nil
	}}
	source := []byte(`{"version":3,"name":"loop","steps":[{"id":"loop","tool":"echo","args":{},"for_each":[{"value":1},{"value":2}],"retry":{"max_attempts":2}}]}`)
	out, err := svc.RunSource(t.Context(), source, "loop.json", nil)
	if err != nil || calls != 3 || len(permissions) != 3 {
		t.Fatal(out, err, calls, permissions)
	}
	node.RetrySafe = false
	svc.Registry, err = automation.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunSource(t.Context(), source, "loop.json", nil); err == nil || calls != 3 {
		t.Fatal("unsafe retry accepted", calls)
	}
	node.RetrySafe = true
	node.Version = 2
	svc.Registry, err = automation.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunSource(t.Context(), source, "loop.json", nil); err == nil || calls != 3 {
		t.Fatal("changed node version accepted", calls)
	}
}

func TestReviewContinuesSnapshotWithoutRepeatingEffects(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	calls := 0
	node := automation.Node{Name: "echo", Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: func(context.Context, json.RawMessage) (string, error) { calls++; return `{"value":42}`, nil }}
	registry, err := automation.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
	raw := []byte(`{"version":3,"name":"review","steps":[{"id":"first","tool":"echo","args":{}},{"id":"publish","tool":"echo","depends_on":["first"],"review":true,"args":{"value":{"$step":"first","pointer":"/value"}}}]}`)
	out, err := svc.RunSource(t.Context(), raw, "recipe.json", nil)
	if err != nil || calls != 1 {
		t.Fatal(out, err, calls)
	}
	var summary struct {
		RunID  string `json:"run_id"`
		Status string
	}
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status != "waiting_review" {
		t.Fatal(out)
	}
	out, err = svc.Review(t.Context(), summary.RunID, "wrong", true)
	if err == nil || calls != 1 {
		t.Fatal("wrong review accepted")
	}
	out, err = svc.Review(t.Context(), summary.RunID, "publish", true)
	if err != nil || calls != 2 || !strings.Contains(out, `"completed"`) {
		t.Fatal(out, err, calls)
	}
	if _, err = svc.Review(t.Context(), summary.RunID, "publish", true); err == nil || calls != 2 {
		t.Fatal("approval repeated effects")
	}
}
