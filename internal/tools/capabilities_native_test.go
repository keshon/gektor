package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	toolaskuser "github.com/keshon/gektor/internal/tools/askuser"
	toolweb "github.com/keshon/gektor/internal/tools/web"
)

func TestNativeEagerLazyAndResume(t *testing.T) {
	eager, lazy := composed(t, Eager), composed(t, Lazy)
	preview := eager.PreviewDefs()
	if eager.Registry.Has("webfetch") || lazy.Registry.Has("webfetch") {
		t.Fatal("construction activated tools")
	}
	if err := eager.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lazy.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lazy.Registry.Has("webfetch") {
		t.Fatal("lazy preparation exposed tools")
	}
	before := lazy.Map()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := lazy.Enable(t.Context(), NativeWeb, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(lazy.Save()) != 1 || lazy.Map() != before {
		t.Fatal("activation not idempotent/stable")
	}
	if !reflect.DeepEqual(eager.Registry.Defs(), lazy.Registry.Defs()) || !reflect.DeepEqual(preview, eager.Registry.Defs()) {
		t.Fatal("exposure changed definitions")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fixture")) }))
	defer server.Close()
	args, _ := json.Marshal(map[string]string{"url": server.URL})
	for _, c := range []*Capabilities{eager, lazy} {
		out, err := c.Registry.Run(t.Context(), "check_url", args)
		if err != nil || !strings.Contains(out, "fixture") {
			t.Fatal(out, err)
		}
	}
	raw, _ := json.Marshal(lazy.Save())
	var saved []conversation.Capability
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	resumed := composed(t, Eager)
	if err := resumed.Restore(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Restore(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	if len(resumed.Save()) != 1 {
		t.Fatal("duplicate restored activation")
	}
	saved[0].Fingerprint = "changed"
	if err := resumed.Restore(t.Context(), saved); err == nil {
		t.Fatal("conflicting already-active state accepted")
	}
	fresh := composed(t, Eager)
	if err := fresh.Restore(t.Context(), saved); err == nil || fresh.Registry.Has("webfetch") {
		t.Fatal("changed native package published")
	}
	missing := &Capabilities{Registry: agent.NewRegistry()}
	if err := missing.Restore(t.Context(), lazy.Save()); err == nil {
		t.Fatal("missing native package restored")
	}
}

func TestNativeActivationFailureIsAtomic(t *testing.T) {
	c := composed(t, Lazy)
	if err := c.Registry.Add(toolweb.Check{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Enable(t.Context(), NativeWeb, nil); err == nil {
		t.Fatal("collision accepted")
	}
	if c.Registry.Has("webfetch") || len(c.Save()) != 0 {
		t.Fatal("partial publication")
	}
	c = composed(t, Lazy)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Enable(ctx, NativeWeb, nil); err == nil {
		t.Fatal("cancelled activation accepted")
	}
	if len(c.Save()) != 0 || c.Registry.Has("webfetch") {
		t.Fatal("cancelled activation published")
	}
	if _, err := c.Enable(t.Context(), NativeWeb, nil); err != nil {
		t.Fatal("retry failed", err)
	}
}

func TestCapabilityRoleExclusionAndDiscoveryBudget(t *testing.T) {
	c := &Capabilities{Registry: agent.NewRegistry(), ForbiddenTools: []string{"ask_user"}, Sources: []*Capability{{ID: "questions", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		return []agent.Tool{toolaskuser.Tool{}}, nil
	}}}}
	if _, err := c.Enable(t.Context(), "questions", nil); err == nil || c.Registry.Has("ask_user") {
		t.Fatal("role exclusion bypassed")
	}
	state := capabilityState(c.Sources[0], []agent.Tool{toolaskuser.Tool{}})
	if err := c.Restore(t.Context(), []conversation.Capability{state}); err == nil {
		t.Fatal("role exclusion bypassed by restore")
	}
	c = composed(t, Lazy)
	c.Sources = []*Capability{{ID: "external", Description: strings.Repeat("長", 2000)}}
	if len([]rune(c.Map())) > 1024 {
		t.Fatal("map exceeds bound")
	}
	out, err := (DiscoverCapabilities{Catalog: c}).Run(t.Context(), json.RawMessage(`{"query":"native_web"}`))
	if err != nil || !strings.Contains(out, NativeWeb) {
		t.Fatal(out, err)
	}
}

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
