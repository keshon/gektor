package extension

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	toolweb "github.com/keshon/gektor/internal/tools/web"
)

func TestAdapterCancellationAndCollisionDoNotCloseSharedSource(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	closed, loads := 0, 0
	source := &Capability{ID: "shared", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		cancel()
		return []agent.Tool{toolweb.Check{}}, nil
	}, Close: func() { closed++ }}
	c := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
	if _, err := c.Enable(ctx, "shared", nil); err == nil || len(c.Save()) != 0 {
		t.Fatal("published after loader cancellation")
	}
	if _, err := c.Enable(t.Context(), "shared", nil); err != nil {
		t.Fatal(err)
	}
	other := &Capabilities{Registry: agent.NewRegistry(toolweb.Check{}), Sources: []*Capability{source}}
	if _, err := other.Enable(t.Context(), "shared", nil); err == nil {
		t.Fatal("collision accepted")
	}
	if closed != 0 || loads != 1 || len(other.Save()) != 0 {
		t.Fatal("shared source lifetime changed", closed, loads)
	}
}

// A wrapper that preserves only Tool would lose these interfaces. Exercise the
// registry's actual consumers, not just the package's constructor result.
func TestNativeActivationPreservesOptionalInterfaces(t *testing.T) {
	probe := &nativeInterfaceProbe{}
	c := &Capabilities{Registry: agent.NewRegistry(), Native: []*NativePackage{{ID: "native_probe", Version: "1", Exposure: Lazy, Tools: []agent.Tool{probe}}}}
	if _, err := c.Enable(t.Context(), "native_probe", nil); err != nil {
		t.Fatal(err)
	}
	if c.Registry.Checklist() != probe || c.Registry.Questioner() != probe || c.Registry.AttacherOf(probe.Name()) != probe {
		t.Fatal("native interfaces or identity lost")
	}
	if c.Registry.ModeOf(probe.Name()) != agent.Concurrent {
		t.Fatal("native mode replaced")
	}
	var streamed string
	out, err := c.Registry.RunStreaming(t.Context(), probe.Name(), json.RawMessage(`{}`), func(s string) { streamed += s })
	if err != nil || out != "result" || streamed != "delta" {
		t.Fatal("streaming lost", out, streamed, err)
	}
}

func TestMainOnlyCapabilityRestoreIsRoleChecked(t *testing.T) {
	loads := 0
	source := &Capability{ID: "main_pack", MainOnly: true, Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return []agent.Tool{toolweb.Check{}}, nil
	}}
	parent := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
	if _, err := parent.Enable(t.Context(), source.ID, nil); err != nil {
		t.Fatal(err)
	}
	child := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}, Subagent: true}
	if err := child.Restore(t.Context(), parent.Save()); err == nil {
		t.Fatal("child restored main-only pack")
	}
	if _, err := child.Enable(t.Context(), source.ID, nil); err == nil {
		t.Fatal("child enabled main-only pack")
	}
	if len(child.entries()) != 0 || len(child.Save()) != 0 || loads != 1 {
		t.Fatal("role-filtered source was exposed or loaded")
	}
}

type nativeInterfaceProbe struct{}

func (*nativeInterfaceProbe) Name() string            { return "native_probe_tool" }
func (*nativeInterfaceProbe) Description() string     { return "fixture" }
func (*nativeInterfaceProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*nativeInterfaceProbe) Mode() agent.ToolMode    { return agent.Concurrent }
func (*nativeInterfaceProbe) Run(context.Context, json.RawMessage) (string, error) {
	return "result", nil
}
func (*nativeInterfaceProbe) RunStreaming(_ context.Context, _ json.RawMessage, output func(string)) (string, error) {
	output("delta")
	return "result", nil
}
func (*nativeInterfaceProbe) Attach(json.RawMessage) ([]string, error) { return nil, nil }
func (*nativeInterfaceProbe) Progress() (int, []string)                { return 1, nil }
func (*nativeInterfaceProbe) Question(json.RawMessage) string          { return "question" }
