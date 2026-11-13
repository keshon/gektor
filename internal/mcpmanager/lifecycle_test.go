package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/tools/extension"
)

type managedProbe struct{ calls *int }

func (*managedProbe) Name() string        { return "managed_probe" }
func (*managedProbe) Description() string { return "fixture" }
func (*managedProbe) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (*managedProbe) Mode() agent.ToolMode { return agent.Exclusive }
func (p *managedProbe) Run(context.Context, json.RawMessage) (string, error) {
	*p.calls++
	return "done", nil
}

func TestManagedLifecycleIsUniform(t *testing.T) {
	for _, bundled := range []bool{false, true} {
		for _, onLoad := range []bool{false, true} {
			t.Run(managedCaseName(bundled, onLoad), func(t *testing.T) {
				root := t.TempDir()
				manager, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close()
				loads, calls, closes := 0, 0, 0
				source := &extension.Capability{ID: "service", Description: "Fixture service", Eager: onLoad}
				if bundled {
					err = manager.RegisterBundled(source)
				} else {
					err = manager.Save(Server{ID: "service", Description: "Fixture service", URL: "https://example.com/mcp", Enabled: true, OnLoad: onLoad})
					source = manager.sources["service"]
				}
				if err != nil {
					t.Fatal(err)
				}
				source.Load = func(context.Context, func(string)) ([]agent.Tool, error) {
					loads++
					return []agent.Tool{&managedProbe{calls: &calls}}, nil
				}
				source.Close = func() { closes++ }
				if _, err = manager.Connect(t.Context(), "service"); err != nil {
					t.Fatal(err)
				}
				if source.Eager != onLoad || calls != 0 || loads != 1 {
					t.Fatal("connect changed exposure or called a tool")
				}
				a := &extension.Capabilities{Registry: agent.NewRegistry(), Sources: manager.Sources()}
				b := &extension.Capabilities{Registry: agent.NewRegistry(), Sources: manager.Sources()}
				if _, err = a.Enable(t.Context(), "service", nil); err != nil {
					t.Fatal(err)
				}
				if b.Registry.Has("managed_probe") || len(b.Save()) != 0 {
					t.Fatal("activation leaked across conversations")
				}
				if err = manager.Disconnect("service"); err != nil {
					t.Fatal(err)
				}
				if source.Eager != onLoad {
					t.Fatal("disconnect changed exposure")
				}
				resumed := &extension.Capabilities{Registry: agent.NewRegistry(), Sources: manager.Sources()}
				if err = resumed.Restore(t.Context(), a.Save()); err != nil {
					t.Fatal(err)
				}
				if loads != 1 {
					t.Fatal("restore reconnected")
				}
				run := func() error {
					_, err := resumed.Registry.Run(t.Context(), "managed_probe", json.RawMessage(`{}`))
					return err
				}
				if err = run(); err != nil || loads != 2 || calls != 1 {
					t.Fatal("reconnect failed", err, loads, calls)
				}
				settings, err := manager.Get("service")
				if err != nil {
					t.Fatal(err)
				}
				settings.Enabled = false
				if err = manager.Configure(settings); err != nil {
					t.Fatal(err)
				}
				if err = run(); err == nil || loads != 2 || calls != 1 {
					t.Fatal("disabled wrapper executed", err)
				}
				settings.Enabled = true
				if err = manager.Configure(settings); err != nil {
					t.Fatal(err)
				}
				if manager.Sources()[0] != source {
					t.Fatal("toggle replaced the stable source")
				}
				if err = run(); err != nil || loads != 3 || calls != 2 {
					t.Fatal("reenable failed", err)
				}
				if bundled {
					if err = manager.Save(settings); err == nil {
						t.Fatal("bundled source overwritten")
					}
					if err = manager.Delete("service"); err == nil {
						t.Fatal("bundled source deleted")
					}
					if _, err = manager.Import([]byte(`{"mcpServers":{"service":{"url":"https://other.example/mcp"}}}`), true); err == nil {
						t.Fatal("import replaced bundle")
					}
					if _, err = os.Stat(filepath.Join(root, "mcp", "service.json")); !os.IsNotExist(err) {
						t.Fatal("bundle persisted raw definition", err)
					}
				} else {
					settings.URL = "https://other.example/mcp"
					if err = manager.Save(settings); err != nil {
						t.Fatal(err)
					}
					if err = run(); err == nil || calls != 2 || loads != 3 {
						t.Fatal("superseded adapter reconnected", err)
					}
				}
				if closes < 2 {
					t.Fatal("cleanup not called")
				}
			})
		}
	}
}

func managedCaseName(bundled, onLoad bool) string {
	origin, loading := "custom", "demand"
	if bundled {
		origin = "bundled"
	}
	if onLoad {
		loading = "load"
	}
	return origin + "/" + loading
}

func TestConnectKeepsStatusReadableAndExposureOnFailure(t *testing.T) {
	root := t.TempDir()
	manager, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	started, release := make(chan struct{}), make(chan struct{})
	source := &extension.Capability{ID: "fixture", Eager: true, Load: func(ctx context.Context, _ func(string)) ([]agent.Tool, error) {
		close(started)
		select {
		case <-release:
			return nil, errors.New("unavailable")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	if err = manager.RegisterBundled(source); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := manager.Connect(t.Context(), "fixture"); done <- err }()
	<-started
	listed := make(chan []Status, 1)
	go func() { listed <- manager.List() }()
	select {
	case statuses := <-listed:
		if statuses[0].State != "connecting" {
			t.Fatal(statuses)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("status blocked behind preparation")
	}
	close(release)
	if err = <-done; err == nil {
		t.Fatal("missing connection failure")
	}
	status := manager.List()[0]
	if !source.Eager || !status.OnLoad || status.State != "failed" || status.Error == "" {
		t.Fatal("failure changed exposure or lost diagnostic", status)
	}
}

func TestBundledRegistrationRejectsCollisions(t *testing.T) {
	root := t.TempDir()
	manager, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), []string{"reserved"})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = manager.Save(Server{ID: "browser", Description: "Saved browser", URL: "https://example.com/mcp", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"browser", "BROWSER", "RESERVED", "native_browser"} {
		if err = manager.RegisterBundled(&extension.Capability{ID: id}); err == nil {
			t.Fatal("collision accepted", id)
		}
	}
}
