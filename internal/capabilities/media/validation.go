package media

import (
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/keshon/gektor/internal/httptransport"
)

func (s *Service) ValidateVideo(name string, in VideoRequest) (VideoRequest, error) {
	switch name {
	case "video_info", "video_transcript", "video_download", "video_playlist":
	default:
		return in, fmt.Errorf("unknown video operation %q", name)
	}

	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = defaultTimeoutSeconds
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 600 {
		return in, fmt.Errorf("timeout_seconds must be 1–600")
	}

	if in.MaxBytes == 0 {
		in.MaxBytes = MaxBytes
	}
	if in.MaxBytes < 1 || in.MaxBytes > MaxBytes {
		return in, fmt.Errorf("max_bytes must be 1–%d", MaxBytes)
	}

	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 {
		return in, fmt.Errorf("offset must be nonnegative and limit 1–100")
	}

	if in.Backend == "" {
		in.Backend = "auto"
	}
	if in.Backend != "auto" && in.Backend != "native" && in.Backend != "yt-dlp" {
		return in, fmt.Errorf("unknown backend")
	}

	var err error
	in.URL, err = httptransport.Normalize(in.URL, false)
	if err != nil {
		return in, err
	}

	u, err := url.Parse(in.URL)
	if err != nil {
		return in, err
	}
	u.Fragment = ""
	in.URL = u.String()
	if in.Backend == "native" && !isYouTube(in.URL) {
		return in, fmt.Errorf("native backend supports YouTube URLs only")
	}

	if name == "video_playlist" {
		if !isYouTube(in.URL) || u.Query().Get("list") == "" {
			return in, fmt.Errorf("video_playlist requires a YouTube playlist URL")
		}
	}

	if in.Mode == "" {
		in.Mode = "combined"
	}
	if in.Mode != "combined" && in.Mode != "audio" && in.Mode != "video" && in.Mode != "merged" {
		return in, fmt.Errorf("unknown download mode")
	}

	if in.StartSeconds < 0 || in.DurationSeconds < 0 || in.StartSeconds > 86400 || in.DurationSeconds > 86400 {
		return in, fmt.Errorf("time range must be within 0–86400 seconds")
	}

	if in.MaxHeight < 0 || in.MaxHeight > 4320 {
		return in, fmt.Errorf("max_height must be 0–4320")
	}

	if in.Path != "" {
		var err error
		in.Path, err = s.path(in.Path)
		if err != nil {
			return in, err
		}
	}

	if name == "video_download" {
		ext := containerExtension(in.Path)
		if ext == "" {
			return in, fmt.Errorf("download path needs a container extension; inspect video_info formats")
		}

		if in.Mode == "merged" && ext != "mp4" && ext != "mkv" && ext != "webm" {
			return in, fmt.Errorf("merged combines video and audio into .mp4, .mkv or .webm; for a track use mode=audio with a source extension such as .m4a, then media_convert only if another format is requested")
		}
	}

	return in, nil
}

func (s *Service) ValidateLocal(name string, in LocalRequest) (LocalRequest, error) {
	switch name {
	case "media_inspect", "media_convert", "media_clip", "media_frame", "media_merge":
	default:
		return in, fmt.Errorf("unknown local media operation %q", name)
	}

	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = defaultTimeoutSeconds
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 600 {
		return in, fmt.Errorf("timeout_seconds must be 1–600")
	}

	if in.MaxBytes == 0 {
		in.MaxBytes = MaxBytes
	}
	if in.MaxBytes < 1 || in.MaxBytes > MaxBytes {
		return in, fmt.Errorf("max_bytes must be 1–%d", MaxBytes)
	}

	if in.StartSeconds < 0 || in.DurationSeconds < 0 || in.StartSeconds > 86400 || in.DurationSeconds > 86400 {
		return in, fmt.Errorf("time range must be within 0–86400 seconds")
	}

	if in.Path != "" {
		var err error
		in.Path, err = s.path(in.Path)
		if err != nil {
			return in, err
		}
	}

	if name == "media_convert" || name == "media_clip" || name == "media_frame" || name == "media_merge" {
		if in.Format == "" || containerExtension(in.Path) != containerExtension("output."+in.Format) {
			return in, fmt.Errorf("output path extension must match format %q", in.Format)
		}
	}

	if in.Copy && (name != "media_convert" || (in.Format != "mp4" && in.Format != "mkv" && in.Format != "webm")) {
		return in, fmt.Errorf("copy is supported only by media_convert with mp4, mkv or webm")
	}

	in.Inputs = append([]string(nil), in.Inputs...)

	for i, path := range in.Inputs {
		var err error
		in.Inputs[i], err = s.path(path)
		if err != nil {
			return in, err
		}
	}

	return in, nil
}

func (s *Service) path(path string) (string, error) {
	if s.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}

	rel, err := s.WS.Relative(path)
	if err != nil {
		return "", err
	}

	if rel == "." {
		return "", fmt.Errorf("expected a file path")
	}

	return filepath.ToSlash(rel), nil
}
