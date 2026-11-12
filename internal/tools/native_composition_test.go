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
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	toolweb "github.com/keshon/gektor/internal/tools/web"
)

func TestNativeEagerLazyAndResume(t *testing.T) {
	eager, lazy := composed(t, toolextension.Eager), composed(t, toolextension.Lazy)
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
	for _, c := range []*toolextension.Capabilities{eager, lazy} {
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
	resumed := composed(t, toolextension.Eager)
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
	fresh := composed(t, toolextension.Eager)
	if err := fresh.Restore(t.Context(), saved); err == nil || fresh.Registry.Has("webfetch") {
		t.Fatal("changed native package published")
	}
	missing := &toolextension.Capabilities{Registry: agent.NewRegistry()}
	if err := missing.Restore(t.Context(), lazy.Save()); err == nil {
		t.Fatal("missing native package restored")
	}
}

func TestNativeActivationFailureIsAtomic(t *testing.T) {
	c := composed(t, toolextension.Lazy)
	if err := c.Registry.Add(toolweb.Check{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Enable(t.Context(), NativeWeb, nil); err == nil {
		t.Fatal("collision accepted")
	}
	if c.Registry.Has("webfetch") || len(c.Save()) != 0 {
		t.Fatal("partial publication")
	}
	c = composed(t, toolextension.Lazy)
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
	c := &toolextension.Capabilities{Registry: agent.NewRegistry(), ForbiddenTools: []string{"ask_user"}, Sources: []*toolextension.Capability{{ID: "questions", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		return []agent.Tool{toolaskuser.Tool{}}, nil
	}}}}
	if _, err := c.Enable(t.Context(), "questions", nil); err == nil || c.Registry.Has("ask_user") {
		t.Fatal("role exclusion bypassed")
	}
	allowed := &toolextension.Capabilities{Registry: agent.NewRegistry(), Sources: c.Sources}
	if _, err := allowed.Enable(t.Context(), "questions", nil); err != nil {
		t.Fatal(err)
	}
	state := allowed.Save()[0]
	if err := c.Restore(t.Context(), []conversation.Capability{state}); err == nil {
		t.Fatal("role exclusion bypassed by restore")
	}
	c = composed(t, toolextension.Lazy)
	c.Sources = []*toolextension.Capability{{ID: "external", Description: strings.Repeat("長", 2000)}}
	if len([]rune(c.Map())) > 1024 {
		t.Fatal("map exceeds bound")
	}
	out, err := (toolextension.DiscoverCapabilities{Catalog: c}).Run(t.Context(), json.RawMessage(`{"query":"native_web"}`))
	if err != nil || !strings.Contains(out, NativeWeb) {
		t.Fatal(out, err)
	}
}
