package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	toolaskuser "github.com/keshon/gektor/internal/tools/askuser"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	toolimages "github.com/keshon/gektor/internal/tools/images"
	toolweb "github.com/keshon/gektor/internal/tools/web"
	"github.com/keshon/gektor/internal/workspace"
)

// Reviewed native schemas and modes. Update individual entries only for an
// intentional interface change. Plan eligibility uses the readonly name set.
func TestNativePackageBaseline(t *testing.T) {
	var want map[string]struct {
		Definitions []llm.ToolDef
		Modes       map[string]agent.ToolMode
	}
	data, err := os.ReadFile("testdata/package_baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	for name, reg := range map[string]*agent.Registry{"base": Base(nil, nil, nil, false), "readonly": ReadOnly(nil, nil, false)} {
		expected := want[name]
		for i := range expected.Definitions {
			d := &expected.Definitions[i]
			d.Description = strings.ReplaceAll(d.Description, "pwsh.exe", commandrun.ShellName())
			if runtime.GOOS != "windows" {
				d.Description = strings.ReplaceAll(d.Description, "PowerShell syntax/scripts", "POSIX sh syntax/scripts")
				d.Description = strings.ReplaceAll(d.Description, "PowerShell source using "+commandrun.ShellName()+". For", "POSIX sh source for")
			}
		}
		a, _ := json.Marshal(expected.Definitions)
		b, _ := json.Marshal(reg.Defs())
		if string(a) != string(b) {
			t.Fatalf("%s tool definitions differ from the reviewed baseline", name)
		}
		for tool, mode := range expected.Modes {
			if reg.ModeOf(tool) != mode {
				t.Fatalf("%s mode changed", tool)
			}
		}
	}
}

func composed(t *testing.T, exposure toolextension.Exposure) *toolextension.Capabilities {
	t.Helper()
	c := &toolextension.Capabilities{}
	r, err := Compose(Dependencies{WS: nil, Vision: false}, c, map[string]toolextension.Exposure{NativeWeb: exposure})
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	return c
}

func TestCompositionKeepsNativeInterfacesAndSessionState(t *testing.T) {
	a, b := composed(t, toolextension.Eager), composed(t, toolextension.Eager)
	if a.Registry.Checklist() == nil || a.Registry.Checklist() == b.Registry.Checklist() {
		t.Fatal("checklists absent or shared")
	}
	if _, err := a.Registry.Run(t.Context(), "todo", json.RawMessage(`{"items":[{"id":1,"state":"pending","text":"one"}]}`)); err != nil {
		t.Fatal(err)
	}
	_, open := b.Registry.TodoProgress()
	if len(open) != 0 {
		t.Fatal("child inherited checklist state")
	}
	// The package publishes concrete instances; optional interfaces are intact.
	for _, p := range builtinPackages(Dependencies{}) {
		for _, entry := range p.entries {
			tool := entry.tool
			if tool.Name() == "load_skill" {
				if _, ok := tool.(agent.ContextReuser); !ok {
					t.Fatal("skill reuse lost")
				}
			}
			if tool.Name() == "read_file" {
				if _, ok := tool.(agent.Attacher); !ok {
					t.Fatal("image attachment lost")
				}
			}
		}
	}
	if err := a.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Registry.Defs(), Base(nil, nil, nil, false).Defs()) {
		t.Fatal("prepared default catalog changed")
	}
	if b.Registry.Has("webfetch") || len(b.Save()) != 0 {
		t.Fatal("activation leaked between agents")
	}
}

func nativeFixture(c *toolextension.Capabilities, id string) *toolextension.NativePackage {
	for _, p := range c.Native {
		if p.ID == id {
			return p
		}
	}
	panic("missing native package: " + id)
}

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

func TestReadOnly_ContainsNoMutatingOrShellTools(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	reg := ReadOnly(ws, toolbackground.New(), false)

	forbidden := map[string]bool{
		"write_file": true, "patch_file": true, "git_add": true, "git_commit": true,
		"move_file": true, "run_shell": true, "run_program": true,
		"start_background": true, "start_program": true, "stop_background": true,
		"ask_user": true, "delegate_task": true,
	}
	var names []string
	for _, def := range reg.Defs() {
		names = append(names, def.Name)
		if forbidden[def.Name] {
			t.Errorf("ReadOnly registry contains forbidden tool %q", def.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("ReadOnly registry is empty")
	}
}

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

func TestBuiltinSocialLifecycleIsLazyAndRestorable(t *testing.T) {
	sources := WithBuiltinCapabilities(nil)
	registry := agent.NewRegistry()
	catalog := &toolextension.Capabilities{Sources: sources, Registry: registry}
	if _, err := (toolextension.DiscoverCapabilities{Catalog: catalog}).Run(t.Context(), json.RawMessage(`{"query":"Twitter"}`)); err != nil {
		t.Fatal(err)
	}
	if connected, _ := sources[0].Connected(); connected || registry.Has("cap__social__read_post") {
		t.Fatal("discovery activated social tools")
	}
	if _, err := catalog.Enable(t.Context(), "social", nil); err != nil {
		t.Fatal(err)
	}
	if !registry.Has("cap__social__read_post") || len(catalog.Save()) != 1 {
		t.Fatal("activation did not save catalog")
	}
	resumedSources := WithBuiltinCapabilities(nil)
	resumed := &toolextension.Capabilities{Sources: resumedSources, Registry: agent.NewRegistry()}
	if err := resumed.Restore(t.Context(), catalog.Save()); err != nil {
		t.Fatal(err)
	}
	if connected, _ := resumedSources[0].Connected(); connected || !resumed.Registry.Has("cap__social__read_post") {
		t.Fatal("resume eagerly loaded adapter")
	}
	output, err := resumed.Registry.Run(t.Context(), "cap__social__read_post", json.RawMessage(`{"url":"https://example.org/unsupported"}`))
	if err == nil || !strings.Contains(output, "unsupported") {
		t.Fatal(output, err)
	}
	custom := &toolextension.Capability{ID: "social", Description: "custom provider"}
	if configured := WithBuiltinCapabilities([]*toolextension.Capability{custom}); len(configured) != 1 || configured[0] != custom {
		t.Fatal("custom provider was shadowed")
	}
}

func TestImageToolsStayLazy(t *testing.T) {
	ws, _ := testWS(t)
	catalog := &toolextension.Capabilities{}
	reg, err := Compose(Dependencies{WS: ws, Vision: true}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Registry = reg
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if reg.Has(name) {
			t.Fatal("image schema added to startup", name)
		}
	}
	if _, err := catalog.Enable(t.Context(), "native_images", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if !reg.Has(name) {
			t.Fatal("missing lazy image tool", name)
		}
	}
	if err := agent.NewRegistry(&toolimages.View{WS: ws, Vision: true}, &toolimages.Download{Vision: true}, toolimages.Attachments{}).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDiscoveryUsesActivityAndToolNames(t *testing.T) {
	catalog := composed(t, toolextension.Eager)
	tool := toolextension.DiscoverCapabilities{Catalog: catalog}
	for query, id := range map[string]string{
		"ssh connect host": "native_ssh", "sftp upload": "native_ssh", "image_view": "native_images", "png crop": "native_images", "workflow cron": "native_workflows",
	} {
		args, err := json.Marshal(map[string]string{"query": query})
		if err != nil {
			t.Fatal(err)
		}
		out, err := tool.Run(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Capabilities []struct {
				ID string `json:"id"`
			} `json:"capabilities"`
		}
		err = json.Unmarshal([]byte(out), &page)
		if err != nil || len(page.Capabilities) == 0 || page.Capabilities[0].ID != id {
			t.Fatal(query, out, err)
		}
	}
	if len(catalog.Save()) != 0 {
		t.Fatal("search activated native tools")
	}
}
