# Media capability

These probes check model decisions with the real lazy media tools.

- `40-media-status` checks discovery and actual application status without commands or media writes.
- `41-media-transcript` reads metadata and two caption pages through native YouTube. A successful second page must use offset 3.
- `44-media-download` reproduces the session download: native first, explicit yt-dlp recovery if needed, merged MP4 and actual container/codecs checked with ffprobe. Re-encoding and shell work are forbidden.
- `42-media-workflow` builds, validates and runs a conversion recipe, inspects its run and checks the output codec with ffprobe. The input is a synthetic 100 ms WAV tone.

The download probe requires public YouTube access, FFmpeg and ffprobe; recovery also requires yt-dlp.

The transcript probe requires public YouTube access and current caption availability. The workflow probe requires FFmpeg with FLAC support and ffprobe. Availability or remote failures must be reported separately from model mistakes. No programs are installed by these probes.

One live pass is calibration; use repeated runs to measure variance. Dry runs validate definitions without testing model behavior.
