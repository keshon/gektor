# Get started

Download and extract a [release](https://github.com/keshon/gektor/releases) for
Windows or Linux amd64. Keep the executable, profiles and examples together.
These instructions run from the extracted application folder. A source checkout
also works after [building GEKTOR](development/README.md).

Choose an Agent task if you want a model to work on your project. Choose a
workflow to try saved steps without a model connection.

## Run your first Agent task

1. Start a model server with a tool-calling model and wait for it to load.
   GEKTOR defaults to llama.cpp at `http://127.0.0.1:9931`.
   For another server, an API or ChatGPT, follow [Connect a model](guide/models.md).
2. On Windows, open `start-tui-agent.cmd`. On Linux, run `./gektor -tui`.
3. Choose **Change workspace…** in **GEKTOR** and select a project folder.
   In the folder browser, **Select folder** selects the folder currently displayed.
4. Check **Settings > Model connection**. Set the protocol, address and model,
   then choose **Apply**. GEKTOR does not install or supervise the model server.
5. Choose **Plan** in **Run**. Send a small request:

   > Read the README and find where this project saves settings. Explain the path without changing files.

6. Read the answer and open a tool result with `Ctrl+T`. Confirm that the cited
   files support it. If the project has no settings, the answer should say so.
7. For an edit, switch **Run > Act** and describe the change.
   Review permission requests, the resulting diff and any tests.

The conversation is saved. Use **Session > Open**, or `Ctrl+P`, to reopen it.
Opening a session restores its workspace and does not send a model request.
A session does not back up project files. Repeating a request can repeat changes;
check the current files before asking Agent to retry.

Press `Esc` to stop a turn. Completed changes remain. See
[Work with Agent](guide/agent.md) for follow-ups, retries and background processes.

## Run a workflow without a model

The bundled transform-and-review example doubles a list of numbers and pauses
for your approval. It needs no model, browser, network or additional runtime.

1. On Windows, open `start-tui-workflows.cmd`. On Linux, run
   `./gektor -tui -tui-screen workflows`.
2. Choose **GEKTOR > Change workspace…** and select
   `examples/workflows/transform-and-review` inside the extracted application folder.
3. Choose **Recipe > Open recipe…** (or `Ctrl+O`) and open `recipe.json`.
4. Press `V` to validate, keeping the default numbers. Validation checks the
   definition and input types; it does not execute the steps.
5. Press `R` to run with the defaults. Answer any permission requests.
6. The run pauses before **double**. Press `P` to review the step and approve it.
7. Select **double**, press `Ctrl+T` and open **Result**. The doubled values are
   `4, 6, 10`. Press `H` to find the saved run, or `D` to return to the recipe.

The recipe transforms values in memory and saves a run record outside the
workspace. It does not edit project files. Run it again with different numbers
to create a separate record; completed steps in an approved paused run are not repeated.

For CLI commands, see the [example instructions](../examples/workflows/transform-and-review/README.md).
Try [Log report](../examples/workflows/log-report/README.md) next to capture a file
and save an artifact. [Run and edit a workflow](guide/workflows.md) explains the editor.

## If you get stuck

Press `F1` for help. For connection failures, check the server's readiness,
protocol and address first. For a failed operation, inspect its error before
retrying. See [Troubleshooting](guide/troubleshooting.md) and
[Terminal controls](reference/terminal.md).
