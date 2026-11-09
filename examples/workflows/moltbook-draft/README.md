# Read a feed and save a draft

## Before running

The commands use PowerShell from the extracted application folder or a
[built source checkout](../../../docs/development/README.md). On Linux, use
`./gektor` in place of `.\gektor.exe` and equivalent shell syntax.
Copy this folder before editing and select that copy with `-workspace`.
Each run creates a separate saved record.

This example reads the public Moltbook posts feed, asks a model for a draft,
and saves the answer as a run artifact. It never publishes, comments or votes.
It needs no Moltbook API key.

## Setup

Run commands from the GEKTOR application folder.

1. Extract a release or build GEKTOR as described above.
2. Edit `model.json` in this folder to match your model server.
   The supplied profile uses llama.cpp at `http://127.0.0.1:9931`.
   This workflow reads that file; it does not use your TUI model settings.
3. Register the state schema once for this workspace:

```powershell
.\gektor.exe workflow register-schema -workspace examples/workflows/moltbook-draft feed_state state.schema.json
```

You do not need to create a state record. The first read uses
`allow_missing: true`; the first write creates it with revision zero.
Running the schema command again with the same schema is allowed.

For ChatGPT, use `gektor auth login` if you are not already signed in.
The included `chatgpt.json` profile uses `gpt-6.1-sol`.
Choose an available model from `gektor auth models` if needed.
Set the run input **profile** to `chatgpt.json` in the TUI.
For CLI runs, use the supplied input file:

```powershell
.\gektor.exe workflow run -workspace examples/workflows/moltbook-draft -inputs chatgpt.inputs.json recipe.json
```

The profiles contain no credentials. The ChatGPT profile uses the signed-in account.

## Try it in the TUI

Choose this example folder as the workspace in Settings.
Open `recipe.json` in Workflows.

1. Validate with `V`, keeping the default inputs.
2. Run with `R`. Answer the permission requests.
3. Select **save_draft** and open Result to find the saved artifact ID.
4. Select **draft** to read the model's decision, title, content and reason.
5. Open **remember** or **remember_idle** to inspect the saved state.

A model may decide that nothing is worth posting. Its decision is still saved.
No publication happens, even if `should_post` is true.

To run from the command line instead:

```powershell
.\gektor.exe workflow validate -workspace examples/workflows/moltbook-draft recipe.json
.\gektor.exe workflow run -workspace examples/workflows/moltbook-draft recipe.json
.\gektor.exe workflow runs -workspace examples/workflows/moltbook-draft
```

Use the run ID to list and export the artifact:

```powershell
.\gektor.exe workflow artifacts -workspace examples/workflows/moltbook-draft <run-id>
.\gektor.exe workflow export-artifact -workspace examples/workflows/moltbook-draft <run-id> <artifact-id> draft.json
```

The last argument is a new destination inside the selected workspace.
See [artifact commands](../../../docs/reference/cli.md#workflow-artifacts) for details.

## What each step does

| Step | Work |
|---|---|
| state | Read this workspace's saved state, or report that it does not exist |
| fetch | Read up to five recent posts |
| prepare | Check the HTTP response and select posts not already seen |
| draft | Ask a model for a structured draft, only if there are new posts |
| package | Put the decision and source IDs into text for an artifact |
| save_draft | Save that text as a run-owned `draft.json` artifact |
| remember | Mark the selected posts as seen after the artifact is saved |
| remember_idle | Save the HTTP status when there are no new posts or the API returns an error |

Only one of the two state-writing steps runs.
The state keeps the most recent 200 seen IDs.
It uses revision checks to avoid silently overwriting another run's state.

## Errors and repeat runs

A `429`, `401` or other non-2xx response is recorded without calling the model.
The state keeps `last_http_status` and the `retry_after` header, when supplied.
This example records the wait information; it does not change a schedule or
sleep until that time.

A network failure, malformed successful feed, or model failure stops the run.
The selected posts are not marked as seen.
If saving the draft succeeds but saving state fails, the artifact remains.
Inspect that run before retrying; a retry may create another local draft.

Repeated runs skip posts already recorded as seen.
The five-post fetch and 200-ID history are limits of this example.
It is not a complete feed archive or a publishing bot.

Feed text is passed to the model as untrusted data, with instructions to draft
only. The model has no tools in that step. Review its text before using it.

## Background use

First test a foreground run and inspect its results.
You can later schedule this same recipe through the
[workflow service](../../../docs/reference/cli.md#workflow-queue-and-service).
There is no endless loop inside the recipe.

A publishing version would need a separate reviewed action and a way to check
whether an uncertain request already succeeded. Neither is included here.
