package media

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/process"
)

func TestNativeMergedDownloadRejectsPossiblyTruncatedOutput(t *testing.T) {
	for _, format := range []string{"mp4", "mkv", "webm"} {
		t.Run(format, func(t *testing.T) {
			s := testService(t)
			s.Apps = availableApps{}
			fixture := `{"playabilityStatus":{"status":"OK","playableInEmbed":true},"videoDetails":{"videoId":"abcdefghijk","title":"Fixture","author":"Fixture","lengthSeconds":"5"},"streamingData":{"adaptiveFormats":[{"itag":137,"url":"https://media.example.com/video","mimeType":"video/mp4","height":360,"contentLength":"1"},{"itag":140,"url":"https://media.example.com/audio","mimeType":"audio/mp4","audioChannels":2,"contentLength":"1"},{"itag":248,"url":"https://media.example.com/video","mimeType":"video/webm","height":360,"contentLength":"1"},{"itag":251,"url":"https://media.example.com/audio","mimeType":"audio/webm","audioChannels":2,"contentLength":"1"}]}}`
			s.transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.Contains(req.URL.Path, "/embed/"):
					return response(200, "/s/player/fixture/player_ias.vflset/en_US/base.js"), nil
				case strings.HasSuffix(req.URL.Path, "base.js"):
					return response(200, "// fixture player"), nil
				case strings.Contains(req.URL.Path, "player"):
					return response(200, fixture), nil
				case req.URL.Host == "media.example.com":
					r := response(206, "x")
					r.Header.Set("Content-Range", "bytes 0-0/1")
					return r, nil
				default:
					return response(200, "var ytInitialPlayerResponse = "+fixture+";"), nil
				}
			})
			s.run = func(_ context.Context, req process.Request) (process.Result, error) {
				if filepath.Ext(req.Args[len(req.Args)-1]) != "."+format {
					t.Fatal(req.Args)
				}
				// FFmpeg may exit successfully after reaching -fs. The final publication
				// must reject that outcome even though the output fits the byte bound.
				return process.Result{}, os.WriteFile(req.Args[len(req.Args)-1], []byte(strings.Repeat("x", 32)), 0600)
			}
			_, err := s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://youtu.be/abcdefghijk", Path: "out." + format, Mode: "merged", MaxBytes: 32})
			if err == nil || !strings.Contains(err.Error(), "possibly truncated") {
				t.Fatal(err)
			}
			if _, err := s.WS.Stat("out." + format); !os.IsNotExist(err) {
				t.Fatal("partial merged output was published", err)
			}
		})
	}
}
