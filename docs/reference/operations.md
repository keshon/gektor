# Operations and tool limits

Look up native operation names, arguments, results and limits. Agent enables optional toolsets on demand; a workflow declares operations in its recipe.

## Available operations

| Task | Operations |
|---|---|
| [Inspect and process images](#image-tools) | `image_inspect`, `image_preview`, `image_optimize`, `image_variants`, `image_compare` |
| [Read and save structured records](#structured-records) | `record_read`, `record_write` |
| [Transform data with JavaScript](#javascript) | `script_transform` |
| [Ask a model for text or structured data](#model-nodes) | `model_generate` |
| [Make an HTTP request](#http-requests) | `http_request` |
| [Download a file or work with archives](#file-downloads-and-archives) | `download_file`, `archive_list`, `archive_create`, `archive_extract` |
| [List YouTube playlists, read video metadata/captions or download media](#video-and-audio) | `video_playlist`, `video_info`, `video_transcript`, `video_download` |
| [Inspect and process local audio/video](#video-and-audio) | `media_inspect`, `media_convert`, `media_clip`, `media_frame`, `media_merge` |
| [Check media applications and requirements](#external-media-applications) | `media_status` |
| [Read DOCX, XLSX or PDF files](#reading-documents) | `docx_read`, `xlsx_read`, `pdf_read` |
| [Keep files or text with a run](#run-artifacts) | `artifact_import`, `artifact_write`, `artifact_read_text` |
| [Run a local program](#local-programs) | `run_program` |
| [Run remote commands or transfer files over SSH](ssh.md) | `ssh_exec`, `ssh_upload`, `ssh_download` |
| [Call another recipe](recipes.md#reusable-subworkflows) | `workflow_call` |

For Agent tools, start with [Command execution](#command-execution),
[Editing and Git](#editing-and-git-tools), or
[Search and background output](#search-context-and-process-output).
Conditions, iteration, review and retry are [recipe step settings](recipes.md#graphs-iteration-and-reviews).
There are no dedicated GitHub, notification, file-watcher or general file
read/write nodes in this catalog. An artifact is stored with a run; it is not
the same as writing an ordinary project file.

Of the operations listed here, only `model_generate` needs a model connection.

## Command execution

On Windows, shell tools and `-verify-cmd` use `pwsh.exe` when available,
otherwise `powershell.exe`. The executable is selected once per process;
the agent's platform instructions and `system_info` report that same shell.
Scripts travel through UTF-8 stdin with profiles and interactive prompts disabled.
Use PowerShell syntax; `&&` and `||` are not portable to Windows PowerShell 5.1.
On other platforms shell commands use `sh`.

Use `run_program` for native executables with literal arguments. PowerShell
has its own quoting, pipeline and error semantics (notably native stderr
redirection in 5.1); the harness does not rewrite scripts or enable global
fail-fast behavior. Shell success describes the final statement, not every
earlier command. Use explicit conditions when one command depends on another.

## Editing and Git tools

Existing files are edited with `patch_file`: its `old_content` must match one
exact block copied from the current file. Include surrounding lines to make
the block unique. Missing, ambiguous or unchanged matches are refused before
writing. CRLF and UTF-8 BOM are
preserved; UTF-16 text must be converted before patching.

`read_file` returns a continuation when its content limit is reached. Use the
returned `offset` for a complete-line boundary or `byte_offset` to continue
inside a long line. Byte positions refer to decoded UTF-8 with CRLF normalized
to LF, not raw bytes in a UTF-16 or CRLF file. Do not combine the two offsets.

Reading a picture (PNG, JPG/JPEG, WebP, GIF, BMP) attaches its pixels to the
conversation on a vision-capable backend instead of returning binary content;
`metadata_only=true` still returns just the header. Vision capability is
detected at startup from llama-server's `/props` modalities, or from an
OpenAI-compatible `/models` list when the provider publishes input modalities,
with a known-model-family fallback. Missing metadata stays unknown and does not
block explicit attachments. ChatGPT reads `input_modalities` from the account
model catalog. A text-only backend reports the image type
and says the pixels are unavailable, so the agent does not re-read the file.

`git` inspects status, log, diff, show, blame and tracked files without shell
interpretation. `diff` includes actual changes; `staged: true` selects the
index. `show` requires a commit revision; `blame` requires a file path. Log
results are limited to 50 commits; all output is byte-bounded.

`git_add` stages a literal workspace-relative path. `git_commit` commits the
already-staged changes using the supplied message and appends
`Co-authored-by: GEKTOR <no-reply@keshon.ru>`. It keeps the configured author
and runs normal repository hooks. These two tools are unavailable for
execution in Plan and read-only workers. Inspect staged changes and obtain
user authorization before committing; a commit includes unrelated changes
that were already staged too.

## Search context and process output

`system_info {"group":"skills"}` discovers metadata from nested `skills/**/SKILL.md`
packages, including libraries copied into an extra category directory. Filter
matches path or description; offset/limit paginate all matches. Discovery is
live, while the initial prompt catalog stays stable for context caching.
Search large libraries on demand and read the matching guide before using it.

`grep_files` accepts `before_context` and `after_context`, each from 0 to 10.
Matches use `path:line:text`, context uses `path-line-text`; overlapping windows
are merged. Disjoint groups are separated by `--`. Results stop at 200 matches
or 48 KiB of rows and report truncation. Long lines remain individually capped.

`run_shell` and `start_background` take a required shell `command`. Use them for
builtins, pipelines, redirection and Windows `.cmd`/`.bat` wrappers.
`run_program` and `start_program` take a required native `program` plus literal
`args`; the executable is started directly, so no shell quoting, expansion,
pipes or redirection applies. All four tools accept a workspace-relative
`workdir`.

`run_shell` and `run_program` also accept `timeout_seconds` from 1 to 600.
Defaults are the workspace root and 120 seconds. Set an explicit timeout for
slower finite work, or use a managed background command. The working directory applies
to that call only. Unknown parameters and invalid bounds are refused before
starting a process. Servers still use `start_background` or `start_program`
rather than a longer blocking timeout.

Inspect startup context without running a task:

```powershell
gektor -workspace . -context-report "Explain this project"
```

Reports separate core instructions, project instructions, skill metadata,
identity, history/task and tool schemas. Component costs use character-based
estimates; they are not tokenizer measurements. Normal runs also publish
`prompt_budget` events with backend prompt/cached counts when available.
The TUI `/context` dialog shows the breakdown and largest tool schemas.
Cached tokens still occupy context. Offline previews exclude eager `-mcp`
catalogs and images, and use text-only schemas; runtime reports use the actual
request. Resumed sessions use runtime reporting instead of offline preview.

`start_background` and `start_program` return an id and `next_cursor`.
`check_background` requires
both `id` and `cursor`: pass the previous `next_cursor` to retrieve only new
output. Cursors count bytes in the process output. Reuse the returned value. `cursor: 0` explicitly
reads retained output from the beginning. Evicted bytes are reported, and
incomplete UTF-8 characters are held until complete or the process exits.
Cursors are owned by the caller, so two readers do not consume each other's
output. `stop_background` still takes only the process id. Completion notifications
contain the process ID and status. Retrieve logs and failure details with
`check_background`; notifications do not replay command output.

## Image tools

Agent image tools use the [Pixelita Go API](https://github.com/keshon/pixelita/blob/v0.2.0/docs/go-api.md)
compiled into GEKTOR. GEKTOR stages workspace inputs and authorizes output paths
before publication. Inspect, compare and optimization reports use the
`gektor.images.v1` schema.

Enable `native_images` through `enable_capability` to load these schemas:

| Tool | Purpose | Writes |
|---|---|---|
| `image_inspect` | Format, dimensions, metadata | None |
| `image_preview` | Preview optimization or explicit width variants | None |
| `image_optimize` | Quantize PNGs, optimize JPEGs or convert to WebP at original dimensions | New output directory |
| `image_variants` | Create explicit width variants | New output directory |
| `image_compare` | PSNR and SSIM for two images | None |

Image inputs and outputs have these limits:

- Up to 16 explicit input files per call, each at most 64 MiB and 40 million pixels.
- Up to eight explicit variant widths per call.
- Output format: `keep` or `webp`.
- A new output directory. Source replacement, deletion and destination overwrite
  are not exposed.

For original-size output, preview without `widths`, then use `image_optimize`:

```json
{"paths":["input.png"],"format":"webp","out_dir":"optimized"}
```

Use `format: "keep"` on a PNG for palette quantization. JPEG `keep` optimizes
Huffman coding. `image_variants` resizes images; it does not quantize PNGs.
Pixelita applies its size and fidelity thresholds and may decline an output.
The tools do not expose quantizer tuning, WebP quality settings or conversion
to PNG from another format.

Image operations use staged copies; workspace access goes through
GEKTOR's rooted file API.

Results distinguish inspected, previewed, written,
blocked and failed outcomes. Each result includes source SHA-256 hashes before
and after processing; `unchanged: true` means the bytes matched. An unavailable
after-hash is reported as an error, not evidence of preservation. These checks
describe the operation, not subsequent edits by other processes.
Metrics do not establish visual quality.

The default `color_profile: "preserve"` blocks conversion or resizing that would
lose an ICC profile. For web output, preview with `color_profile: "srgb"`, then
use the same option on `image_optimize` or `image_variants`. Pixelita converts
supported RGB matrix/TRC ICC profiles before encoding or resizing. This changes
the pixel values to sRGB; it does not simply discard the profile. Unsupported
profiles and animation remain blocked, with the actual reason in the result.
A blocked preview stops the recipe before writing.

Same-size JPEG `format: "keep"` retains the original profile and pixels through
lossless Huffman optimization. Use WebP or an explicit resize for sRGB conversion;
JPEG Huffman optimization cannot transform colors. Profile stripping is not exposed.

Enable `native_workflows` for schema lookup, validation, execution, run inspection
and structured records. `workflow_validate` and `workflow_run` accept
`{"recipe_path":"recipe.json"}` with an optional typed `inputs` object. Only recipe version 4 is supported.
They read that saved workspace file, up to
64 KiB, and reject unknown fields.

Validation
checks structure, supported operations, argument schemas, IDs and path scope.
It does not predict whether future input files exist or encoding will succeed.

The catalog includes these five image operations, `record_read`, `record_write`,
`script_transform`, `model_generate`, `http_request`, `artifact_import`,
`artifact_write`, `artifact_read_text`, `docx_read`, `xlsx_read`, `pdf_read`, `download_file`, `archive_list`, `archive_create`, `archive_extract`,
`run_program`, `workflow_call` and the [media operations](#video-and-audio).

Image and record permissions match direct calls. It validates step templates and authorizes known arguments
before starting. Output-dependent arguments are validated and authorized after
resolution, immediately before that step. Earlier effects remain if a later
value or permission check fails.
Execution reads and revalidates the current file; prior validation does not
freeze it. Each run stores the exact source text, its SHA-256 hash and the parsed
definition. The returned hash identifies the version actually executed.

## File downloads and archives

These Go-native operations are also Agent tools in the optional `native_files`
pack. Enable that pack for direct tool calls. Workflow execution uses the same
implementation and does not need a separate capability activation.

### Download a file

`download_file` accepts `url` and `path`. It downloads one public HTTP(S) URL
into a new workspace file. The parent folder must exist. Existing files and
links are never overwritten. Optional `max_bytes` can lower the 16 MiB limit;
`timeout_seconds` is 1–120, with a default of 30. Redirects, automatic retries,
credentials, recursive downloading and resume are not supported.

The result contains `path`, `url`, `size`, `sha256` and `media_type`. Bytes are
copied without text conversion and are not returned to the model. Hand the
returned `path` to another file operation or `artifact_import` to keep a
run-owned copy. Both the normalized URL and destination path are permission
resources under `download_file`. Failed or cancelled transfers remove their
new output file; downloading never executes its contents.

### Archive formats

All three archive operations accept an optional `format`: `zip` (the default),
`tar`, `tar.gz` or `gzip`. The filename does not select the format. Existing ZIP
recipes keep working. For a `.tgz` file, use `tar.gz`.

### List archive entries

`archive_list` accepts a workspace `path` to a ZIP file up to 16 MiB with at
most 10,000 entries. Metadata reads are capped at 2 MiB to bound inspection
work even for malformed archives. Optional `offset` starts at zero; `limit`
defaults to 50 and accepts 1–100. Results contain `total`, `offset`, `more` and `entries`.
An offset past the end returns an empty page. Each entry reports its name,
declared size, compressed size, directory/link flags and `safe_path`.

ZIP listing does not decompress entries, check their contents or extract files.

TAR and GZIP listing must read the stream. They allow at most 16 MiB of payload
and 2 MiB of headers and padding. Listing writes no files. TAR entries report
`compressed_size: 0` because a per-entry compressed size is unavailable.

GZIP holds one file: creation requires one source and keeps its basename.
GZIP header names must fit Latin-1; use TAR.GZ for other names.
Extraction uses the GZIP header name, or the input basename without `.gz` if
that header is empty. Unsafe names, corrupt trailers and multiple GZIP members
are rejected on extraction. Sparse TAR entries are unsupported.

Treat archive names and sizes as untrusted. `safe_path` reports a name check.
Extraction validates the archive separately. The ZIP source
path is its permission resource under `archive_list`.

### Create an archive

`archive_create` accepts `paths`, a list of 1–128 explicit regular workspace
files, and `path`, the new archive filename. It keeps workspace-relative names
inside ZIP and TAR files. It does not scan directories or store source links. Input
contents and the resulting archive are each limited to 16 MiB. The result contains
`path`, `files`, `size` and `sha256`. Permissions cover every source path and
the destination under `archive_create`.

### Extract an archive

`archive_extract` accepts an archive `path` and a new `out_dir`. Both source and
destination must be in the workspace; the destination's parent must exist.
Optional `max_bytes` can lower the 16 MiB total expanded-data limit. It returns
the source `path`, output `directory`, extracted `files` count and total `bytes`.

Extraction checks all names and authorizes every generated file and directory
before writing. It refuses traversal, drive and stream paths, device names,
links, encrypted or special entries, duplicate names and file/directory or
case collisions. ZIP compression methods are Store and Deflate. Names are limited
to 4,096 bytes and 64 path segments. Entries and implied directories together
cannot exceed 10,000 paths inside the new folder. ZIP metadata reads have a separate 2 MiB budget.

Directories use private permissions and files are written without executable
bits. Existing outputs are never replaced. Failed or cancelled operations
remove the files and directories they created; cleanup errors are reported.
The source and output folder are permission resources under `archive_extract`;
individual generated paths are checked under the same name. Archive listing works
in Plan mode; creation and extraction require Act mode. Creation and extraction
are not marked retry-safe.

For a download step named `get`, pass its saved path to a later listing step:

```json
{"id":"list","tool":"archive_list","args":{"path":{"$step":"get","pointer":"/path"}}}
```

## Reading documents

`docx_read`, `xlsx_read` and `pdf_read` read files inside the workspace. They
are also Agent tools in the optional `native_documents` package. All three work
in Plan mode. Their permission resource is the source path under the operation's
name. A file is limited to 16 MiB. No Office suite, Python or PDF command is needed.

| Operation | What comes back | Paging |
|---|---|---|
| `docx_read` | Body paragraphs and table rows in document order | `offset` counts blocks; `limit` defaults to 50, maximum 100 |
| `xlsx_read` | Sheet names and visibility; add `sheet` to read its stored cells | `offset` counts stored cells; `limit` defaults to 50, maximum 100 |
| `pdf_read` | Page text and common metadata fields | `offset` counts pages; `limit` defaults to 10, maximum 10 |

Offsets start at zero. Pass the returned `next_offset` as the next request's
`offset` while `more` is true. `total` counts blocks, stored cells or PDF pages.
For XLSX, omit `sheet` to list names first, then pass an exact name.

```json
{"id":"read","tool":"xlsx_read","args":{"path":"sales.xlsx","sheet":"Sales","limit":50}}
```

Results contain at most 32 KiB of content text. A shortened block, cell or PDF
page has `truncated: true` and `next_text_offset`. To finish that item, repeat
its zero-based item `offset` with `text_offset` copied from `next_text_offset`:

```json
{"path":"report.docx","offset":7,"text_offset":32768}
```

`text_offset` counts Unicode code points, not bytes. When present, `limit`
defaults to 1 and must be 1; XLSX also requires an exact `sheet`. Each returned
item reports `text_offset` when nonzero and `next_text_offset` only while text
remains. Use the returned offsets. Positions beyond
the selected item's text, or selecting a missing item, are errors; a position
exactly at the text end returns an empty completed slice.

Continuation traverses paragraph/page `text`, DOCX row `cells` in order, or XLSX
`value` then `formula`, without adding separators. Each slice stays in its
original field; row cell positions and item metadata are preserved. Consumed or
not-yet-reached fields are empty. Append slices to each corresponding field or
cell to reconstruct it.

Empty or omitted fields in a truncated item may still
be unread; finish continuation before claiming their content is absent.

XLSX
cell type describes the stored value, not whether a formula exists: a string
cell can have a formula returned only after its value is fully read.

PDF `no_text` describes the full page, not its slice.
PDF page numbers in results start at one.

Finish item text continuations before following the original page's
`next_offset`. Top-level `more` counts remaining items: even `more: false` can
contain an incomplete last item. Sources must stay unchanged across reads;
restart after edits. There are no automatic continuation reads or snapshots.
The normal agent output limiter and context compaction also apply, so an entire
document need not remain in active model context. Read only needed content and
retain useful findings while processing a long document.

DOCX tables return rows with a `table` number and `cells`. Formatting, drawings,
headers, footnotes and deleted revisions are omitted. Nested tables are refused.
Merged cells are not expanded into a rectangular grid.

XLSX cells keep their addresses, types and saved values. Empty gaps do not
allocate rows or columns. Formulas are returned as text and are never evaluated;
their cached values may be old or missing. Shared and array formula markers are
kept without expanding formulas into other cells. Numbers remain raw numbers,
including dates stored as numbers. Styles, merged ranges, charts, macros and
external links are not read or executed.

PDF text order can differ from the page layout. `no_text: true` means that no
text was extracted from that page; it does not prove that the page is blank.
Image-only pages need OCR, which is not included. Encrypted PDFs, legacy `.doc`
and `.xls` files and document generation are not supported by these readers.

Document readers use a separate GEKTOR process with a ten-second deadline and
bounded output. It receives the already-opened file's bytes, not a filesystem
path. Read Office XML parts share a 16 MiB expansion budget. Document contents
are untrusted data, including text that looks like instructions.

## Video and audio

Enable `native_media` for direct Agent calls. The same operations are workflow
nodes and use the same service. Enabling the pack exposes schemas; it does not
install programs or grant permission. Its short description appears in the
bounded capability map. Detailed instructions and tool schemas appear after activation.

`backend: "auto"` is the default for video operations. It uses the bundled
`kkdai/youtube` library for YouTube and yt-dlp for other public HTTP(S) sites.
Choose `backend: "native"` or `backend: "yt-dlp"` explicitly when needed.
With `auto`, a YouTube extraction or stream-access failure retries once with
yt-dlp. Explicit backends never switch. Both attempts share the operation deadline
and retain the same output size limit. Cancellation, transport rejections, resource
limits, local file errors and FFmpeg failures stop the call. Results record the
backend that succeeded; if both extractors fail, the error includes both causes.
Native merged downloads use FFmpeg after fetching the separate streams.

| Operation | Main arguments | Result |
|---|---|---|
| `media_status` | Optional `refresh` | Application paths, versions, local readiness and operation requirements |
| `video_info` | `url`, optional `backend` | Title, author, duration, languages and up to 64 formats |
| `video_transcript` | `url`, optional `backend`, `language`, `offset`, `limit`, `start_seconds`, `duration_seconds` | Timestamped caption segments, `next_offset`, `more` |
| `video_playlist` | YouTube `url` with `list=`, optional `offset`, `limit` | Ordered entries, source URLs, durations, continuation offset and actual backend |
| `video_download` | `url`, `path`, optional `backend`, `mode`, `max_height`, `max_bytes` | New file, actual container, size, SHA-256 and backend |
| `media_inspect` | `inputs: ["file"]` | ffprobe stream and container metadata |
| `media_convert` | `inputs: ["file"]`, `path`, `format` | New re-encoded file; optional `copy: true` remuxes MP4/MKV/WebM |
| `media_clip` | `inputs: ["file"]`, `path`, `format`, `duration_seconds`, optional `start_seconds` | New re-encoded clip |
| `media_frame` | `inputs: ["video"]`, `path`, `format: "png"` or `"jpg"`, optional `start_seconds` | One frame for image tools such as Pixelita |
| `media_merge` | `inputs: ["video", "audio"]`, `path`, `format` | Video from the first input and audio from the second, merged without re-encoding |

Download modes are `combined` (default, one stream with video and audio),
`audio`, `video` and `merged`. Native selection uses formats with a known size
that fit the requested limits. `max_height` is an upper bound, not a minimum
quality promise. The output path needs a container extension. Single-stream
selection matches that extension; merged downloads copy streams into the
requested MP4, MKV or WebM container without re-encoding. Incompatible codecs
fail without publishing a file. Tools reject a filename/container mismatch;
they never rename the requested output silently. `media_convert` and `media_clip` accept `mp4`, `mkv`, `webm`,
`mp3`, `m4a`, `wav` and `flac`; merge accepts `mp4`, `mkv` and `webm`.
Output extensions must match `format`. Conversion and clipping re-encode:
MP4/MKV use H.264 video and AAC audio. `media_convert` with `copy: true` copies
the first video and audio streams into MP4, MKV or WebM without re-encoding.
That changes the container without another lossy encode; incompatible codecs
fail. Installed codecs and container compatibility can still limit these operations.

Caption text is not audio transcription. Each page contains at most 100 segments
(default 50) and 32 KiB of text. Continue from `next_offset` while `more` is true.
Time ranges are in seconds and include overlapping caption segments. Native
language selection prefers English when advertised, otherwise the first track;
yt-dlp defaults to English. An explicit language never falls back to another.
yt-dlp captions must be available in json3 format. Metadata and captions are
untrusted source content.

Inputs and outputs are limited to 256 MiB per file. `max_bytes` can lower the
output limit. `timeout_seconds` defaults to 120 and is limited to 600. Existing
outputs are preserved, parent directories must exist, and failed work is not
published. External work runs in a temporary directory; inputs are copied there
through workspace access. Temporary size checks are periodic and can briefly
overshoot their limit. Local media operations refuse network protocols and
playlist demuxers. External programs are not an OS sandbox, and remote
extractors make their own network requests.

### External media applications

`media_status` probes FFmpeg, ffprobe, yt-dlp, Deno and Node with short version
commands. Native YouTube works without those executables. Missing dependencies
only affect operations that need them. A ready application is a local observation;
it does not prove remote access, a codec or an extractor will succeed. yt-dlp's
EJS component is bundled or installed separately; status does not verify it.

GEKTOR first reads explicit executable paths from
`~/.gektor/applications.json`, then uses PATH for applications without a configured
path. Configured paths must be absolute. For example:

```json
{
  "applications": {
    "ffmpeg": {"path": "C:\\Programs\\ffmpeg-6.1\\bin\\ffmpeg.exe"},
    "ffprobe": {"path": "C:\\Programs\\ffmpeg-6.1\\bin\\ffprobe.exe"},
    "yt-dlp": {"path": "C:\\Programs\\yt-dlp\\yt-dlp.exe"},
    "node": {"path": "C:\\Program Files\\nodejs\\node.exe"},
    "deno": {"disabled": true}
  }
}
```

Probes are cached for up to a minute. Pass `refresh: true` to probe again.
Execution rechecks required programs in the process that runs the operation,
including a workflow worker. yt-dlp merging currently requires FFmpeg and ffprobe
in the same directory. Ordinary downloads disable FFmpeg post-processing.
User yt-dlp configuration, plugins and remote component downloads are disabled.
`video_playlist` lists YouTube playlist entries through kkdai or yt-dlp, with zero-based
`offset`, `limit` (default 50, maximum 100), `next_offset` and `more`. Entry indexes
are one-based and retain playlist order. Listing downloads nothing. The library
fetches the playlist before paging the returned entries; each page request refetches
it, so changes to the remote playlist can shift offsets. Auto fallback and explicit
backend selection also apply to playlist listing. yt-dlp lists without downloading
individual entries using `--flat-playlist`. The operation deadline still applies;
native HTTP responses and external-process output retain their size limits. Inspect selected entries with `video_info`
and download each requested track with `video_download`; each file keeps its own
permission, size limit and output path. yt-dlp downloads remain single-video.

For a track, use `mode: "audio"` and a supported source extension such as `.m4a`.
Keep that format unless another format is requested. Reuse returned workspace paths
verbatim. Metadata titles and uploader names do not verify official ownership.

No arbitrary CLI arguments, installation, automatic updates, cookies, implicit
playlist batches or live recording are exposed.

Recipes keep operation names and backend choices; machine-specific executable
locations stay in user settings. Permissions use each operation's own name and
its source URL, input files and output path. Third-party execution is allowed
through that bounded operation, without granting general `run_program` access.

## Structured records

Records hold bounded JSON objects, such as image metadata or a draft for later
review. Each workspace has its own record kinds and registered JSON schemas.
Register a workspace schema file explicitly:

```powershell
gektor workflow register-schema -workspace . assets assets.schema.json
```

For example, `assets.schema.json`:

```json
{"type":"object","properties":{"width":{"type":"integer","minimum":1}},"required":["width"],"additionalProperties":false}
```

The schema must declare `type: object`; schema and data limits are 64 KiB each.
Local schema references are supported; loading external schemas is disabled.
A kind's schema is immutable. Register a new kind to change its shape.
The agent can read a registered schema through
`workflow_schema {"record_kind":"assets"}`. No schema-registration tool is
exposed; registration is an explicit CLI operation.

Direct tools and recipe steps use the same arguments:

| Tool | Arguments | Result |
|---|---|---|
| `record_write` | `{"kind":"assets","id":"image","expected_revision":0,"data":{"width":320}}` | Entry with revision 1 |
| `record_read` | `{"kind":"assets","id":"image"}` | Entry with `data`, `revision`, schema hash and update time |

By default, reading a missing record fails. Add `"allow_missing": true` to
return `{"kind":"assets","id":"image","found":false,"revision":0,"data":null}`
instead. An existing record returns its normal fields plus `"found": true`
when this option is enabled. The read does not create anything. Pass revision
zero to a later write to create it; a concurrent create still causes a conflict.
The kind must have a registered schema. Missing schemas, invalid IDs and damaged
records still fail.

Revision 0 creates; updates require the current revision. Concurrent or stale
updates fail without overwriting. Pass `/revision` from a workflow read to a later write. Repeating a create
with revision 0 produces a conflict. Kind and ID use 1–64 lowercase ASCII letters, digits, `_` or `-`; Windows
device names and the ID `schema` are reserved. Permission resources are `kind/id`,
for example `-deny "record_write=assets/*"`.

Record data is validated against the registered schema immediately before writing.
`workflow_validate` checks operation arguments and reference templates; it does
not establish that a record exists, its data matches a schema, or a revision is
still current. Each write is atomic individually; there is no transaction across
steps and no automatic retry after conflicts. Records live under
`~/.gektor/workflows/<workspace-hash>/records/<kind>/`.

## JavaScript

`script_transform` takes `code` (a function body) and JSON `input`. Return a JSON
value, for example `return {total: input.items.reduce((n, x) => n + x.price, 0)};`.
There is no Node.js runtime, package loading, filesystem, network or environment
API. Use a separate node for effects. JavaScript numbers use IEEE-754; preserve
large identifiers as strings.

Scripts run in a separate Goja worker with a two-second evaluation deadline,
five-second process deadline, a 128 MiB soft Go heap target and OS memory limits
(256 MiB process memory on Windows; 2 GiB address space on Unix). Input/code are
limited to 64 KiB each; output is limited to 1 MiB. These are resource boundaries,
not a general OS sandbox for hostile native code.

## Model nodes

`model_generate` takes a workspace `profile`, explicit `prompt`, optional JSON
`input`, optional `output_schema`, `max_tokens` and `timeout_seconds`. It makes
one model request with no agent tools or hidden chat history. Output contains
`text`, `model`, `usage`, and `data` when a schema is supplied. Invalid structured
output and truncated responses fail the node. Profile output/context limits
apply; the default node timeout is 120 seconds. One model node runs at a time
within a process. Separate service processes have separate limits.

The request carries both schema encodings used by the existing providers.
KoboldCpp supports a stringified schema in the grammar field from
[v1.90.2](https://github.com/LostRuins/koboldcpp/releases/tag/v1.90.2).

## HTTP requests

`http_request` accepts a public HTTP(S) URL, method (default GET), optional JSON
body, `Accept`/`Content-Type` headers, a named credential with its required
`credential_revision`, and a 1–120 second
`timeout_seconds` (default 30). Redirects are not followed. Non-2xx statuses fail
the step with a bounded response retained for inspection. Set
`"status_as_data": true` to return those statuses as ordinary results so later
steps can handle them. Network failures, read failures and oversized responses
still fail the step.

Responses are limited to 256 KiB. Output contains `status_code`, `body`,
`headers`, and `data` for valid JSON. The `headers` object contains only
`content-type`, `retry-after`, `x-ratelimit-limit`, `x-ratelimit-remaining` and
`x-ratelimit-reset`, when present. Names are lowercase; only the first value is
returned. Each selected header is limited to 4 KiB; exceeding it fails the step.
Credential values are redacted from these headers as well as the body.
No HTTP request is automatically retried, including POSTs with ambiguous outcomes.

The [Moltbook draft example](../../examples/workflows/moltbook-draft/README.md)
uses HTTP status handling and an optional record read. It saves local drafts
without publishing anything.

It shares URL and DNS validation with native network tools: private destinations,
loopback and environment proxies are excluded. Resolved addresses are checked
before dialing. Each request requires permission for `METHOD URL`; credentials
require a second permission for their name, origin and binding revision. See
[credential commands](cli.md#workflow-credential-references) for inspection and
revocation. Secrets are loaded only
after authorization and are redacted from response/error text when echoed
verbatim. Never put secrets in recipe URLs, bodies or ordinary arguments.

[Repository report example](../../examples/workflows/repository-report/README.md)
fetches data, transforms it, optionally asks a model for a summary, waits for
operator review, and saves a record.

## Run artifacts

Artifacts store file bytes outside the workspace, under the producing run. Their
metadata includes `id`, `run_id`, `step_id`, `name`, `media_type`, `size`, `sha256`
and `created`. Names are labels, not filesystem paths. MIME type is detected from
the bytes. Artifact IDs only resolve within the current run; no cross-run lookup
is exposed to nodes.

- `artifact_import`: copy a regular workspace `path`. Its permission resource is
  the normalized workspace-relative path under `artifact_import`.
- `artifact_read_text`: read an artifact `id` as UTF-8 text, rejecting binary data
  and files larger than 32 KiB. Returns `text` and `artifact` metadata. Its permission
  resource is the ID under `artifact_read_text`.
- `artifact_write`: save explicit `name` and `text`; its permission resource is
  the name under `artifact_write`. Returns metadata, including the new ID.

Use `{"$step":"capture","pointer":"/id"}` to pass an imported artifact to
another node. Text still obeys ordinary recipe argument and output limits. Binary
files can be captured and exported, but `artifact_read_text` is not a binary decoder.
Image nodes accept workspace paths rather than artifact IDs.

Each artifact is limited to 16 MiB; a run may retain 256 artifacts and 256 MiB total.
All runs in one workspace store share 4,096 artifacts and 1 GiB, including pending
reservations and deletions. The SQLite reservation transaction enforces these
limits across worker and CLI processes. These are artifact limits, not a quota
for the entire database or workspace filesystem.
Parallel writers reserve capacity before streaming and wait, with cancellation,
when other producers temporarily hold the quota. Metadata and reservations live
in SQLite; payload bytes remain in per-run files. Only complete publications are
visible. Reads and exports verify size
and checksum. Import and write nodes do not permit automatic retries, because another
attempt could create a second artifact. Completed artifacts survive review pauses
and service restarts. Failed steps may leave published artifacts as execution evidence.

Use `workflow artifacts` and `workflow export-artifact` to retrieve files. Explicit
`workflow prune-artifacts -older-than 168h` removes artifacts from terminal runs
finished more than seven days ago. It preserves run history and excludes active,
interrupted and waiting-review runs. References in old run history remain as evidence
after pruning; their payloads are no longer available.
`workflow prune-runs -older-than 168h` also removes old terminal run/job history,
including invocation attempts and callback receipts. Active leases, waiting,
reviewable, interrupted and unfinished runs are preserved, as are recipes,
schedules and structured records. Removing history ends its callback/submission
deduplication window. Both retention commands are explicit; neither runs in the
background. See [retention commands](cli.md#workflow-artifacts) for failure and
storage behavior.
Unfinished publications are reclaimed before new execution and by the service;
live run leases protect active producers. Interrupted pruning resumes from its
recorded deletion intent.

See [the log report example](../../examples/workflows/log-report/README.md) for a complete
AI-free file pipeline with review and export.

## Local programs

`run_program` executes a native executable with literal `program` and `args`.
`workdir` defaults to the workspace root and must name a directory inside it.
`timeout_seconds` defaults to 60, with a range of 1–600. Optional `stdin_artifact`
feeds captured bytes from the current run to stdin; otherwise stdin is closed.
There is no automatic shell expansion. On Windows, `.cmd`, `.bat` and `.ps1`
require an explicitly named interpreter. The node is finite; it does not launch
a background service.

Permissions use the existing `run_program` identity and the resource spelling
`program arg1 arg2`. This is a permission/display string, never an executed shell
command. The normal policy applies: defaults are permissive, explicit denies and
destructive-command prompts are honored, and unattended execution denies prompts.
An allow rule is not an allowlist for every other program. Use policy deny rules
when restricting a service. Review gates remain separate from permissions.

The child inherits the environment with the same credential-name scrubbing used
by native agent tools. This is not a sandbox: programs and explicit interpreters
run with the user's OS permissions and can access files or network services.
Workspace confinement applies to selecting `workdir`, not to everything the child
can do. Program files and PATH resolution remain live resources.

Results contain `exit_code`, `truncated`, and `stdout`/`stderr` artifact metadata.
Nonzero exits, startup errors, deadlines, cancellation and output overflow fail
the step. Each stream retains at most 1 MiB; excess output stops the process and
sets `truncated: true`. Capture is byte-preserving, including binary output. Tree
cleanup uses the shared Windows job / Unix process-group implementation, with
bounded pipe draining. Diagnostics are persisted with a separate five-second
deadline even after cancellation. Artifact storage failures also fail the step.

No automatic retries are allowed: a failed process may already have changed files
or contacted a service. Stdout/stderr are artifacts; other generated workspace files
can be captured by a subsequent `artifact_import` step after successful execution.
See [the local program example](../../examples/workflows/process-report/README.md).

## Public social reads

Agent's built-in social pack and workflow nodes use the same FxTwitter readers:

| Workflow node | Agent permission identity | Input resource |
| --- | --- | --- |
| `social_read_post` | `cap__social__read_post` | Requested post URL |
| `social_read_thread` | `cap__social__read_thread` | Canonical `https://x.com/i/status/<id>` |
| `social_read_timeline` | `cap__social__read_timeline` | `https://x.com/<lowercase handle>` |
| `social_read_trends` | `cap__social__read_trends` | `https://x.com/explore` |

`read_post` takes `url`. `read_thread` takes `url` and optional `cursor`;
its result separates `post`, `thread` and `replies`. `read_timeline` takes
`handle`, optional `cursor` and `limit`. `read_trends` takes optional `limit`
and returns topic names/context without associated posts. Limits default to
10 and range from 1 to 20. A timeline may return more posts if the provider
ignores the requested count. Timeline and thread reads retain the provider page,
capped at 100 entries per post/thread/reply list for response safety.

Page results include `status`, `source`, `retrieved_at`, `more`, `truncated`
and an optional `next_cursor`. Follow that cursor for another provider page.
Local truncation is separate from pagination; the cursor cannot recover
omitted entries from the current page. Restricted, unavailable, transient,
malformed, partial and truncated responses retain evidence and fail the
operation. Missing accounts and unavailable timelines are not empty successes.

Requests send post IDs, handles and cursors to FxTwitter. No media is downloaded.
Content is untrusted source data. The provider does not currently offer public
search; trends are not search results, and account timelines are not home feeds.
Private posts are unsupported; conversation coverage depends on the provider. The built-in
nodes do not use configured replacements for Agent's social pack. See
[built-in social reader](../guide/extensions.md#built-in-social-reader) for limits.
