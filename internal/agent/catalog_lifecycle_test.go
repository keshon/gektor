package agent

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// lifecycleCatalog records the boundary order while retaining selection state.
type lifecycleCatalog struct {
	events  *[]string
	states  []conversation.Capability
	failure error
}

func (c *lifecycleCatalog) Prepare(context.Context) error {
	*c.events = append(*c.events, "prepare")
	return c.failure
}
func (*lifecycleCatalog) PreviewDefs() []llm.ToolDef        { return nil }
func (c *lifecycleCatalog) Save() []conversation.Capability { return c.states }
func (c *lifecycleCatalog) Restore(_ context.Context, states []conversation.Capability) error {
	*c.events = append(*c.events, "restore catalog")
	c.states = states
	return nil
}

type lifecycleBrowser struct {
	events  *[]string
	profile *conversation.BrowserProfile
}

func (b *lifecycleBrowser) Reference() *conversation.BrowserProfile { return b.profile }
func (b *lifecycleBrowser) SelectProfile(profile *conversation.BrowserProfile) error {
	*b.events = append(*b.events, "restore browser")
	b.profile = profile
	return nil
}

func TestCatalogPreparationFailureRetainsRestoredSelections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	snapshot := conversation.New([]llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "inspect"}})
	snapshot.Capabilities = []conversation.Capability{{ID: "saved", Version: "1", Fingerprint: "catalog"}}
	snapshot.Browser = &conversation.BrowserProfile{Channel: "chrome", Profile: "work"}
	if err := snapshot.Save(path); err != nil {
		t.Fatal(err)
	}
	var events []string
	failure := errors.New("eager integration failed")
	catalog := &lifecycleCatalog{events: &events, failure: failure}
	browser := &lifecycleBrowser{events: &events}
	client := &stubClient{}
	a := New(Config{Client: client, Tools: NewRegistry(), Catalog: catalog, Browser: browser, StateFile: path, System: "system"})
	_, err := a.Resume(t.Context(), snapshot.Context(), "continue")
	if !errors.Is(err, failure) || client.calls != 0 {
		t.Fatal(err, client.calls)
	}
	if !reflect.DeepEqual(events, []string{"restore browser", "restore catalog", "prepare"}) {
		t.Fatal(events)
	}
	saved, err := conversation.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Capabilities, snapshot.Capabilities) || !reflect.DeepEqual(saved.Browser, snapshot.Browser) {
		t.Fatal("restored selections lost on failed preparation", saved)
	}
	if len(saved.Runs) != 1 || saved.Runs[0].Outcome != "failed" {
		t.Fatal(saved.Runs)
	}
}
