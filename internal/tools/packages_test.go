package tools

import (
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
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
