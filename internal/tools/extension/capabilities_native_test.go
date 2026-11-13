package extension

import (
	"context"
	"encoding/json"
	"errors"
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

func TestOnLoadAdaptersFailWithoutBlockingUnrelatedTools(t *testing.T) {
	loads := 0
	source := &Capability{ID: "offline", Eager: true, Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return nil, errors.New("fixture unavailable")
	}}
	catalog := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source},
		Native: []*NativePackage{{ID: "native_probe", Version: "1", Exposure: Eager, Tools: []agent.Tool{&nativeInterfaceProbe{}}}},
	}
	if err := catalog.Prepare(t.Context()); err != nil {
		t.Fatal("optional failure blocked preparation", err)
	}
	if !source.Eager || !catalog.Registry.Has("native_probe_tool") || len(catalog.Save()) != 1 {
		t.Fatal("loading policy or unrelated tools changed")
	}
	state, _, detail := source.ConnectionStatus()
	if state != "failed" || detail == "" {
		t.Fatal("failure diagnostic missing")
	}
	source.Load = func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return []agent.Tool{toolweb.Check{}}, nil
	}
	if _, err := catalog.Enable(t.Context(), "offline", nil); err != nil || loads != 2 {
		t.Fatal("explicit retry failed", err)
	}
}

func TestOnLoadAndDemandPublishAtConversationPreparation(t *testing.T) {
	for _, eager := range []bool{false, true} {
		source := &Capability{ID: "fixture", Eager: eager, Load: func(context.Context, func(string)) ([]agent.Tool, error) {
			return []agent.Tool{toolweb.Check{}}, nil
		}}
		catalog := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
		if err := catalog.Prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		if catalog.Registry.Has("check_url") != eager {
			t.Fatal("wrong initial exposure", eager)
		}
		source.Disconnect()
	}
}

func TestUnloadPersistsAcrossResumeWithoutDisconnecting(t *testing.T) {
	loads, closes := 0, 0
	source := &Capability{ID: "remote", Eager: true, Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return []agent.Tool{&nativeInterfaceProbe{}}, nil
	}, Close: func() { closes++ }}
	first := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
	if err := first.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := first.Unload("remote"); err != nil {
		t.Fatal(err)
	}
	if first.Registry.Has("native_probe_tool") || closes != 0 {
		t.Fatal("unload left a schema or closed transport")
	}
	resumed := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
	if err := resumed.Restore(t.Context(), first.Save()); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resumed.Registry.Has("native_probe_tool") || loads != 1 {
		t.Fatal("on-load policy undid session selection")
	}
	if _, err := resumed.Enable(t.Context(), "remote", nil); err != nil {
		t.Fatal(err)
	}
	if !resumed.Registry.Has("native_probe_tool") || resumed.Save()[0].Unloaded || loads != 1 {
		t.Fatal("reload lost shared adapter or state")
	}
	if err := resumed.Unload("core"); err == nil {
		t.Fatal("always tools were unloadable")
	}
	source.Disconnect()
}

func TestUnloadedNativeDefaultIsNotPreparedOrPreviewed(t *testing.T) {
	catalog := &Capabilities{Registry: agent.NewRegistry(), Native: []*NativePackage{{ID: "native_probe", Version: "1", Exposure: Eager, Tools: []agent.Tool{&nativeInterfaceProbe{}}}}}
	if err := catalog.Unload("native_probe"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(catalog.PreviewDefs()) != 0 {
		t.Fatal("unloaded native schemas were advertised")
	}
	resumed := &Capabilities{Registry: agent.NewRegistry(), Native: catalog.Native}
	if err := resumed.Restore(t.Context(), catalog.Save()); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resumed.Registry.Has("native_probe_tool") {
		t.Fatal("native default reloaded")
	}
	if _, err := resumed.Enable(t.Context(), "native_probe", nil); err != nil {
		t.Fatal(err)
	}
	if !resumed.Registry.Has("native_probe_tool") {
		t.Fatal("explicit native reload failed")
	}
}
