# TUI ownership

Who writes UI state, how results cross goroutines, and what enforces the rules.
This uses the ownership approach from Melodix, applied to TARS rather than
copying its playback-specific rules.

## Enforcement

- **Compiler:** the forbidden operation cannot typecheck.
- **Checked:** a source-shape test rejects the operation.
- **Tested:** a behavioral test drives the relevant transition or interleaving.
- **Documented:** review is the only enforcement. This is a limitation, not a guarantee.

Run `go test -race ./...` and `go vet ./...`. Performance probes live in
`internal/tui/performance_test.go`; run them with
`go test ./internal/tui -run '^$' -bench . -benchmem`.

## Component boundaries

`internal/tui/components` contains concrete reusable models, not a widget framework:

- `form`: field/editor state, keyboard navigation, Apply/Cancel/Pick intents,
  compact form rows and action hit geometry. The caller supplies styles, labels,
  dimensions, visibility and choice display; it performs persistence.
- `composer`: textarea, bounded input history, draft recall and rune-based
  completion token/replacement operations. Command and file discovery stay in TARS.
- `selection`: selection and scrolling geometry, used by Sessions and Settings pickers.
- `transcript`: viewport, follow behavior and part anchoring across reflows.
  TARS retains role styling and the rendered-block cache.

The root model routes input to the active owner and translates component intents
into application actions. Components never receive a root model pointer or own
service lifetimes. They are updated on the same event loop; this split adds no
component goroutines. Exported Bubble Tea state is mutable by its owning adapter;
these are composition boundaries, not an ownership type system.

**Enforced by:** checked, `TestComponentsDoNotImportApplicationPackages`; tested
by the component packages and the existing overlay, focus, multiline, completion,
settings and layout integration suites.

Non-UI boundaries: `config.ApplySection` edits staged configuration;
`llm.DiscoverModels` owns HTTP model discovery; `sessionindex.Scan` reads and
indexes sessions without terminal dependencies. Search positions refer to source
messages. The TUI resolves them into rendered blocks on open, including reasoning
offsets (`TestHistoryMatchTranslationSkipsReasoningAndToolCards`).

`internal/tui/settings.Model` owns fields, editor and picker focus, categories,
keyboard and mouse navigation, rendering, and revision-checked catalog commands.
It receives dimensions and styles as props, and emits Apply, Cancel, SaveCopy
and SwitchCategory intents. It imports neither the root model nor app
services. Rendering performs no discovery and does not mutate choice caches.
`settings.Draft` restores edits across page switches and resolves changed sections
in stable order against a fresh `config.Profile.Clone()`. Failed validation
preserves both the draft and active profile. The parent handles application
actions, overlay placement and the active input owner.

Applying settings does not reconfigure the running session. `requestProfileChange`
stages the custom profile and the session and application preferences through
`filetxn.Replace`, reads the current task back from disk, and returns a
`ModeChange` carrying the new preferences, that history, the composer draft and
the session directory. `Run` surfaces it as an error; `cmd/agent` persists the
preferences, rebuilds argv and launches again. The next launch composes fresh
services from the new profile. Failed preparation writes nothing and queues no
restart; a commit failure restores already replaced files. If restoration itself
fails, the error names the retained original backup. This is recovery for
ordinary I/O errors, not a crash-atomic multi-file database; writers must be
serialized. The update loop owns the operation.

**Enforced by:** tested, `TestRefusedApplyLeavesFilesAndRuntimeAlone`,
`TestUnreadableTaskStopsApplyBeforeWriting`, and the file transaction suite.

Command launches construct one `app.Runtime` and defer its Close across frontend
handoffs. The TUI borrows the runtime. Direct TUI callers without a supplied
runtime receive an owned fallback. Close is idempotent. Worker closures capture
borrowed services before starting, rather than reading the mutable runtime from
another goroutine. Runtime lifecycle tests and the TUI race suite cover these
boundaries.

`app.Runtime` deliberately has no reconfigure method. Settings used to be applied
in place against a live session, which needed a candidate/reuse/persist dance and
had three separate implementations that drifted apart enough to give one setting
three different effects. A launch composes once now.

The architecture refactor was smoke-tested through a Windows pseudoterminal
using an isolated profile, session directory and deterministic local HTTP backend.
The checks exercised slash completion into Help, Ctrl+O and Shift+Enter multiline
submission, arrow navigation at draft boundaries, Settings category navigation
and Apply, persisted generation settings, session search/reopen, Details/Less,
workspace file completion, and clean quit. This verifies the
assembled console path without claiming real-model inference quality.

## 1. The update loop owns UI state

The model, widgets, transcript, selections, render cache, and session index are
written by the Bubble Tea update loop after construction. Session scan commands
capture root, revision, search mode, and the previous immutable cache. They
return a `sessionsLoadedMsg`; they do not receive the live model.

**Because:** a worker reading whichever workspace or popup is current at completion
can target a different session than the one that requested it. Sharing writable
widget state also introduces races.

**Enforced by:** tested, `TestSessionCommandDoesNotReadLiveModel` and the race suite.
The unexported message types constrain the boundary, but Go does not prevent a
future command closure from capturing a model pointer.

The permission grant map is the deliberate exception: the gate callback reads
and writes it from the run goroutine under `alwaysMu`. No I/O occurs under that
lock. The input adapter owns its console buffer; it does not own UI state.

## 2. A scan can replace only the revision that requested it

`acceptSessions` requires both the current revision and workspace root to match.
Scanning and history decoding run in commands. Unchanged histories reuse the
previous cache; searchable text is built when Search requests it.

**Because:** a slow old scan must not restore deleted sessions, undo a rename,
or replace a newer workspace's list. Scanning every history in Update used to
block input and repainting.

**Enforced by:** tested, `TestSessionScanRunsOutsideUpdateAndRejectsStaleResults`
and `TestSessionIndexCachesUnchangedHistoryAndLoadsSearchOnDemand`.

## 3. The active owner controls all input

`activeInputOwner` determines which zone owns input from run state, overlays,
and pane selection. Widget focus flags are synchronized from that answer;
they are not the authority. Clipboard messages and the native caret use the
same owner as keyboard routing.

Within Settings, the form resolves a second, local owner: field editor,
picker search, or no text input for categories and action buttons. Opening a
picker blurs the underlying editor. Paste updates picker filtering through
the same input path as typing.

**Because:** finishing a run while Help was open used to focus the hidden
composer, allowing paste to modify the draft behind the modal. Search also had
separate key and paste paths, and only keys recomputed results.

**Enforced by:** tested, `TestCompletionAndPasteRespectHelpOwner`,
`TestPastedSearchRecomputesResults`, and
`TestCompletionClosesCancelledPermissionOverlay`, and
`TestHelpRestoresNavigatorOwner`, and `TestSettingsPasteRespectsNestedFocus`.

## 4. Rendering observes state

View helpers may normalize local copies for safe geometry, but do not change
selection, focus, or scroll fields on the model. Updates and resize handlers
own normalization. Settled transcript blocks are rendered and cached on the
update loop; append batches paint once after the event's changes are complete.

**Because:** rendering a frame should not decide what the next key means.
Re-rendering all settled blocks on each streamed update also wasted work.

**Enforced by:** tested, `TestViewDoesNotChangeSelectionOrFocus` and
`TestTranscriptCacheInvalidatesChangedBlocksAndGeometry`.
**Checked by:** `TestRenderHelpersDoNotAssignModelFields` rejects direct model
assignments in render helpers. It does not follow calls into other helpers.
Cache keys include the block value, viewport width, and detail mode. The palette
is static; introducing runtime themes must add palette invalidation.

## 5. Display metadata does not invent history

Reopening a session restores a saved-history context estimate without resetting
it afterward. Previous backend usage stays separate and names its source.
Stored messages have no timestamp field, so their display timestamps remain
absent; live messages are stamped when received.

**Because:** session opening previously erased the estimate it had just computed
and stamped old messages with the reopening time.

**Enforced by:** tested, `TestOpeningSessionRetainsContextEstimate`,
`TestLoadedHistoryDoesNotInventTimestamps`, and the context persistence tests.

## 6. Attachment extraction preserves the user's text

Recognized image references are removed by source span. The remaining prompt
keeps internal whitespace and indentation. Text references append bounded file
snapshots without rewriting the original prompt.

**Because:** reconstructing token strings with spaces collapsed multiline code
when an image was attached, while the transcript still showed the original.

**Enforced by:** tested, `TestImageReferencePreservesMultilinePrompt`, plus
attachment size, binary-text, and workspace-boundary tests.

## 7. The palette has one home

Color literals belong in `styles.go`; components use named palette values.

**Because:** scattered colors drift as components evolve.

**Enforced by:** checked, `TestPaletteLivesInStyles`.

## Limits

- Race tests cover driven interleavings; Go does not provide an ownership type system.
- View tests cover application selection and focus, not every internal field in
  third-party widgets. They are behavioral tests, not a static proof of purity.
- File signatures use modification time and size. An external edit preserving
  both can evade cache invalidation; TARS writes snapshots atomically.
- Single-session reads, attachment reads, mutation writes, and pre-run checkpoint
  creation remain synchronous. The expensive all-session scans are asynchronous.
- Terminal-cell table alignment is tested. The lightweight Markdown parser still
  does not implement escaped table pipes or nested formatting.

Add an ownership rule only with an enforcement mechanism and a test that fails
when the original defect is reintroduced.
