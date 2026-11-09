# Schedule and queue workflows

Save a recipe, choose its schedule and keep a workflow service running for the same workspace. The service has its own permission rules.

## Schedule a workflow

Save the recipe first. In **Node**, choose **Add schedule trigger…**.
You can also press `A` and choose **Schedule trigger** from the node types.

If the recipe has inputs, fill in the values to use for scheduled runs.
Then choose **Every hour**, **Daily**, or **Cron**. For Daily, enter a time
such as `09:00`. For Cron, enter a five-field expression such as `*/15 * * * *`.
Set the time zone explicitly; the default is `UTC`.
Choose **Save schedule** to enable it.

A Schedule card appears on the canvas. It starts the whole recipe; its
connections are automatic. Select it and press Enter to change the timing,
input values or enabled setting. Delete removes the trigger without deleting
queued jobs or saved runs. **Refresh schedule status** in Run reloads CLI changes
and updates the inspector's saved observations: worker ownership, latest job,
latest run and declared outputs. Worker ownership does not prove health or success.

Keep the workflow service running for this workspace:

```powershell
.\serve-gektor-workflows.cmd -workspace "D:\path\to\project"
```

The service uses its own permission rules. Start it outside GEKTOR if it must
continue after the TUI closes. Processes launched by the Agent belong to that
GEKTOR launch and stop when it closes. The inspector's **Service** tab explains
service setup.

A schedule stores a copy of the recipe and input values. Saving later recipe
edits does not change that copy. The inspector reports snapshot changes in the
root and called recipes after a status refresh. It reports unavailable files
separately and does not request additional read access. Edit and save the trigger
again to update its snapshot.
Schedules belong to the workspace and are stored in your user data, outside
the recipe JSON. Copying a recipe to another workspace does not enable its schedule.

## Background execution

See [the CLI reference](../reference/cli.md#workflow-queue-and-service) for submit, serve,
schedules, cancellation and review commands. Queue entries freeze recipe bytes
and inputs. Referenced workspace files, model profiles and external services
remain live resources; a snapshot does not freeze their contents.

The optional [HTTP API](../reference/http-api.md#http-api-and-webhooks) submits to this same queue.
Named credentials reference environment values; they are not a secret vault.
The TUI can edit recipes, inspect runs and manage a recipe's Schedule trigger.
See [the TUI guide](automation.md#schedule-a-workflow) for timing, input values and
service setup. Schedule cards describe triggers stored in the workspace's
user data; they are not executable steps in recipe JSON. Existing CLI schedules
for an opened recipe also appear as cards. Queue management uses the CLI/service
commands linked above.

The Agent can enable `native_workflows` and call `workflow_schema` to discover
operation inputs, outputs and schedule triggers. The bundled `workflows` skill
is available in every workspace. `workflow_schedule` creates, updates, pauses,
resumes or removes a trigger; `workflow_schedules` lists settings and revisions. For example, after saving
and validating a recipe, this schedules it every minute:

```json
{"action":"create","id":"uptime-minute","recipe_path":"uptime-checker-recipe.json","cron":"* * * * *","timezone":"UTC"}
```

Use `paused: true` when creating a trigger that should remain disabled.
To update a trigger, first list it and pass its `expected_revision`. Omitted
settings stay unchanged. Set `refresh_recipe: true` to replace its frozen recipe.
Use `workflow_status` with `schedule_id` for schedule settings, next due time,
the latest job, the latest run with declared outputs, and snapshot changes.
A queued job can be newer than the last run; both are shown separately. Use
`workflow_inspect` when you need more detail. A held worker lease shows queue
ownership, not worker health. These are saved observations, not an atomic health
check.

Both `workflow_status` and `workflow_schedules` report snapshot state as
`unchanged`, `changed` or `unavailable`. Comparison covers the saved root and
called recipes, ignoring JSON spacing and object key order. A changed parent
also signals changes to its dependency list. Unreadable or missing files are
reported separately; inspection never asks for additional file access or
refreshes a snapshot. Use `refresh_recipe: true` explicitly after reviewing changes.

New scheduled jobs and their run records save `schedule_id` (named `schedule`
in the full job record) and `scheduled_for`, the intended trigger time. The job's
`created` time and the run's `started` time describe later stages. Manual jobs
and older records have no intended trigger time. This metadata is for inspection;
recipes do not gain a new `$run` reference.

Creating a schedule does not start the service or execute the recipe.
The service uses its own permission rules. These triggers use the same store
as the TUI and CLI; they do not use the operating system's scheduler.
