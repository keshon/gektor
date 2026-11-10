# Media decisions from fixed evidence

These offline probes reproduce decision points from session `106db964` without
depending on YouTube, search ranking or CDN availability. Each run starts in a
fresh workspace containing only that probe's synthetic saved metadata. The Agent
discovers and activates the real media capability, reads the evidence and returns
a JSON decision.
Commands, network requests, media execution and file changes fail trace checks.

- `46-media-source-selection`: choose full studio audio over a 61-second clip
  with an attractive title and a full-length live performance. Score the selected
  URL and rationale naming uploader, numeric durations and the live mismatch.
  Separately score whether the saved evidence establishes official channel
  ownership; an uploader display name alone does not establish it.
- `47-media-audio-format`: plan an M4A audio download, MP3 conversion with
  re-encoding and final inspection. Score the source extension, mode, conversion
  format, copy decision and operation order. The requested M4A source is fixed;
  this does not compare selection between equally valid source codecs.
- `48-media-claim-evidence`: return two search candidates while declining to
  label them verified official uploads. Score the links, verification boolean
  and search evidence basis. Either URL order is accepted. Inspect the explanation
  manually for contradictory prose; schema validation cannot establish the truth
  of unrestricted explanations.

These are constrained decision tests, not proof that the Agent executes a
download/conversion correctly, authenticates a channel or handles arbitrary
search results. The existing media suite retains live metadata/download probes
and a local FFmpeg workflow for execution coverage. Fixed JSON fields reduce
wording ambiguity; they also make this easier than the original open-ended task.
Whole-answer JSON Schema checks reject extra prose, duplicate keys, missing
fields and contradictory typed values. The source rationale is checked after
JSON decoding, so escaped quotation marks cannot prematurely end its checks.

Run from the repository root:

```text
go run ./cmd/eval -suite media-decisions -runs 3 -jobs 1 -backend http://127.0.0.1:9931 -require-model Cyber-Tiel
```

Preserve baseline results before editing capability instructions. Keep fixtures,
criteria, model, budgets and sampler settings fixed for comparisons. A baseline
pass means this simplified case did not reproduce the observed failure; do not
present it as evidence that the original session issue is resolved.

For interactive diagnosis, run exactly one probe and inspect its trace before
starting the next:

```text
go run ./cmd/eval -only 46-media-source-selection -runs 1 -jobs 1 -backend http://127.0.0.1:9931
```

The probes now allow 360 seconds for up to six scored model steps. The original
180-second allowance timed out while Tiel was waiting for its fourth response.
This is a calibration of the time allowance, not a model-quality improvement.
The original runs remain saved. Isolating fixtures also removes unrelated search
metadata that earlier source-selection explanations incorrectly cited. Changed
fixture/budget fingerprints distinguish this experiment from the earlier batches.
