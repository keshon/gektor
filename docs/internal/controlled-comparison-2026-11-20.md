# Controlled comparison preparation

This is the original preparation record. The server was subsequently restored
and GEKTOR profile runs were recorded in the
[remediation record](audit-remediation-2026-11-20.md#continuation-acceptance-record).
The user then dropped competitor testing and returned work to the TUI. The
three-product plan below remains unexecuted and is no longer an active gate
for this continuation.

This protocol prepares experiments; it reports no measured product improvement.
The [preflight manifest](remediation-2026-11-20/comparison-preflight.json) records
an unavailable backend on 2026-11-20 and 36 **unexecuted** planned runs. Both
`/health` and `/v1/models` refused connections at `127.0.0.1:9931`. GEKTOR started
this pass at `57a7108`; its dirty state in the manifest includes concurrent
remediation edits. Pi and OpenCode remain at the revisions in the original audit.

From the repository root:

```powershell
pwsh -NoProfile -File scripts/prepare-comparison.ps1 -OutDir eval/results/comparison-preflight-new
```

Use a fresh output directory each time. The script reads all three checkout
revisions and dirty states, makes one bounded health request, freezes input
hashes and exports tasks and artifact acceptance checks. It neither invokes a
solver nor copies hidden acceptance tests into a solver workspace. The four
initial portable tasks cover exact editing, ambiguous editing, nested project
instructions and Go repair. Three repeats rotate GEKTOR/Pi/OpenCode order to
reduce systematic order effects. Rotation does not control cache state itself.

Before scoring, fill the manifest's required settings from observed requests
and server configuration: exact weights/hash and quantization, server arguments,
context, response budget, sampler, reasoning and cache policy. Verify the same
settings in all three clients, with one active request and no overlapping builds
or tests. Record unsupported settings as a comparison limitation and withhold
latency rankings if effective settings differ. Identify executable hashes as
well as checkout revisions; installed global executables do not prove checkout
execution. Keep each task and repeat in a fresh workspace, retaining its final
artifacts and stdout/stderr. Run trusted acceptance after the solver stops.

The manifest exports GEKTOR's artifact criteria verbatim. Its tool-name trace
criteria remain GEKTOR-only: e.g. `patch_file`, `read_file`, and `run_*` do not
have identical names or batching semantics in Pi/OpenCode. Define and review
semantic trace adapters before scoring interaction efficiency across products.
Do not count a missing adapter as a competitor failure. The ledger acceptance
file must remain unavailable until the agent finishes; its hash is included.

Pi connection and invocation contracts are in the pinned checkout's
`packages/coding-agent/docs/models.md`, `cli.md`, and `cli-integration.md`.
The CLI supports print/JSON mode and an explicit `--tools` selection; configure
an isolated agent directory rather than changing the user's profile. OpenCode's
pinned `packages/web/src/content/docs/providers.mdx` documents the compatible
provider and `baseURL`; `cli.mdx` documents `opencode run --model provider/model`.
These are preparation source trails, not successfully exercised launch adapters.

For GEKTOR profile experiments, compare baseline against one change at a time:
optional-tool exposure first, then a separately defined context policy. Preserve
permissions, capability discovery and the same task acceptance. The existing
`-context-limit` flag changes the effective context constraint; it is not by
itself proof of a smaller prompt. Keep identical effective server context and
record prompt section/tool estimates plus actual request tokens. Do not silently
remove required tools or use different acceptance rules for a reduced profile.

For each run retain task outcome, independent artifact/answer/trace verdicts,
wall time, first useful action, request and cache counts, steps, failed tool
results, recovery attempts and raw request/response traces. Record unsupported
metrics as unavailable. Summarize all attempts and infrastructure errors;
exclude errors/skips from solved fractions while reporting their counts.
Three repeats per case are a minimum calibration, not a strong reliability claim.

This initial fixture subset does not complete the acceptance gate. Add equivalent
cancellation/resume, unavailable-tool and long-context fixtures and acceptance
adapters before a full three-product claim. No solver was run in this pass;
smaller-profile benefits, solve-rate comparisons and distribution readiness
remain unproved.
