# Read documents and attach files

Attach a small file directly to a message, or ask Agent to read a workspace document with its native tools. Image understanding requires a vision-capable connection.

## Read a large document

Ask Agent to read a DOCX, XLSX or PDF in the workspace and name the information
you need. Native readers return bounded parts rather than an entire document.
Workflows can use the same operations.

Item pagination and text continuation are separate. If an item has
`next_text_offset`, continue that item using the returned value. Top-level
`more: false` means there are no later items; the last item may still have unread text.
Keep the same document and item selector when continuing. Each continuation is
bounded too, so read only the parts needed for the task.

See [document reader contracts](../reference/operations.md#reading-documents)
for offsets, per-call limits and context-budget guards.

## Attach files and images

Type `@` to browse workspace files. For example:

```text
Review @README.md
Compare @"docs/design notes.txt" with @README.md
Describe @screenshot.png
```

Use arrows or click a row to choose a suggestion, then Tab to insert it.
Page Up/Down and the scrollbar move through longer lists. Clicking a row
does not insert text or run a command. Esc closes the list and keeps the draft.
File suggestions insert a path; they do not send the message.

Text files must be UTF-8, up to 32 KiB each and 64 KiB combined per message.
Invalid, binary, missing or out-of-workspace file references stop submission
and keep the draft. A repeated reference attaches one copy.
The model receives file snapshots.
Queued references are read when their turn begins; retries reuse that turn's snapshot.
Attach only content you intend to send to the selected backend.

On Windows, copy a screenshot and press `Ctrl+Alt+V`, or use `/paste-image`.
Ctrl+V also works when the terminal forwards it.
Some terminals paste an image path instead.
A single pasted absolute image path can be imported, including from outside
the workspace. Typing a path or using a file tool keeps the workspace restrictions.

The composer accepts up to eight images, 20 MiB each and 40 MiB combined.
Clipboard images also have a 32-megapixel limit.

Pasted images and browser screenshots are saved in GEKTOR's image cache.
The Agent can enable `native_images` and use `image_attachments` to list their
session IDs. `image_view` shows an image or a cropped detail; `image_inspect`
reads metadata. These tools accept the attachment ID without a workspace copy.
`image_download` fetches a public reference image into that same cache and
reports its source URL. Crop and view operations leave project files alone.
Reusable workflow recipes still need workspace files; attachment IDs belong
to one conversation.
`Alt+1` through `Alt+8`, or `/detach N`, removes an attachment.
`Ctrl+U` clears the draft and attachments.

Image understanding needs a vision-capable model and backend.
ChatGPT accepts PNG, JPEG, WebP and non-animated GIF; convert BMP to PNG first.
llama.cpp vision also needs its vision projector.
GEKTOR's KoboldCPP connection currently rejects image attachments.

## Download audio or video

Give Agent a media URL and say what to save, where in the workspace to put it,
and whether you need a particular format. For a YouTube playlist, specify all
entries or a selection. For example:

> List this YouTube playlist. Download the first three available tracks as MP3 into music/.

Playlist listing downloads nothing. Downloads operate on individual entries;
conversion is a separate step when the source format differs from the requested
format. Existing output files are preserved. Inspect the returned files and any
failed entries before asking Agent to retry.

With the default Auto backend, YouTube tries the built-in downloader first,
then retries source failures with an installed yt-dlp. Other supported sites
use yt-dlp directly. Local conversion
uses FFmpeg, and inspection uses ffprobe. Configure or check these applications
in **Settings > Tools… > Media**. Native YouTube listing and source-stream downloads
can run without them, but conversion and fallback need their respective programs.

Titles and uploader names are source metadata; they do not establish official
channel ownership.

See [video and audio operations](../reference/operations.md#video-and-audio) for
backend selection, formats, limits and final-file inspection.
