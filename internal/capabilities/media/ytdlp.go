package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func (s *Service) external(ctx context.Context, name string, in Request) (any, error) {
	if name == "video_download" {
		if err := s.preflightOutput(in.Path); err != nil {
			return nil, err
		}
	}
	dir, err := s.work()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-remote-components", "--no-playlist", "--no-cache-dir", "--no-progress", "--no-warnings", "--socket-timeout", "20", "--retries", "0", "--fragment-retries", "0"}
	// Explicit runtime selection makes local availability deterministic.
	for _, runtime := range []string{"deno", "node"} {
		status, err := s.Apps.Probe(ctx, runtime, false)
		if err != nil {
			return nil, err
		}
		if status.State == "ready" {
			args = append(args, "--js-runtimes", runtime+":"+status.Path)
			break
		}
	}
	args = append(args, "--fixup", "never")
	if in.Mode != "merged" || name != "video_download" {
		args = append(args, "--ffmpeg-location", filepath.Join(dir, "ffmpeg-disabled"))
	}
	switch name {
	case "video_info":
		args = append(args, "--skip-download", "--dump-single-json")
	case "video_transcript":
		language := in.Language
		if language == "" {
			language = "en"
		}
		args = append(args, "--skip-download", "--write-subs", "--write-auto-subs", "--sub-langs", "^"+regexp.QuoteMeta(language)+"$", "--sub-format", "json3", "--output", filepath.Join(dir, "captions.%(ext)s"))
	case "video_download":
		format := "best[vcodec!=none][acodec!=none]"
		switch in.Mode {
		case "audio":
			format = "bestaudio"
		case "video":
			format = "bestvideo"
		case "merged":
			format = "bestvideo+bestaudio/best"
		}
		if in.MaxHeight > 0 && in.Mode != "audio" {
			format = strings.ReplaceAll(format, "bestvideo", "bestvideo[height<="+fmt.Sprint(in.MaxHeight)+"]")
			if in.Mode == "combined" {
				format += "[height<=" + fmt.Sprint(in.MaxHeight) + "]"
			}
			if in.Mode == "merged" {
				format = strings.ReplaceAll(format, "/best", "/best[height<="+fmt.Sprint(in.MaxHeight)+"]")
			}
		}
		if in.Mode != "merged" {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(in.Path)), ".")
			if in.Mode == "audio" && ext == "mp4" {
				ext = "m4a"
			}
			format += "[ext=" + ext + "]"
		}
		sizeFilter := fmt.Sprintf("[filesize<=?%d]", in.MaxBytes)
		format = strings.ReplaceAll(format, "bestvideo", "bestvideo"+sizeFilter)
		format = strings.ReplaceAll(format, "bestaudio", "bestaudio"+sizeFilter)
		format = strings.ReplaceAll(format, "/best", "/best"+sizeFilter)
		if strings.HasPrefix(format, "best[") {
			format = "best" + sizeFilter + strings.TrimPrefix(format, "best")
		}
		args = append(args, "--no-part", "--no-mtime", "--max-filesize", fmt.Sprint(in.MaxBytes), "--format", format, "--output", filepath.Join(dir, "download.%(ext)s"))
		if in.Mode == "merged" {
			ff, err := s.Apps.Require(ctx, "ffmpeg")
			if err != nil {
				return nil, err
			}
			fp, err := s.Apps.Require(ctx, "ffprobe")
			if err != nil {
				return nil, err
			}
			if !strings.EqualFold(filepath.Dir(ff.Path), filepath.Dir(fp.Path)) {
				return nil, fmt.Errorf("yt-dlp merging requires ffmpeg and ffprobe in the same directory")
			}
			args = append(args, "--ffmpeg-location", filepath.Dir(ff.Path), "--merge-output-format", containerExtension(in.Path))
		}
	}
	args = append(args, "--", in.URL)
	result, err := s.command(ctx, "yt-dlp", args, dir, 3*in.MaxBytes)
	if err != nil {
		return nil, err
	}
	if name == "video_info" {
		return externalInfo(result.Stdout)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	if len(files) != 1 {
		return nil, fmt.Errorf("yt-dlp produced %d files; expected exactly one result", len(files))
	}
	if name == "video_transcript" {
		if filepath.Ext(files[0]) != ".json3" {
			return nil, fmt.Errorf("this extractor did not supply json3 captions")
		}
		file, err := os.Open(files[0])
		if err != nil {
			return nil, err
		}
		defer file.Close()
		var data struct {
			Events []struct {
				Start    int `json:"tStartMs"`
				Duration int `json:"dDurationMs"`
				Segments []struct {
					Text string `json:"utf8"`
				} `json:"segs"`
			} `json:"events"`
		}
		raw, err := readBounded(file, 8<<20)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		segments := []segment{}
		for _, event := range data.Events {
			var text strings.Builder
			for _, seg := range event.Segments {
				text.WriteString(seg.Text)
			}
			if value := strings.TrimSpace(text.String()); value != "" {
				segments = append(segments, segment{value, event.Start, event.Duration})
			}
		}
		if len(segments) == 0 {
			return nil, fmt.Errorf("no transcript segments returned")
		}
		lang := in.Language
		if lang == "" {
			lang = "en"
		}
		return transcriptPage(in, segments, "yt-dlp", lang)
	}
	provider := "yt-dlp"
	if in.Mode == "merged" {
		provider = "yt-dlp+ffmpeg"
	}
	return s.publish(ctx, in, files[0], provider)
}
func externalInfo(raw []byte) (any, error) {
	var source struct {
		ID, Title, Description string
		Uploader               string
		Duration               float64
		Formats                []struct {
			ID         string `json:"format_id"`
			Ext        string
			Height     int
			AudioCodec string `json:"acodec"`
			VideoCodec string `json:"vcodec"`
			Size       int64  `json:"filesize"`
		}
		Subtitles map[string]json.RawMessage
		Auto      map[string]json.RawMessage `json:"automatic_captions"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	out := metadata{Backend: "yt-dlp", ID: source.ID, Title: brief([]byte(source.Title), 512), Author: brief([]byte(source.Uploader), 256), Description: brief([]byte(source.Description), 4096), DurationSeconds: source.Duration, Formats: []videoFormat{}, Languages: []string{}}
	for i, f := range source.Formats {
		if i >= 64 {
			out.FormatsOmitted = true
			break
		}
		out.Formats = append(out.Formats, videoFormat{ID: f.ID, MediaType: f.Ext, Height: f.Height, Audio: f.AudioCodec != "none" && f.AudioCodec != "", Video: f.VideoCodec != "none" && f.VideoCodec != "", Size: f.Size})
	}
	langs := map[string]bool{}
	for lang := range source.Subtitles {
		langs[lang] = true
	}
	for lang := range source.Auto {
		langs[lang] = true
	}
	for lang := range langs {
		out.Languages = append(out.Languages, lang)
	}
	sort.Strings(out.Languages)
	if len(out.Languages) > 64 {
		out.Languages = out.Languages[:64]
	}
	return out, nil
}
