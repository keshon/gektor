package agenttools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

// Schedule manages triggers in the same workspace store used by the editor and service.
type Schedule struct {
	WS        *workspace.Workspace
	StoreDir  string
	List      bool
	Authorize func(context.Context, string, json.RawMessage) error
}

func (t Schedule) Name() string {
	if t.List {
		return "workflow_schedules"
	}
	return "workflow_schedule"
}
func (t Schedule) Description() string {
	if t.List {
		return "List this workspace's saved schedule triggers, next due times and paused state. Does not start the service or execute recipes."
	}
	return "Create, update, pause, resume or remove a native workflow schedule trigger. A trigger starts a saved recipe, not a step in recipe JSON. Create snapshots and validates recipe_path and inputs; cron uses five fields and timezone defaults to UTC. Update requires expected_revision from workflow_schedules. Set refresh_recipe=true explicitly to replace the frozen recipe; omitted settings stay unchanged. The workflow service must be running separately; its permissions govern execution. No OS scheduler is needed."
}
func (t Schedule) ReadOnly() bool { return t.List }
func (t Schedule) Mode() agent.ToolMode {
	if t.List {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func (t Schedule) Schema() json.RawMessage {
	if t.List {
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["create","update","pause","resume","remove"]},"id":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"},"recipe_path":{"type":"string","minLength":1},"cron":{"type":"string"},"timezone":{"type":"string"},"inputs":{"type":"object","maxProperties":32},"paused":{"type":"boolean"},"expected_revision":{"type":"integer","minimum":0},"refresh_recipe":{"type":"boolean"},"overlap":{"type":"string","enum":["skip","queue"]},"missed":{"type":"string","enum":["skip","once"]}},"required":["action","id","recipe_path"],"additionalProperties":false}`)
}
func (t Schedule) Resources(args json.RawMessage) ([]string, error) {
	if t.List {
		return nil, nil
	}
	paths, err := ScheduleResources(args)
	if err != nil {
		return nil, err
	}
	if t.WS == nil {
		return nil, fmt.Errorf("workspace unavailable")
	}
	for i, path := range paths {
		rel, err := t.WS.Relative(path)
		if err != nil {
			return nil, err
		}
		paths[i] = filepath.ToSlash(rel)
	}
	return paths, nil
}

func ScheduleResources(args json.RawMessage) ([]string, error) {
	var in struct {
		Path string `json:"recipe_path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.Path == "" {
		return nil, fmt.Errorf("recipe_path is required")
	}
	return []string{in.Path}, nil
}
func (t Schedule) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		Action           string                     `json:"action"`
		ID               string                     `json:"id"`
		Path             string                     `json:"recipe_path"`
		Cron             string                     `json:"cron"`
		Timezone         string                     `json:"timezone"`
		Inputs           map[string]json.RawMessage `json:"inputs"`
		Paused           *bool                      `json:"paused"`
		ExpectedRevision *uint64                    `json:"expected_revision"`
		RefreshRecipe    bool                       `json:"refresh_recipe"`
		Overlap          string                     `json:"overlap"`
		Missed           string                     `json:"missed"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	db, err := store.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer db.Close()
	if !t.List {
		path, err := t.WS.Relative(in.Path)
		if err != nil {
			return "", err
		}
		var expected *store.Schedule
		value := store.Schedule{ID: in.ID, RecipePath: path, Timezone: "UTC"}
		if in.Action != "create" {
			all, err := db.Schedules(ctx)
			if err != nil {
				return "", err
			}
			for _, saved := range all {
				if saved.ID != in.ID {
					continue
				}
				actual, err := t.WS.Relative(saved.RecipePath)
				if err != nil || actual != path {
					return "", fmt.Errorf("schedule does not belong to recipe_path")
				}
				value, expected = saved, &saved
				break
			}
			if expected == nil {
				return "", fmt.Errorf("unknown schedule")
			}
			if in.ExpectedRevision != nil && *in.ExpectedRevision != expected.Revision {
				return "", fmt.Errorf("schedule revision changed; list schedules before editing")
			}
		}
		switch in.Action {
		case "create", "update":
			if in.Action == "update" && in.ExpectedRevision == nil {
				return "", fmt.Errorf("update requires expected_revision from workflow_schedules")
			}
			if in.Action == "create" && in.Cron == "" {
				return "", fmt.Errorf("create requires cron, for example * * * * * for every minute")
			}
			if in.Cron != "" {
				value.Cron = in.Cron
			}
			if in.Timezone != "" {
				value.Timezone = in.Timezone
			}
			if in.Inputs != nil {
				value.Inputs = in.Inputs
			}
			if in.Paused != nil {
				value.Paused = *in.Paused
			}
			if in.Overlap != "" {
				value.Overlap = in.Overlap
			}
			if in.Missed != "" {
				value.Missed = in.Missed
			}
			r, err := app.AutomationRegistry(t.WS, t.StoreDir, nil)
			if err != nil {
				return "", err
			}
			svc := service.Service{WS: t.WS, StoreDir: t.StoreDir, Registry: r, ValidateOnly: true, Authorize: t.Authorize}
			if in.Action == "create" || in.RefreshRecipe {
				value.Source, err = svc.SnapshotRecipe(ctx, path)
				if err != nil {
					return "", err
				}
			}
			if _, err := svc.RunSource(ctx, value.Source, path, value.Inputs); err != nil {
				return "", err
			}
			if expected == nil {
				err = db.PutSchedule(ctx, value)
			} else {
				err = db.UpdateSchedule(ctx, value, *expected)
			}
			if err != nil {
				return "", err
			}
		case "pause", "resume", "remove":
			if in.Cron != "" || in.Timezone != "" || in.Inputs != nil || in.Paused != nil || in.RefreshRecipe || in.Overlap != "" || in.Missed != "" {
				return "", fmt.Errorf("pause, resume and remove accept action, id, recipe_path and optional expected_revision only")
			}
			if in.Action == "remove" {
				err = db.DeleteScheduleIfUnchanged(ctx, *expected)
			} else {
				value.Paused = in.Action == "pause"
				err = db.UpdateSchedule(ctx, value, *expected)
			}
			if err != nil {
				return "", err
			}
		}
	}

	all, err := db.Schedules(ctx)
	if err != nil {
		return "", err
	}
	type summary struct {
		ID             string                     `json:"id"`
		Path           string                     `json:"recipe_path"`
		Cron           string                     `json:"cron"`
		Timezone       string                     `json:"timezone"`
		Paused         bool                       `json:"paused"`
		Next           time.Time                  `json:"next_due"`
		Revision       uint64                     `json:"revision"`
		Inputs         map[string]json.RawMessage `json:"inputs"`
		Overlap        string                     `json:"overlap"`
		Missed         string                     `json:"missed"`
		SnapshotSHA256 string                     `json:"snapshot_sha256"`
	}
	items := []summary{}
	for _, s := range all {
		items = append(items, summary{s.ID, s.RecipePath, s.Cron, s.Timezone, s.Paused, s.Next, s.Revision, s.Inputs, s.Overlap, s.Missed, fmt.Sprintf("%x", sha256.Sum256(s.Source))})
	}
	b, err := json.Marshal(struct {
		Schedules       []summary `json:"schedules"`
		ServiceRequired bool      `json:"service_required"`
		Note            string    `json:"note"`
	}{items, true, "The workflow service must run separately for this workspace. Saving a schedule does not start it. Recipes and inputs are snapshotted; service permissions still apply."})
	return string(b), err
}
