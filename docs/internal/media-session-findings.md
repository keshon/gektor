# Media session and lazy-tool findings

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
