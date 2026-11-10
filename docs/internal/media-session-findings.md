# Media session and lazy-tool findings

Status: completed on 2026-11-10. The media decision-eval and guidance iteration
is implemented, validated and accepted for delivery. No implementation or
validation task remains open within this iteration.

Delivered: three isolated decision probes, whole-answer JSON Schema scoring
with duplicate-key rejection, calibrated time allowances, and optional media
guidance covering source selection, source/output formats and evidence-qualified
claims. The latest Tiel runs passed all three probes individually; relevant Go
tests, vet, dry validation and the startup-prompt budget check passed.

The sections below retain the investigation history, including superseded
proposals and failed experiments. Their pending-work language describes the
state at that time. Residual explanatory overstatements are accepted model
limitations outside this iteration's implementation scope, not release blockers
or unfinished feature work. This completion does not claim universal model
reliability. See [the final acceptance record](#final-acceptance-record).

Recorded on 2026-11-09. Source session: `12cebbcc`, using Ornith 1.5
35B-A3B Q5_K_M through llama-server on port 9931, with a 131072-token window.

This section records the initial audit. The 2026-11-10 follow-up below records
the implemented dispatcher and request-attribution changes and their validation.

Current operation behavior belongs in the [operation reference](../reference/operations.md#video-and-audio).
Package boundaries belong in the [capability guide](../development/capabilities.md).

## Why activation caused a long prefill

The request immediately after `enable_capability(native_media)` spent about
23.2 seconds in prefill. Later requests generally spent 0.8–4.1 seconds there.
The saved session reports no compaction or summary, and retains its conversation.
This was reprocessing, not lost history.

Activation adds nine tool schemas. `Registry.Defs` sorts tools alphabetically.
The running Ornith template puts those definitions before both the system text
and the conversation. Adding tools changes the prefix before the history, so
much of that history cannot reuse its previous prefix cache.

The old diagnostics retained phase timing but not cached-token counts. Timing
and the template explain the spike; they do not establish its exact cache-miss
count. Diagnostics now retain prompt, cached and processed counts, the
`cache_known` and `estimated` flags, and final usage. Prefill sampling groups
1024 tokens; generation records phase boundaries rather than every token.

## A cache-preserving alternative

A read-only experiment used the running server's `/apply-template` and
`/tokenize` endpoints, the saved pre-activation conversation, a synthetic
two-tool baseline and the actual nine media schemas. No inference was run.

| Change | Earlier prompt tokens preserved |
|---|---:|
| Add schemas to the alphabetically sorted tool list | 92 of 5419 |
| Append schemas to the existing leading tool list | 134 of 5419 |
| Keep a fixed dispatcher and append schemas in the activation result | 5251 of 5251 |

The dispatcher baseline differs because its tool definition differs. The media
schemas cost 2051 tokens; this approach preserves earlier tokens rather than
removing the schemas' own cost. These are rendering measurements, not measured
latency savings or evidence of reliable model use.

The proposed design keeps eager tools directly callable and exposes one fixed
`invoke_tool` definition for optional tools. Activation returns their schemas
and instructions as conversation content. The model supplies a target name and
an arguments object to the dispatcher. Its definition must not change when a
pack activates.

Keep each catalog where it was appended. Moving it toward the beginning changes
the prefix again. After compaction, restore missing catalogs from saved
`session.Capability` state before estimating and fitting the next request. Do
not rely on a generated summary to retain exact schemas. Catalog presence needs
trusted metadata, not a substring match against arbitrary message text.

The dispatcher must resolve the target before ordinary validation, Plan-mode
admission, permissions, resource checks, scheduling and execution. Calling a
target directly from a wrapper would bypass those boundaries. Execution,
attachments and mutation evidence must identify the actual target; model
history must retain the original dispatcher call unchanged. Delegates keep
their own catalogs and restrictions.

The tradeoff is weaker generation-time argument constraints: the model sees a
fixed envelope, while GEKTOR validates the target's full schema afterward.
Before adopting this design, measure rejected arguments and real cache counts
with media, workflow and browser evals. Cover denied targets, Plan-mode writes,
compaction, resume and delegate restrictions. Stay within the startup prompt
budget. Merely changing alphabetical ordering is insufficient.

## What happened during the download

The native download returned HTTP 503. The agent explicitly selected yt-dlp
afterward, as intended. The failed native request did not silently switch
providers. The session does not establish a permanent native-provider failure.

Our yt-dlp implementation forced merged output to MKV, then published it under
the requested `.mp4` filename. The agent noticed the mismatch and inspected it.
It then treated `media_convert` as a remux, although that operation re-encoded
AV1/Opus into H.264/AAC. The file grew from 23815673 to 43710912 bytes and received
another encoding generation. The agent removed the misnamed intermediate.

Independent inspection confirmed that the final file was MP4, 1280 by 720,
H.264/AAC, and 255.303 seconds long. The task took about 214 seconds.

Implemented corrections:

- Native and yt-dlp merged downloads use the path's MP4, MKV or WebM container
  and copy streams without re-encoding. Incompatible codecs fail explicitly.
- Single-stream selection respects the requested extension. Publication rejects
  a container/extension mismatch and does not silently change the destination.
- Local output extensions must match the requested format.
- `media_convert` clearly describes re-encoding. Optional `copy: true` remuxes
  the first video and audio streams into MP4, MKV or WebM without re-encoding.

## Assistant text became a supposed user request

Near the end, the assistant wrote `Скачай видео «Цой - пачка.sigare»` alongside
an unnecessary program-discovery call. On its next step, it described that text
as a repeated user request. The archive records it as assistant text. No such
new user message exists, and the request was not compacted.

The replay harness preserves message roles and resumes without adding a new
user request. Its initial Act notice stays before the recorded continuation,
matching an already-started run. A regression test checks those boundaries.

Early model experiments replayed a completed media inspection. The final probe
uses a report file present in its seeded workspace and an eager `read_file`
result, avoiding a dependency on an unactivated media catalog. These fixture
revisions are separate experiments, not repeated measurements of one scenario.

The final 2026-11-09 `45-assistant-role` run still failed. Despite a user request to review
the report without commands or network access, Ornith tried to use yt-dlp in
response to its own imperative text. The run timed out after 60.1 seconds and
four model steps, with two tool failures. A short system-text clarification
added four Ornith tokens and kept the startup budget check passing; it did not
establish a reliable fix. Do not describe this behavior as resolved.

Local evidence is in `eval/results/20261109-222040`. Earlier experimental runs
are `20261109-221007`, `20261109-221317` and `20261109-221651`. These ignored local
traces are not distributed with the repository. The committed probe, seed and
criteria make the final experiment reproducible.

## Validation and remaining work

Full Go tests, build and vet passed. Targeted media/run race checks passed.
Installed FFmpeg and ffprobe passed local conversion, remux, inspection, clip,
frame and merge checks. Deterministic tests cover both merged-download
providers, container selection and refusal to publish mismatched outputs.

`44-media-download` is added and dry-validated. Its post-fix, model-backed
YouTube download has not run. Earlier status, caption and workflow eval passes
do not cover that new scenario or establish reliability after these changes.

At the end of the initial audit, the download probe, role-attribution fix and
dispatcher implementation were still pending.

## Follow-up: 2026-11-10

### Stable optional-tool handling

The dispatcher is implemented. Eager tools stay in the leading tool list;
optional schemas arrive in their activation result. `invoke_tool` resolves to
the registered target before validation, permissions, read-only admission,
scheduling, attachments, output limiting and mutation tracking. Observers see
the effective target; saved model calls retain the original envelope.

Trusted catalog markers and saved fingerprints restore missing schemas after
compaction or resume. Budget estimates include restored catalogs. Inactive and
recursive targets are rejected. Plan-mode restrictions apply to the target,
including read-only extensions; the dispatcher itself cannot grant write access.
Legacy registries without a dispatcher retain direct exposure.

The startup catalog map now uses at most 700 characters and 48-character
summaries. Full descriptions remain available through discovery. The existing
5000-token startup estimate check passes without raising its limit.

Live evidence from `eval/results/20261110-085106`:

- All five leading tool arrays in the media-status run were identical.
- The request before activation contained 7317 prompt tokens.
- The request after activation reused 7177 cached tokens and processed 1963
  new tokens, including the newly appended catalog. Prompt processing took
  about 4.9 seconds. It did not reprocess the earlier conversation in full.
- All four media probes passed: status, caption paging, workflow conversion
  and the newly added merged-download case. These runs are calibration,
  not a controlled latency comparison with the old implementation.

The download case encountered a native CDN TLS handshake timeout and explicitly
selected yt-dlp. It produced a real MP4 with AV1 video and Opus audio at 720p,
23810359 bytes, and 255.321 seconds. No conversion was needed. The model called
all media operations through the dispatcher; ffprobe checked the actual file.

### Request identity

Explicit content labels for assistant and tool messages did not fix the local
model failure and were removed. A transient footer now repeats the latest real
user request, the host's user-message count and the current run directive.
The request comes from saved archive roles and metadata, not assistant prose
or a generated summary. Tool images are marked as harness input and excluded.

The footer is used by the llama-server serializer after assistant turns. It
copies at most 2048 Unicode characters of the request and explicitly identifies
an excerpt when shortened. Earlier complete instructions remain in history.
The current Plan/Act directive still applies. The footer is not persisted as
another user message; only this short suffix is replaced on later requests.
Native provider replay is unchanged. Context estimates reserve its bounded cost.

A Russian answer matcher also rejected a correct response containing Markdown
emphasis and “1 реальный пользовательский запрос”. The matcher now handles that
wording and Unicode letters. The required count remains one; the forbidden tool
checks are unchanged. Do not count the initial scorer rejection as a model
attribution failure or silently rewrite its saved result.

The text-only reminder reduced the failure but did not eliminate it: one run
still counted its own imperative as a second request. After adding the host's
actual count, both targeted runs passed, in 23.5 and 34.3 seconds, with one model
step and no tool failures (`eval/results/20261110-084831`). Final checks with
current-mode metadata also passed twice, in 10.3 and 10.4 seconds, with no
commands, network access or tool failures (`eval/results/20261110-090150`). An
intermediate correctly counted answer was rejected by the matcher when the
number followed a Markdown heading; scorer regression tests now cover both
valid formatting and rejection of counts 2, 11 and 21. Previous results remain
unchanged. Repeated model-backed
checks remain necessary; this does not guarantee correct attribution for every
model or conversation.

Full Go tests, vet and targeted Agent, Chat Completions and tools race checks
passed. Regression tests cover denied and malformed targets, Plan-mode writes,
concurrent reads, image grants, trusted catalog restoration, archive identity,
current mode and unchanged wire history before the transient footer.


## Session 106db964 follow-up (2026-11-10)

Reviewed the saved history and diagnostics from a music download followed by
recommendations. These are observations from this session, not a model-wide
success rate.

The dispatcher was used throughout optional media calls. Activation retained
the earlier prompt prefix: request 3 had 7,834 prompt tokens; request 4, after
activation, had 9,102 with 7,729 cached. Later requests also reused most of their
prefix. There is no full-context restart at activation in this trace. Corrected
user input cancelled an unstarted video-info call before execution.

The first result was the wrong source, not a truncated download. The agent
selected an upload by Maggie Jaliskilla whose metadata explicitly reported
61 seconds. A second candidate was also 60 seconds. It nevertheless downloaded
and converted the first, reporting completion. After the user complained, it
searched for the official title, selected Harry Styles' official audio, and
verified the resulting MP3 with ffprobe: 341.916735 seconds, 5,471,013 bytes.
Duration verifies length; it does not establish by listening that the music
starts at the expected point.

Additional friction:

- The agent requested an MP3 directly from native audio downloading. Native
  streams did not match; yt-dlp also failed that request. Native M4A followed by
  FFmpeg conversion worked. Separate source stream selection from conversion
  when an output codec is not offered by the source.
- Two native downloads hit a CDN TLS handshake timeout. Explicit retries
  succeeded. This trace does not establish the cause or justify hidden backend
  fallback.
- An unnecessary host-program scan preceded the more useful media_status.
- Cleanup used a compound PowerShell command with a malformed calculated
  property. Parsing failed, so its intermediate-file deletion did not run.
  The agent recovered with simpler commands but retained the M4A files.
- Recommendation links were found using search. The final claim that all were
  verified official uploads exceeded the evidence; no video_info checks were
  made on those recommendations. Do not equate search titles with verification.

These observations led to the completed decision-eval and optional media-guidance
iteration recorded below: source selection, source-stream versus output format,
and evidence-qualified link claims. Fixed evidence isolates those decisions;
live downloads remain separate network probes. The base prompt was unchanged.

## Offline decision baseline (2026-11-10)

Added probes `46-media-source-selection`, `47-media-audio-format` and
`48-media-claim-evidence` to the media suite, with an offline-only
`media-decisions` subset. Synthetic saved metadata exercises the decisions
without YouTube or search availability. The Agent activates the shipped media
guidance, reads fixtures and returns a structured decision; no media action is
executed. Criteria and limits are in [the probe write-up](../../eval/prompts/media-decisions.md).

Baseline: Cyber-Tiel-Coder-35B-A3B UD-Q4_K_M, llama-server on port 9931,
98,304-token context, 8,192 response tokens, default sampler, one job and three
runs per probe. Results: `eval/results/20261110-104327/`. These ignored local
results include full traces, fingerprints and a snapshot of the original probe
definitions in `probes/`; they are not distributed with the repository.

- Source selection: 2/3 scored passes. All three selected the intended studio
  candidate; one failed because it called `media_status` during saved-evidence
  review. The guidance's unconditional status instruction is a candidate cause,
  not a demonstrated causal result. The original wrong-source choice was not
  reproduced by this simpler case.
- Audio planning: 0/3 under the initial task. Run 1 timed out at 180 seconds
  before answering. Run 2 returned `download_path: song.mp3` despite describing
  an M4A download followed by conversion. Both completed answers omitted final
  inspection. Run 3 otherwise had the correct M4A/AAC-to-MP3 plan.
- Search claim discipline: 3/3 scored passes; all declined to label search
  candidates verified official uploads. This constrained task did not reproduce
  the original recommendation overclaim.

The initial audio task did not explicitly request final inspection although
the scorer required it. Its 0/3 must not be presented as three wrong codec
decisions. The task now explicitly requests inspection; the assertions,
fixtures, runtime instructions, model and budgets remain unchanged. Fresh
repetitions are recorded separately rather than replacing the initial results.

Manual review found a remaining scoring gap: source-selection answers promoted
the saved uploader name into official ownership; two called it a verified artist
or verified artist channel. No channel-verification evidence was provided.
Probe 46 checks selection and rationale, while probe 48 checks a structured
verification decision on search results. Neither establishes truthfulness across
all source-selection prose. Metadata inspection alone also does not prove channel
ownership. These observations motivated the typed ownership decision and scoring
corrections implemented below. External identity verification and grading all
unrestricted prose are outside this iteration's accepted scope.

These results support a focused review of optional media guidance: when status
is necessary, source container versus converted output, and the evidence needed
for official-source claims. They do not establish a prompt fix or justify an
engine redesign. No runtime instructions changed during this baseline, and no
live-network media probes were run as part of it.

### Explicit-inspection repetitions

The clarified audio probe ran three times under the same model, response budget,
sampler and 180-second timeout. All three were incomplete: two timed out while
waiting for the third model response; the last returned no completed model step.
Results: `eval/results/20261110-110331/`. There is no completed format decision
to score semantically in this batch. The original path mismatch remains the
concrete completed-answer failure; these timeouts do not reproduce it.

A host observation at the end found about 593 MiB of free physical memory on a
machine with about 31.9 GiB usable RAM. Memory pressure is a possible latency
confound, not a proven cause of the timeouts. Local Go validation also overlapped
parts of the experiments. Do not interpret the elapsed times as a controlled
model-performance comparison or relax criteria to hide the failures. This led to
the 65k calibration and individually inspected Tiel runs recorded below.

Validation: evaluator and conventions tests, evaluator vet and the 44-probe dry
validation passed. Scorer regressions reject short/live source substitutions,
direct-MP3 download paths, stream-copy conversion, unsupported verification and
execution during offline review. At this baseline, runtime instructions were
unchanged and no commit, push or live-network probe had been performed.

## 65k context and bounded tuning (2026-11-10)

The relaunched server reports the same Cyber-Tiel-Coder-35B-A3B UD-Q4_K_M
weights with a 65,536-token context. A calibration of the clarified audio task
passed in 163.9 seconds, three model steps, with no tool failures. Results:
`eval/results/20261110-111832/`. It used the earlier regex-based scoring and
is not directly comparable with the stricter baseline below.

### Evaluator corrections

Optional `answer_schema` now validates the whole final answer using the existing
operation JSON Schema validator. It rejects extra prose, Markdown fences,
missing fields and wrong typed decisions. A token-level JSON check rejects
duplicate keys before validation; otherwise the existing JSON parser retained
only the last contradictory value. Schemas compile during dry validation.
Existing prose probes keep their regex behavior unless they opt into a schema.

The three media-decision probes use schemas. The source probe now asks for
`verified_official` separately from the preferred candidate URL, exposing the
ownership claim that earlier source scoring missed. Candidate-link ordering
does not affect the search decision, and escaped quotation marks do not end
source-rationale checks. These are changes to the experiment; earlier saved
results remain unchanged and their aggregate rates must not be compared directly.
Free-text explanations still require manual review for unsupported claims.

### Baseline with the corrected probes

Results: `eval/results/20261110-112711/`, with snapshots of probe definitions
and the original media instructions/descriptions. The original executable
finished the entire batch before the revised executable was started.

- Source selection: 0/3 scored passes. Two completed answers selected the
  intended candidate but claimed verified ownership; the third timed out.
- Audio planning: 1/3 scored passes. One timed out. Both completed answers
  produced the correct format plan, but one unnecessarily called `media_status`
  during offline planning and failed the tool-use criterion.
- Search claim discipline: 3/3 passed under whole-answer schema validation.
- Overall: 4/9. There were no failed tool results; timeouts and forbidden
  successful calls remain separate failure categories.

Physical-memory observations ranged from about 2 GiB free at startup to about
0.55 GiB during the batch. After the user freed memory, about 3 GiB was available
and subsequent audio/search runs completed. This is a confounded latency
observation, not proof that memory pressure caused every timeout. The completed
unsupported-ownership answers and unnecessary status call are direct behavioral
evidence regardless of timeout interpretation.

### Bounded implementation

The optional media instructions now make application-status checks conditional
on execution needs, compare requested recordings against source metadata,
distinguish downloaded source paths from converted outputs, and require evidence
before claiming official ownership. Shared `video_info` and `video_download`
descriptions clarify metadata provenance and the absence of transcoding during
download. Single-stream source constraints remain distinct from merged remuxing.

No execution code, permission policy, provider, fallback, compaction or base-prompt
changes were made. The startup budget guard still passes. Evaluator, tool, media and
conventions tests, focused role/startup tests and relevant vet checks passed.

### Interrupted comparison and Tiel calibration

The revised Cyber-Tiel batch is saved in `eval/results/20261110-115017/`.
Source selection scored 1/3: one run skipped activation and overclaimed ownership,
one timed out, and one passed the typed criteria after activating the guidance.
Manual review of the passing answer still found an unsupported assertion of artist
ownership in its rationale, followed by a correct ownership disclaimer. Its
`video_metadata_fetched` claim also borrowed a field from the unrelated search
fixture. A schema pass does not establish the truth or consistency of free text.

The backend was stopped before the remaining audio/search runs could proceed.
All six saved attempts report connection refusal. Preserve those records but
exclude them from claims about model decisions; the batch is not a completed
before/after comparison.

The next server reports Tiel-Coder-35B-A3B UD-Q4_K_XL, text-only, with a
131,072-token context. This differs from Cyber-Tiel and is a separate experiment.
No old eval process remained. Runs now start one probe at a time, with a single
attempt and trace review before the next invocation.

The first Tiel source run, `eval/results/20261110-123547/`, activated media guidance
but timed out at 186.2 seconds while awaiting its fourth model response. There
is no final decision to grade. It also read all three files in the formerly shared
seed. Each probe now has its own seed containing only the relevant evidence.
The time allowance is 360 seconds; task, answer criteria, sampler and response
budget are unchanged. Earlier inputs are snapshotted in that run's `inputs/`.
Neither this budget change nor fixture isolation is evidence of a model fix.

### Individually inspected Tiel runs

With isolated evidence and the 360-second allowance, source selection completed
in 191.0 seconds but failed because it called `media_status`. Its JSON declined
to verify ownership, while the rationale still asserted that the uploader was
the artist. Results: `eval/results/20261110-123946/`. The revised media instructions
were present in the activation result; this was not a delivery failure.

The `media_status` operation description now explicitly excludes saved-evidence
review and planning without execution. The optional guide also asks for a
consistent ownership qualification throughout the rationale and distinguishes
reported duration from proof of recording identity/completeness. No execution
implementation or operation argument schema changed. Fixture-isolation tests
prevent other probes' evidence from being copied into the same workspace.

Each following invocation selected exactly one probe with one attempt. Its
result and final answer were reviewed before the next invocation started:

- Source selection passed in 119.9 seconds, three steps, without a status call:
  `eval/results/20261110-124436/`. It selected the intended candidate and declined
  to verify ownership. Manual review still found its claim that the reported
  duration establishes a full-length recording too strong.
- Audio planning passed in 122.7 seconds, three steps:
  `eval/results/20261110-124742/`. It chose an M4A download path, MP3 re-encoding
  with stream copy disabled, then inspection. Its explanation incorrectly called
  all planned steps local-only, although the future remote download needs network
  access. No media operation was actually executed during this offline task.
- Search claim discipline passed in 68.6 seconds, three steps:
  `eval/results/20261110-125058/`. Both links remained unverified search candidates;
  no extra commands, network calls or status probe were made.

All three had zero failed tool results. These are single-run calibrations,
not a reliability estimate or a controlled latency benchmark. Earlier failures
are preserved. Automated field/tool checks improved, but they do not establish
truthful unrestricted prose. Duration and plan/execution overstatements are
recorded model limitations, outside the accepted implementation scope. They do
not leave an implementation task open in this completed iteration.

## Final acceptance record

The user accepted the bounded feature as sufficient and authorized documentation
closure, commit and push to `main` on 2026-11-10.

Acceptance covers the three probes and isolated fixtures, strict structured
scoring, optional media guidance and operation descriptions, and the completed
code checks and individual model calibrations. Latest model evidence is the
three passing Tiel runs listed above, one attempt per probe; it is not a
reliability estimate or a direct Cyber-Tiel comparison.

Historical failures, interrupted-backend attempts and manual prose observations
remain evidence. They are not pending acceptance checks. No further model run,
engine redesign or implementation change is required to deliver this iteration.
