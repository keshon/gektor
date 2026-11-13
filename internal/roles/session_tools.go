package roles

import (
	"context"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/tools/extension"
)

// SelectToolset changes a conversation's catalog without calling a model or tool action.
func (e Env) SelectToolset(ctx context.Context, snapshot conversation.Snapshot, id string, loaded bool) (conversation.Snapshot, error) {
	cfg := agent.Config{}
	err := e.configureTools(&cfg, true, true)
	if err != nil {
		return snapshot, err
	}
	catalog := cfg.Catalog.(*extension.Capabilities)
	if loaded && id == "browser" && e.Browser != nil {
		err = e.Browser.SelectProfile(snapshot.Browser)
		if err != nil {
			return snapshot, err
		}
		snapshot.Browser = e.Browser.Reference()
	}
	err = catalog.Restore(ctx, snapshot.Capabilities)
	if err != nil {
		return snapshot, err
	}
	if loaded {
		_, err = catalog.Enable(ctx, id, nil)
	} else {
		err = catalog.Unload(id)
	}
	if err != nil {
		return snapshot, err
	}
	snapshot.Capabilities = catalog.Save()
	return snapshot, nil
}
