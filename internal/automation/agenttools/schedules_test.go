package agenttools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestAgentScheduleCreatesFrozenTriggerWithoutExecutingRecipe(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	source := `{"version":3,"name":"Scheduled","inputs":{"n":{"type":"integer","required":true}},"steps":[{"id":"calc","tool":"script_transform","args":{"code":"return input;","input":{"$input":"n"}}}]}`
	if err := ws.WriteFile("recipe.json", []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	tool := Schedule{WS: ws, StoreDir: base, Authorize: allow}
	args := json.RawMessage(`{"action":"create","id":"minute","recipe_path":"recipe.json","cron":"* * * * *","inputs":{"n":9007199254740993}}`)
	if out, err := tool.Run(t.Context(), args); err != nil || !strings.Contains(out, `"service_required":true`) {
		t.Fatal(out, err)
	}
	if _, err := tool.Run(t.Context(), args); err == nil {
		t.Fatal("overwrote an existing schedule")
	}
	db, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	all, err := db.Schedules(t.Context())
	if err != nil || len(all) != 1 || all[0].Timezone != "UTC" || string(all[0].Inputs["n"]) != "9007199254740993" {
		t.Fatal(all, err)
	}
	if runs, err := db.Runs(); err != nil || len(runs) != 0 {
		t.Fatal("scheduling executed a recipe", runs, err)
	}
	if err := ws.WriteFile("recipe.json", []byte(strings.Replace(source, "Scheduled", "Changed", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Tick(t.Context(), all[0].Next); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || len(jobs) != 1 || strings.Contains(string(jobs[0].Source), "Changed") {
		t.Fatal("queued live recipe instead of snapshot", jobs, err)
	}
	for _, action := range []string{"pause", "resume", "pause", "remove"} {
		if _, err := tool.Run(t.Context(), json.RawMessage(`{"action":"`+action+`","id":"minute","recipe_path":"wrong.json"}`)); err == nil {
			t.Fatal("changed another recipe's trigger")
		}
		if _, err := tool.Run(t.Context(), json.RawMessage(`{"action":"`+action+`","id":"minute","recipe_path":"recipe.json"}`)); err != nil {
			t.Fatal(action, err)
		}
		if action == "pause" {
			if err := db.Tick(t.Context(), time.Now().Add(24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	all, _ = db.Schedules(t.Context())
	if len(all) != 0 {
		t.Fatal("remove failed")
	}
	listed, err := (Schedule{WS: ws, StoreDir: base, List: true}).Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(listed, `"schedules":[]`) {
		t.Fatal(listed, err)
	}
	for _, bad := range []string{
		`{"action":"create","id":"bad","recipe_path":"../escape.json","cron":"* * * * *"}`,
		`{"action":"create","id":"bad","recipe_path":"recipe.json","cron":"* * * * *"}`,
		`{"action":"create","id":"bad","recipe_path":"recipe.json","cron":"garbage","inputs":{"n":1}}`,
	} {
		if _, err := tool.Run(t.Context(), json.RawMessage(bad)); err == nil {
			t.Fatal("invalid schedule accepted", bad)
		}
	}
}

func TestWorkflowSchemaAdvertisesScheduleTrigger(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	out, err := (Schema{WS: ws, StoreDir: t.TempDir()}).Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Scheduling struct {
			Tool    string          `json:"tool"`
			Example json.RawMessage `json:"example"`
		} `json:"schedule_trigger"`
		Operations map[string]json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal([]byte(out), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Scheduling.Tool != "workflow_schedule" || !strings.Contains(string(schema.Scheduling.Example), "* * * * *") || schema.Operations["workflow_schedule"] != nil {
		t.Fatal("trigger is missing or advertised as an executable step", out)
	}
}

func TestScheduleUpdatePreservesSnapshotAndRejectsStaleRevision(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	source := `{"version":3,"name":"Original","inputs":{"n":{"type":"integer","default":3}},"steps":[{"id":"calc","tool":"script_transform","args":{"code":"return input;","input":{"$input":"n"}}}]}`
	if err := ws.WriteFile("recipe.json", []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	tool := Schedule{WS: ws, StoreDir: base, Authorize: allow}
	call := func(args string) {
		t.Helper()
		if out, err := tool.Run(t.Context(), json.RawMessage(args)); err != nil {
			t.Fatal(out, err)
		}
	}
	call(`{"action":"create","id":"daily","recipe_path":"recipe.json","cron":"0 9 * * *","paused":true,"inputs":{"n":7}}`)
	db, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	all, _ := db.Schedules(t.Context())
	original := all[0]
	if err := ws.WriteFile("recipe.json", []byte(strings.Replace(source, "Original", "Changed", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	update := `{"action":"update","id":"daily","recipe_path":"recipe.json","expected_revision":1,"cron":"0 10 * * *"}`
	call(update)
	all, _ = db.Schedules(t.Context())
	if !all[0].Paused || string(all[0].Source) != string(original.Source) || string(all[0].Inputs["n"]) != "7" {
		t.Fatal("lost omitted settings", all)
	}
	if _, err := tool.Run(t.Context(), json.RawMessage(update)); err == nil {
		t.Fatal("accepted stale revision")
	}
	call(`{"action":"update","id":"daily","recipe_path":"recipe.json","expected_revision":2,"refresh_recipe":true,"inputs":{}}`)
	all, _ = db.Schedules(t.Context())
	if !strings.Contains(string(all[0].Source), "Changed") || len(all[0].Inputs) != 0 {
		t.Fatal("refresh or clear failed", all)
	}
	call(`{"action":"remove","id":"daily","recipe_path":"recipe.json"}`)
	call(`{"action":"create","id":"daily","recipe_path":"recipe.json","cron":"0 9 * * *","paused":true}`)
	if _, err := tool.Run(t.Context(), json.RawMessage(update)); err == nil {
		t.Fatal("stale revision modified replacement")
	}
}
