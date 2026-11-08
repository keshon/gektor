# Delegate budgets

These two probes deliberately request delegation. They measure how much work a
child can complete at a fixed allowance, not whether delegation was necessary.
Probe 11 separately checks unnecessary delegation.

The review must identify the wrong lower-bound return without repairing it.
The repair must preserve the API, add tests, pass hidden boundary checks, and
record a successful command. Inspect the child trace to confirm that command
actually ran the tests; a successful unrelated command is not enough.

Run the same model and response budget with delegate allowances of 6, 12 and 24,
at least three repeats each, one job at a time. Each child uses the configured
default; the task forbids retries and parent repairs. The parent trace checks
do not include child calls, which are recorded separately in the delegate report.

Compare artifact checks and delegate completion together. An exhausted child
with correct files is still incomplete. Review prose checks are approximate;
inspect examples and confirm no files changed before accepting a review pass.
Report model identity, repeat count, exhaustion, steps, time and token usage.
Do not promote a limit based on a single run or a synthetic client.

## Ornith calibration, 2026-11-08

Ornith 1.5 35B Q5_K_M, llama-server, 131072 context, 4096 response tokens,
one job, three repeats per scenario and allowance. All 18 runs passed, with one
delegate call each and no exhaustion. Actual child steps:

| Allowance | Review | Repair |
|---|---|---|
| 6 | 2, 2, 2 | 5, 6, 6 |
| 12 | 4, 2, 2 | 6, 7, 7 |
| 24 | 2, 2, 2 | 5, 4, 9 |

Keep the existing 12-step default. Six left little room for the repairs; 24
showed no completion advantage on these tasks. The default maximum equals the
default allowance so a model cannot silently raise it. Operators can set a
larger maximum for other tasks. These results do not establish a suitable
budget for large coding changes or other models.

[Measurements](../baselines/delegation-ornith-2026-11-08.json) include child
timing, tokens, tool-result statuses and source/input digests. The working tree
changed between groups and builds/tests ran alongside them; timings are not a
controlled speed comparison. Inspection confirmed successful Go test commands
in the repair archives. The later all-attempt cap also passes for all 18 runs.
