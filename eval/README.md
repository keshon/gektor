# Evals

Frozen scenarios for a weak local model, scored by a program.

- `prompts/` — the write-up for each scenario: what it stresses, what counts as
  a pass, what counts as a failure. Written for a human.
- `probes/` — the machine-checkable form of those criteria.
- `fixtures/` — the seed workspace probes start from. Copied per run, never
  written to.
- `results/` — output, gitignored.

## Running

For the fixed seven-scenario starting baseline:

```text
eval-run.cmd -suite core -runs 1 -jobs 1 -require-model Cyber-Tiel
eval-run.cmd -suite core -runs 3 -jobs 1 -require-model Cyber-Tiel
```

On other platforms use `go run ./cmd/eval -suite core` with explicit backend
flags. One pass is a calibration, not a reliability estimate; use at least
three repeats before interpreting model variance. Keep weights/quantization,
server configuration, response budget, suite inputs and machine load fixed.
With `-jobs 1`, scenarios and repeats execute in declared order.

Core covers an exact patch, search/edit, managed HTTP server lifecycle,
overflow recovery, truthful reporting without browser capability, nested skills,
and a small Go CLI repair. The last scenario uses trusted acceptance tests
injected only after the model finishes; their contents enter the input digest.
Browser absence is a deliberate capability test, not a completed UI task.
Connected-browser mechanics are separately checked by the opt-in browser test;
this core suite does not measure visual design quality.

Every run directory also contains `summary.json`: per-scenario and overall
pass counts, excluded errors/skips, median time/steps, tool failures, clarification
calls and token counts. Cached tokens are reported only as supplied by the
backend; zero does not establish that caching was disabled. Read `meta.json`
and raw traces before comparing summaries. A pass proves declared criteria,
not general agent correctness or the truth of unrestricted prose.
Tool failures count all failed tool results, including deliberate negative-input
checks; inspect the trace before treating them as wasted attempts.

```bash
go run ./cmd/eval -dry                          # validate probes, no model calls
go run ./cmd/eval                               # everything, once each
go run ./cmd/eval -tier signal -runs 3          # signal tier, three runs each
go run ./cmd/eval -only 05 -runs 3              # probe 05, three runs
go run ./cmd/eval -tier smoke                  # quick smoke subset
go run ./cmd/eval -require-model qwen           # refuse to start on other weights
```

Each invocation writes `eval/results/<timestamp>/` by default, containing `results.jsonl`, `meta.json`
recording which model produced the numbers, and a full request/response trace
per run.

`meta.json` also records requested response budget, run count, sampler setting,
concurrency and runtime platform. SHA-256 digests identify the running binary,
its embedded prompt files, and the selected probe definitions plus seed-file
contents (including skills inside those seeds). Repository guides are not
automatically copied into eval workspaces. A source-checkout revision and dirty
flag are recorded when Git is available; they describe the checkout, while the
binary digest identifies what actually ran. Metadata write failures stop the
evaluation before solver calls. These fields identify experiments; they do not
enable result caching or guarantee deterministic model sampling.

On Windows use `eval-run.cmd` instead of `go run` directly. It reads `.env` and
prepends `-backend-kind llama -backend http://$LLAMA_HOST:$LLAMA_PORT
-context-limit $LLAMA_CTX_SIZE`, so the suite targets the same server
`run-server-llama.cmd` starts. A `-backend` or `-backend-kind` on the command
line overrides those, since Go's flag package takes the last occurrence. Called
directly, `cmd/eval` still defaults to koboldcpp on `localhost:5001`.

`-only` is a substring match on the probe filename, not a numeric range:
`-only 0` selects 01–09, while `-only 10` selects 10. `-dry` validates the
whole directory even if `-only` is set; `-tier` filters its printed list.
Run from the repository root because probe/seed paths are repository-relative.
Automated runs copy fixtures, create requested Git seeds, and supply scripted
replies; prefer them over editing the checked-in fixture manually.

The runner accepts backend/auth/context flags like the agent. `-jobs` defaults
to 1; exclusive probes hold their shared resource alone. The runner locks its
output directory against another invocation; `-force` bypasses that lock only
when you deliberately accept overlapping runs. `-out` changes the result root.
`-probes` selects another probe directory, `-max-tokens` sets the response
budget, and `-dry-sampler` enables DRY for a controlled comparison. Exact flags
are available with `go run ./cmd/eval -h`.

`-runs` matters. A weak model is stochastic, and a single pass is close to no
evidence. Step counts are recorded for every run whether or not the probe
scores them — the difference between passing in 4 steps and passing in 13 is
usually the thing worth reading.

## How a probe is scored

Trace `tool` values accept exact names or globs (`start_*` covers both managed
launchers). Optional `success: true` requires a completed, nonfailed tool result;
`output_regex` checks the observed output. A successful diagnostic tool call is
not necessarily a successful operation: for example, require an HTTP 200 in
`check_url` output rather than counting a connection-error report as server proof.

Three independent verdicts, all of which must hold:

| Field | Asserts |
|---|---|
| `verify` | what the workspace contains afterwards, using the evaluator’s Check type |
| `trace` | which tools were called, with `mode`, `args_regex` and `max_calls` |
| `answer` | what the run reported, by regex |

Plus `max_steps`, but only where the write-up states a limit.

A run that never reached the model is reported as `ERR` and excluded from the
rate. A run the harness had to cut off is a failure, however the workspace
happens to look.

## Adding a probe

Write the scenario in `prompts/` first, then mechanize it. Criteria come from
the write-up — a threshold invented while writing the JSON is a number that
will fail someday for no stated reason.

```json
{
  "prompt": "eval/prompts/07-fake-save.md",
  "task": "create a new file notes.txt containing the text eval-ok",
  "seed": "eval/fixtures",
  "verify": [{"type": "content_contains", "path": "notes.txt", "contains": "eval-ok"}],
  "trace": [{"tool": "write_file", "mode": "required", "why": "text in a reply saves nothing"}]
}
```

`why` is quoted in the failure, so a red row explains itself without opening
the write-up. The same Interactive role as the CLI executes every scenario.

## Coverage

The scenarios are mechanized. Probes 01 and 02 request a temporary Git
seed with a fixed commit. Probe 12 supplies a scripted clarification and
requires an improvement note grounded in the fixture. `git` and `replies`
configure these harness inputs.

## Vacuity audit

Every probe must be able to fail — a probe that passes on an empty run
measures nothing. Rules are enforced two ways: `loadProbes` rejects a
probe with nothing asserted, and the table below records why each probe
cannot pass vacuously (criteria are maintained with the probe write-ups).

| Probe | Why it cannot pass on an empty run |
|---|---|
| 01, 02 | `git log` required and the answer must identify the seeded commit |
| 12 | `ask_user` required and a new note must mention the real fixture |
| 03 | `list_files` required — no calls, no pass |
| 04 | `goodbye.txt` content + `hello.txt` absence both require action; `move_file` required |
| 05 | `v2` symbol must appear; `Greet()` presence pins the rest of the file |
| 06 | `REPLACED` must appear; `read_file` required |
| 07 | `notes.txt` is a new file; `write_file` required |
| 08 | `read_file` required; mutating tools forbidden |
| 09 | `return "hi"` must appear; `grep_files` required |
| 10 | answer regex on the real size; `read_file` required |
| 11 | `Bye()` symbols must appear; `delegate_task` forbidden |
| 13 | `start_background` + `check_url` required |
| 14 | three new files with contents; `write_file` required |
| 15, 17 | new files with distinctive symbols |
| 16 | shell runs the script and matches `42`; `oses: [windows]` (see below) |
| 18 | answer + `read_file` required after surviving the injected overflow |

Platform-bound probes declare `"oses"` (`windows`/`linux`/`darwin`) and
report SKIP elsewhere — a skip is excluded from the rate like ERR. Today
that is 16 (`findstr` pipeline). Probe 04 used to shell out to
`if exist hello.txt`; it now uses the portable `file_absent` check type
instead. Prefer portable checks over `oses`: a skip is honest, but every
skip is a configuration the suite never exercises.
