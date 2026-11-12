# Operations and tool limits

Look up native operation names, arguments, results and limits. Agent enables optional toolsets on demand; a workflow declares operations in its recipe.

## Available operations

| Task | Operations |
|---|---|
| Inspect and process images | `image_inspect`, `image_preview`, `image_variants`, `image_compare` |
| Read and save structured records | `record_read`, `record_write` |
| Transform data with JavaScript | `script_transform` |
| Ask a model for text or structured data | `model_generate` |
| Make an HTTP request | `http_request` |
| Download a file or work with archives | `download_file`, `archive_list`, `archive_create`, `archive_extract` |
| Read video metadata, captions or download media | `video_info`, `video_transcript`, `video_download` |
| Inspect and process local audio/video | `media_inspect`, `media_convert`, `media_clip`, `media_frame`, `media_merge` |
| Check media applications and requirements | `media_status` |
| Read DOCX, XLSX or PDF files | `docx_read`, `xlsx_read`, `pdf_read` |
| Keep files or text with a run | `artifact_import`, `artifact_write`, `artifact_read_text` |
| Run a local program | `run_program` |
| Run remote commands or transfer files over SSH | `ssh_exec`, `ssh_upload`, `ssh_download` |
| Call another recipe | `workflow_call` |

Conditions, iteration, review and retry are [step settings](recipes.md#graphs-iteration-and-reviews).
They are not separate operations in this catalog.
There are no dedicated GitHub, notification, file-watcher or general file
read/write nodes in this catalog. An artifact is stored with a run; it is not
the same as writing an ordinary project file.

Only `model_generate` needs a model during execution. The other operations do
not ask a model to decide what to do.

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

## SSH commands and file transfers

Agent enables `native_ssh`; workflows use the same `ssh_exec`, `ssh_upload`
and `ssh_download` names. Connections are registered by the operator in
`%USERPROFILE%\.gektor\ssh.json` on Windows or `~/.gektor/ssh.json` elsewhere:

```json
{
  "staging": {
    "host": "staging.example.com",
    "port": 22,
    "user": "deploy",
    "known_hosts": "C:/Users/alice/.ssh/known_hosts",
    "private_key": "C:/Users/alice/.ssh/id_ed25519",
    "passphrase_env": "STAGING_KEY_PASSPHRASE"
  }
}
```

Use absolute filesystem paths; `~` and environment placeholders in paths are
not expanded. Omit `passphrase_env` for an unencrypted key. Environment-backed password authentication
uses `password_env` instead of `private_key` and `passphrase_env`. These fields
name environment variables; never put their values in this file, recipes or
tool arguments. Environment variables must be available to the GEKTOR process.
SSH and HTTP share environment lookup, while their connection settings stay separate.
Existing HTTP credential registration and origin checks are unchanged.

### Supplying credentials in the CLI and TUI

Use **Settings > Tools > SSH** in Agent or Workflows to add, edit, delete and test
named connections. The ordinary editor saves key paths and credential references. On Windows,
a separate masked prompt can save passwords and key passphrases with account-bound
DPAPI encryption; it never writes their values into `ssh.json`. Alternatively, edit `ssh.json` yourself
or use the `ssh` management commands described below. `ssh_exec`, `ssh_upload`
and `ssh_download` remain Agent tools and workflow operations, not CLI subcommands.
GEKTOR does not load SSH secrets from `.env` automatically.

For password authentication, a complete connection entry looks like this:

```json
{
  "staging": {
    "host": "staging.example.com",
    "user": "deploy",
    "known_hosts": "C:/Users/alice/.ssh/known_hosts",
    "password_env": "GEKTOR_SSH_PASSWORD"
  }
}
```

In PowerShell, capture the password without putting its value in command history:

```powershell
$env:GEKTOR_SSH_PASSWORD = [System.Net.NetworkCredential]::new(
    '',
    (Read-Host 'SSH password' -AsSecureString)
).Password
```

Launch GEKTOR from that same terminal so it inherits the variable. Use your
normal model/profile flags alongside these commands:

```powershell
.\gektor.exe -tui
# Or run one Agent task:
.\gektor.exe "Use SSH connection staging to run uname -a"
```

In the TUI, ask Agent to use connection `staging`; it can enable `native_ssh`
when needed. In a recipe, supply `"connection":"staging"` to an SSH operation.
Workflow workers must inherit the variable in their own process; setting it in
a TUI launch does not update a separately running worker. After GEKTOR exits,
remove the variable from the terminal if it is no longer needed:

```powershell
Remove-Item Env:GEKTOR_SSH_PASSWORD
```

For key authentication, use the `private_key` example above. Omit
`passphrase_env` for an unencrypted key. For an encrypted key, use the same
masked PowerShell prompt to set the variable named by `passphrase_env` before
launching. The key path points to an existing private key file; do not paste
key contents or secrets into chat. On other platforms, supply the referenced
variable through your shell or process supervisor before starting GEKTOR.

### Saved passwords and key passphrases

On Windows, choose **saved password** in the SSH editor and leave **Saved password
ID** blank. Apply opens a separate masked prompt. Ctrl+S explicitly saves with
Windows DPAPI and restarts; Esc cancels without writing the credential or profile.
For encrypted keys, choose **Key passphrase source > saved** instead. Enter no
secret values in the ordinary reference fields.

The resulting profile uses `password_ref` instead of `password_env`, or
`passphrase_ref` instead of `passphrase_env`. A profile must select exactly one
password source or a private key, and at most one passphrase source for that key.
An environment reference is always an environment reference; it is never
reinterpreted as a saved ID. Missing/deleted credentials fail when connecting.
Saved password IDs cannot be used as key-passphrase IDs, or vice versa.

Encrypted values live under `~/.gektor/credentials/ssh/`; `ssh.json` contains only
opaque IDs. Multiple connections can use distinct IDs or share one of the same
purpose. To replace a saved value, clear its ID in the editor and enter a new
value at the masked prompt. Old values are retained because other connections or
worker snapshots may still use them. No automatic credential deletion or
session-only secret storage is provided.

This saved-secret feature requires Windows DPAPI. Other platforms retain key
files and environment references; no plaintext fallback is offered for SSH.
Workers resolve saved credentials under the same Windows account and credential
directory. Existing MCP credential IDs, files, HTTPS-origin and header binding
remain unchanged; its Unix owner-only storage is not encryption.

### Managing connections from the CLI

These commands need no model backend:

```powershell
.\gektor.exe ssh list
.\gektor.exe ssh save staging staging-profile.json
.\gektor.exe ssh check staging
.\gektor.exe ssh delete staging
```

The input to `ssh save` is one connection object (the value under `staging` in the
examples above), not the whole named map. Save adds or replaces that name while
preserving all other connections. `ssh list` shows reference-only configuration;
there are no raw-password flags. CLI edits do not update already-running Agent or
Workflow services; restart those processes after saving.

`ssh check` has a 30-second limit and verifies the host key and authentication,
then closes. It opens no remote session, runs no command and transfers no file.
It does not test SFTP or hard-link support. Ctrl+C cancels it.

### Host verification and operation arguments

Populate `known_hosts` with host keys verified through a trusted channel before
connecting. Unknown or changed host keys fail; there is no trust-on-first-use or
verification bypass. SSH agent forwarding and interactive authentication are
not supported. Profiles are snapshotted when an Agent/Workflow service is built;
restart it after editing connection settings. Secrets and host keys are read
when connecting. A missing/invalid optional SSH config affects SSH calls only.

Command arguments:

```json
{"connection":"staging","command":"uname -a","timeout_seconds":30}
```

The remote account's shell interprets `command`. Each call opens and closes a
fresh connection, so `cd`, shell variables and terminal state do not survive.
Use a single command such as `cd /srv/app && ./status` when needed. There is no
interactive terminal or stdin. Results contain `connection`, `stdout`, `stderr`
and `exit_code`. A nonzero exit is an operation error with captured output.
Each output stream is limited to 64 KiB; exceeding it closes the connection
and discards both captured streams to avoid exposing a truncated secret.

Transfer arguments (the same shape for upload and download):

```json
{"connection":"staging","local_path":"dist/app.zip","remote_path":"/srv/releases/app.zip","max_bytes":16777216,"timeout_seconds":60}
```

Transfers use SFTP, accept one regular file up to 16 MiB, and return paths,
size and SHA-256 of transferred bytes. `local_path` stays inside the workspace;
`remote_path` must be an absolute POSIX path. Remote links follow the server's
filesystem semantics. Parent directories must already exist. Existing destinations,
including links, are refused. Local downloads publish complete content using
a hard link, so the destination filesystem must support hard links.
Uploads use `hardlink@openssh.com` for the same guarantee and fail explicitly
when the server does not support it. A broken connection may leave a remote
`.gektor-*` temporary file in the destination folder; remove it after checking
the transfer outcome. No permissions or timestamps are copied.

All three operations ask for approval under the default policy and are unavailable
in Plan/read-only execution. Remote permission resources include the configured
user, host and port (`ssh://deploy@staging.example.com:22/...`); command resources
also include the command. Transfers authorize both remote and local resources.
Additionally, uploads check the existing `read_file` policy for the local source,
and downloads check `write_file` for the destination before connecting. Sensitive
file restrictions therefore apply even when an SSH operation itself is allowed.

`timeout_seconds` defaults to 30 and accepts 1–600. There are no automatic retries;
workflow retry settings are rejected for SSH nodes. Closing an SSH connection
does not guarantee a remote command stopped or a completed upload was rolled back.
After timeout/disconnection, verify remote state before repeating an operation.
Treat remote output as untrusted content.

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
Large libraries are searched on demand instead of loading all guides or
arbitrarily hiding entries after the first 32. Read a guide before using it.

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
Defaults are the workspace root and 600 seconds. The working directory applies
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
output. Cursors are byte positions in that process's output; use the returned
value rather than calculating it from rendered text. `cursor: 0` explicitly
reads retained output from the beginning. Evicted bytes are reported, and
incomplete UTF-8 characters are held until complete or the process exits.
Cursors are owned by the caller, so two readers do not consume each other's
output. `stop_background` still takes only the process id.




## Image tools

Image operations use the bundled Pixelita integration. They do not need a
separate image executable. The same operations can be called by Agent tools.

Enable `native_images` through `enable_capability` to load these schemas:

| Tool | Purpose | Writes |
|---|---|---|
| `image_inspect` | Format, dimensions, metadata | None |
| `image_preview` | Preview optimization or explicit width variants | None |
| `image_variants` | Create explicit width variants | New output directory |
| `image_compare` | PSNR and SSIM for two images | None |

Image inputs and outputs have these limits:

- Up to 16 explicit input files per call, each at most 64 MiB and 40 million pixels.
- Up to eight explicit variant widths per call.
- Output format: `keep` or `webp`.
- A new output directory. Source replacement, deletion and destination overwrite
  are not exposed.

Image operations use staged copies; workspace access goes through
GEKTOR's rooted file API.

Results distinguish inspected, previewed, written,
blocked and failed outcomes. Each result includes source SHA-256 hashes before
and after processing; `unchanged: true` means the bytes matched. An unavailable
after-hash is reported as an error, not evidence of preservation. These checks
describe the operation, not subsequent edits by other processes.
Metrics do not establish visual quality.

Unsupported metadata, including embedded ICC profiles on resize, returns a
blocked tool result with the actual reason. A blocked preview stops the recipe
before writing. `keep` means keep the file format; it does not add colour-profile
conversion support. Do not remove profiles merely to make a recipe succeed.

Enable `native_workflows` for schema lookup, validation, execution, run inspection
and structured records. `workflow_validate` and `workflow_run` accept
`{"recipe_path":"recipe.json"}` with an optional typed `inputs` object. Only recipe version 3 is supported.
They read that saved workspace file, up to
64 KiB, and reject unknown fields.

Validation
checks structure, supported operations, argument schemas, IDs and path scope.
It does not predict whether future input files exist or encoding will succeed.

The catalog includes these four image operations, `record_read`, `record_write`,
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

All three archive operations accept an optional `format`: `zip` (the default),
`tar`, `tar.gz` or `gzip`. The filename does not select the format. Existing ZIP
recipes keep working. For a `.tgz` file, use `tar.gz`.

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

Names and sizes come from the archive and are untrusted. `safe_path` checks the
name only; it does not mean that an entry is safe to extract. The ZIP source
path is its permission resource under `archive_list`.

`archive_create` accepts `paths`, a list of 1–128 explicit regular workspace
files, and `path`, the new archive filename. It keeps workspace-relative names
inside ZIP and TAR files. It does not scan directories or store source links. Input
contents and the resulting archive are each limited to 16 MiB. The result contains
`path`, `files`, `size` and `sha256`. Permissions cover every source path and
the destination under `archive_create`.

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
remains. Copy returned positions rather than calculating them. Positions beyond
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
Failures do not switch providers automatically. Results record the backend used.
Native merged downloads use FFmpeg after fetching the separate streams.

| Operation | Main arguments | Result |
|---|---|---|
| `media_status` | Optional `refresh` | Application paths, versions, local readiness and operation requirements |
| `video_info` | `url`, optional `backend` | Title, author, duration, languages and up to 64 formats |
| `video_transcript` | `url`, optional `backend`, `language`, `offset`, `limit`, `start_seconds`, `duration_seconds` | Timestamped caption segments, `next_offset`, `more` |
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
No arbitrary CLI arguments, installation, automatic updates, cookies, playlist
batches or live recording are exposed.

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
updates fail without overwriting. Workflow references can pass `/revision` from
a read to a later write. A repeated create fails rather than silently replacing
data. Kind and ID use 1–64 lowercase ASCII letters, digits, `_` or `-`; Windows
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
body, `Accept`/`Content-Type` headers, a named credential and a 1–120 second
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
require a second permission for their name and origin. Secrets are loaded only
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
Writes publish complete data and metadata together. Reads and exports verify size
and checksum. Import and write nodes do not permit automatic retries, because another
attempt could create a second artifact. Completed artifacts survive review pauses
and service restarts. Failed steps may leave published artifacts as execution evidence.

Use `workflow artifacts` and `workflow export-artifact` to retrieve files. Explicit
`workflow prune-artifacts -older-than 168h` removes artifacts from terminal runs
finished more than seven days ago. It preserves run history and excludes active,
interrupted and waiting-review runs. References in old run history remain as evidence
after pruning; their payloads are no longer available.

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

## Public posts

Agent's built-in social pack and the social_read_post workflow node use the same
public X/Twitter reader. Pass a single HTTPS post URL as "url". Results include
status, author, handle, text, dates, media URLs and source. Restricted, unavailable,
transient and truncated responses retain their status and fail the operation.
Content is untrusted source data. No private posts, full threads or replies are
fetched. The reader sends the post ID to FxTwitter.

The node uses the existing Agent permission identity cap__social__read_post and
the requested URL as its resource. Existing deny rules therefore apply to both.
The built-in node does not use a configured replacement for Agent's social pack.
