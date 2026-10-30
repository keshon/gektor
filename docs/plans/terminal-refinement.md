# Terminal refinement

Branch: `codex/gektor-terminal-refinement`.

## Agreed constraints

- Keep the top menu and bottom hotkey row (Midnight Commander layout).
- USER, ASSISTANT and SYSTEM are the visible sources. SYSTEM covers all
  scaffolding output, including local status. Preserve internal delivery
  metadata: local notices must never become model-facing messages by styling.
- View controls speaker names independently of role labels. Names may be hidden;
  compact rendering must still distinguish user, assistant and system output.
  Use a shaded user prompt and a left rail for system blocks.
- Try minimal framing first, with the existing connected outline available.
  Frame choice is independent of theme, density and source-name visibility.
- Presentation preferences persist globally and never alter model context.
- No workspace backup, rollback or implicit restoration on resume.

## Implementation

1. Appearance and attribution: persist minimal/outlined framing, tool density
   and speaker-name visibility; expose them in View. Keep existing role-label
   and SYSTEM visibility controls, detailed output and raw tool arguments.
2. Prefill: reuse the live input-status area for a transient bar and processed /
   total token counts from real backend telemetry. Never invent a percentage
   from elapsed time or estimated counts. Retain a fallback waiting indicator.
   These are local SYSTEM status, not archived/model-facing prompt text.
3. Outcome receipt: restyle the existing outcome/files/checks/resume event.
   Preserve failed, cancelled and unavailable verification states. Successful
   ordinary tools are not final verification. Keep existing execution semantics.
4. Approval evidence: offer a complete operator-only diff or new-file view,
   independently of the bounded 40-line preview/model prompt. Preserve reachable
   gate choices and allow/deny behavior. Detect stale source content and require
   a refreshed approval rather than applying a stale preview.
5. Guide-only dedup: suppress a new duplicate body only when complete matching
   guide content still exists in the actual working projection after output
   limiting. Key by resolved source and digest; do not mutate older history.
   Compaction, resume, truncation, edited/overridden sources, failures and
   concurrent loads must not produce false loaded states.
6. Extend existing real Go-renderer tests for these changes. Do not introduce
   another transcript, renderer or testing framework.

## Acceptance

- Both frames keep menu, hotkeys, gates, scrolling and dialogs usable on resize.
- Names and role labels can be toggled independently. SYSTEM remains identifiable
  with names hidden; changing display never changes saved/model messages.
- Known prefill progress shows a bounded bar/counts. Unknown/estimated telemetry
  never claims a measured percentage. Progress is removed when prefill ends.
- Existing outcome receipts retain exact failure/unverified information.
- Full approval inspection includes new files and content past preview line 40;
  it does not append extra model context, execute a tool or approve anything.
- Dedup is based on delivered content, not just a success/header/digest. A guide
  larger than the per-call result budget must not be marked fully loaded.
- Run focused tests during development, then `go test ./...`, `go test -race
  ./...`, `go build ./...`, and `go vet ./...`. Export real TUI frames for visual
  inspection; distinguish automated coverage from manual Windows Terminal checks.

## Deferred

Skill installation/seeding, reference-resource dedup, extra permanent activity
strips, PTY/ConPTY, agent managers, filesystem checkpoints, inference-server
changes and any renderer rewrite.

## Progress

- [x] Save accepted plan and create separate branch.
- [x] Appearance, attribution and View controls.
- [x] Transient prefill display.
- [x] Existing receipt presentation.
- [x] Full and current approval evidence.
- [x] Correctness-gated guide dedup.
- [x] Verification and documentation.

## Implementation notes and validation

Guide dedup runs on each outgoing request after output limiting and compaction.
It preserves original tool results in the archive and restores a retained full
body automatically when an earlier matching copy leaves the projection.

Full approval evidence uses private TUI events. File fingerprints detect changes
while approval is pending; this is not a filesystem lock between approval and
execution. Unsupported/unreadable proposals retain the existing bounded prompt.

Passed `go test ./...`, `go test -race ./...`, `go build ./...`, `go vet ./...`
and `git diff --check`. Renderer exports cover both frames at 120×34, 80×24,
40×18 and 20×10, plus pending approval and full proposal dialogs. Inspected
the exported terminal text. Manual Windows Terminal interaction and live model
testing were not performed in this change.
