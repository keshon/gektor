package media

import (
	"context"
	"time"

	"github.com/keshon/gektor/internal/executables"
)

type OperationStatus struct {
	Name    string   `json:"operation"`
	Backend string   `json:"backend"`
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"`
}

func (s *Service) Status(ctx context.Context, refresh bool) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeoutSeconds*time.Second)
	defer cancel()

	apps := []executables.Status{}
	states := map[string]string{}

	for _, name := range Applications() {
		v, err := s.Apps.Probe(ctx, name, refresh)
		if err != nil {
			return nil, err
		}

		apps = append(apps, v)
		states[name] = v.State
	}

	operations := []OperationStatus{}
	add := func(name, provider string, dependencies ...string) {
		item := OperationStatus{
			Name:    name,
			Backend: provider,
			State:   "ready",
		}

		for _, dependency := range dependencies {
			if states[dependency] != "ready" {
				item.Missing = append(item.Missing, dependency)
			}
		}

		if len(item.Missing) > 0 {
			item.State = "unavailable"
		}

		operations = append(operations, item)
	}
	add("youtube_info_transcript_stream", "native")
	add("youtube_merged_download", "native+ffmpeg", "ffmpeg")
	add("video_info_transcript_download", "yt-dlp", "yt-dlp")
	add("video_merged_download", "yt-dlp+ffmpeg", "yt-dlp", "ffmpeg", "ffprobe")
	add("media_inspect", "ffprobe", "ffprobe")
	add("media_convert_clip_frame_merge", "ffmpeg", "ffmpeg")
	if states["yt-dlp"] == "ready" && states["deno"] != "ready" && states["node"] != "ready" {
		operations[2].State = "limited"
		operations[2].Missing = []string{"deno or node for full YouTube support"}
	}

	add("video_playlist", "native")

	return struct {
		Native       bool                 `json:"native_youtube"`
		Preferred    string               `json:"youtube_preferred_backend"`
		Applications []executables.Status `json:"applications"`
		Operations   []OperationStatus    `json:"operations"`
		Note         string               `json:"note"`
	}{true, "native", apps, operations, "Local requirements only, not execution permission or a remote/codec compatibility test. yt-dlp's EJS component is bundled or separately installed; its presence is not verified here. No installation or hidden fallback."}, nil
}

// Applications lists local dependencies; native YouTube needs none for metadata or single streams.
func Applications() []string { return []string{"yt-dlp", "ffmpeg", "ffprobe", "deno", "node"} }
