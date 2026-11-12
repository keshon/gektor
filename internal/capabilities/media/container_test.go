package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kkdai/youtube/v2"

	"github.com/keshon/gektor/internal/process"
)

func TestExternalMergedContainerFollowsRequestedPath(t *testing.T) {
	for _, format := range []string{"mp4", "mkv", "webm"} {
		t.Run(format, func(t *testing.T) {
			s := testService(t)
			s.Apps = availableApps{}
			s.run = func(_ context.Context, req process.Request) (process.Result, error) {
				i := slices.Index(req.Args, "--merge-output-format")
				if i < 0 || req.Args[i+1] != format {
					t.Fatal(req.Args)
				}
				return process.Result{}, os.WriteFile(filepath.Join(req.Directory, "download."+format), []byte("fixture"), 0600)
			}
			out, err := s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://vimeo.com/123", Path: "out." + format, Mode: "merged"})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(out)
			if !strings.Contains(string(raw), `"container":"`+format+`"`) {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestUnexpectedContainerIsNeverPublished(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.run = func(_ context.Context, req process.Request) (process.Result, error) {
		return process.Result{}, os.WriteFile(filepath.Join(req.Directory, "download.mkv"), []byte("fixture"), 0600)
	}
	_, err := s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://vimeo.com/123", Path: "out.mp4", Mode: "merged"})
	if err == nil || !strings.Contains(err.Error(), "output container") {
		t.Fatal(err)
	}
	if _, err := s.WS.Stat("out.mp4"); !os.IsNotExist(err) {
		t.Fatal("mislabeled output published", err)
	}
}

func TestNativeStreamSelectionHonorsContainer(t *testing.T) {
	formats := youtube.FormatList{
		{MimeType: `video/webm; codecs="vp9"`, Height: 720, AudioChannels: 2, ContentLength: 10},
		{MimeType: `video/mp4; codecs="avc1"`, Height: 360, AudioChannels: 2, ContentLength: 10},
	}
	got, err := nativeFormat(formats, VideoRequest{Path: "out.mp4", Mode: "combined", MaxBytes: 100})
	if err != nil || got.Height != 360 {
		t.Fatal(got, err)
	}
	if _, err := nativeFormat(formats[:1], VideoRequest{Path: "out.mp4", Mode: "combined", MaxBytes: 100}); err == nil {
		t.Fatal("selected WebM for an MP4 path")
	}
}

func TestConvertCopyRemuxesWithoutEncoding(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	if err := s.WS.WriteFile("source.mkv", []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s.run = func(_ context.Context, req process.Request) (process.Result, error) {
		i := slices.Index(req.Args, "-c")
		if i < 0 || req.Args[i+1] != "copy" || slices.Contains(req.Args, "libx264") {
			t.Fatal(req.Args)
		}
		return process.Result{}, os.WriteFile(req.Args[len(req.Args)-1], []byte("fixture"), 0600)
	}
	if _, err := s.RunLocal(t.Context(), "media_convert", LocalRequest{Inputs: []string{"source.mkv"}, Path: "out.mp4", Format: "mp4", Copy: true}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []LocalRequest{
		{Path: "wrong.mp4", Format: "flac"},
		{Path: "out.flac", Format: "flac", Copy: true},
	} {
		if _, err := s.ValidateLocal("media_convert", in); err == nil {
			t.Fatal("invalid conversion accepted", in)
		}
	}
}
