package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/process"
	"github.com/keshon/gektor/internal/userdirs"
)

func TestAutoRetriesSourceFailureWithinOriginalDeadline(t *testing.T) {
	for _, name := range []string{"video_info", "video_download", "video_playlist", "video_transcript"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			s.Apps = availableApps{}
			var nativeDeadline time.Time
			s.transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				nativeDeadline, _ = req.Context().Deadline()
				return response(403, "source unavailable"), nil
			})
			externalCalls := 0
			s.run = func(ctx context.Context, req process.Request) (process.Result, error) {
				externalCalls++
				deadline, ok := ctx.Deadline()
				if !ok || !deadline.Equal(nativeDeadline) {
					t.Fatal("fallback reset the operation deadline")
				}
				if name == "video_playlist" {
					args := strings.Join(req.Args, " ")
					if !strings.Contains(args, "--flat-playlist") || strings.Contains(args, "--no-playlist") {
						t.Fatal(req.Args)
					}
					return process.Result{Stdout: []byte(`{"id":"PL1234567890123","title":"Fixture","entries":[{"id":"abcdefghijk","title":"Track","duration":60}]}`)}, nil
				}
				if name == "video_info" {
					return process.Result{Stdout: []byte(`{"id":"abcdefghijk","title":"Fallback","formats":[]}`)}, nil
				}
				file := "download.m4a"
				body := "audio"
				if name == "video_transcript" {
					file = "captions.en.json3"
					body = `{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"Hello"}]}]}`
				}
				err := os.WriteFile(filepath.Join(req.Directory, file), []byte(body), 0600)
				return process.Result{}, err
			}

			in := VideoRequest{URL: "https://www.youtube.com/watch?v=abcdefghijk&list=PL1234567890123", Mode: "audio", TimeoutSeconds: 5}
			if name == "video_download" {
				in.Path = "track.m4a"
				in.MaxBytes = 32
			}
			value, err := s.RunVideo(t.Context(), name, in)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if externalCalls != 1 || !strings.Contains(string(raw), `"backend":"yt-dlp"`) {
				t.Fatalf("calls=%d result=%s", externalCalls, raw)
			}
		})
	}
}

func TestAutoStopsAtCancellationAndLocalFailures(t *testing.T) {
	for _, scenario := range []string{"cancelled", "existing output", "invalid path", "transport rejection", "byte limit", "response limit"} {
		t.Run(scenario, func(t *testing.T) {
			s := testService(t)
			s.Apps = availableApps{}
			s.run = func(context.Context, process.Request) (process.Result, error) {
				t.Fatal("stop condition triggered yt-dlp")
				return process.Result{}, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			in := VideoRequest{URL: "https://youtu.be/abcdefghijk", Path: "track.m4a", Mode: "audio", MaxBytes: 2}
			s.transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				switch scenario {
				case "cancelled":
					cancel()
					return response(403, "unavailable"), nil
				case "transport rejection":
					return nil, fmt.Errorf("refused non-public destination")
				case "response limit":
					return response(200, strings.Repeat("x", (8<<20)+1)), nil
				default:
					body := `{"playabilityStatus":{"status":"OK","playableInEmbed":true},"videoDetails":{"videoId":"abcdefghijk","title":"Fixture","author":"Uploader","lengthSeconds":"5"},"streamingData":{"adaptiveFormats":[{"itag":140,"url":"https://media.example.com/audio","mimeType":"audio/mp4","audioChannels":2,"contentLength":"100"}]}}`
					if req.URL.Path == "/watch" {
						body = "var ytInitialPlayerResponse = " + body + ";"
					}
					return response(200, body), nil
				}
			})
			if scenario == "existing output" {
				err := s.WS.WriteFile(in.Path, []byte("keep"), 0600)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "invalid path" {
				in.Path = "../track.m4a"
			}
			_, err := s.RunVideo(ctx, "video_download", in)
			if err == nil {
				t.Fatal("stop condition succeeded")
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestBothExtractorErrorsAreReported(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(403, "unavailable"), nil
	})
	s.run = func(context.Context, process.Request) (process.Result, error) {
		return process.Result{}, fmt.Errorf("external unavailable")
	}
	_, err := s.RunVideo(t.Context(), "video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk"})
	if err == nil || !strings.Contains(err.Error(), "native:") || !strings.Contains(err.Error(), "yt-dlp:") || !strings.Contains(err.Error(), "external unavailable") {
		t.Fatalf("missing extractor causes: %v", err)
	}
}

func TestExplicitYtDlpSkipsNative(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.transport = roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("explicit yt-dlp called native")
		return nil, nil
	})
	s.run = func(context.Context, process.Request) (process.Result, error) {
		return process.Result{Stdout: []byte(`{"id":"abcdefghijk","title":"External","formats":[]}`)}, nil
	}
	_, err := s.RunVideo(t.Context(), "video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk", Backend: "yt-dlp"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLiveAutoFallback(t *testing.T) {
	if os.Getenv("GEKTOR_MEDIA_LIVE_FALLBACK") != "1" {
		t.Skip("set GEKTOR_MEDIA_LIVE_FALLBACK=1 to test real yt-dlp after a simulated native source failure")
	}
	for _, call := range []struct{ name, url string }{
		{"video_info", "https://www.youtube.com/watch?v=rFejpH_tAHM"},
		{"video_playlist", "https://www.youtube.com/playlist?list=PLqAfPOrmacr963ATEroh67fbvjmTzTEx5"},
	} {
		t.Run(call.name, func(t *testing.T) {
			s := testService(t)
			s.Apps = executables.New(userdirs.Sub("applications.json"))
			s.transport = roundTrip(func(*http.Request) (*http.Response, error) {
				return response(403, "simulated native source failure"), nil
			})
			value, err := s.RunVideo(t.Context(), call.name, VideoRequest{URL: call.url, Limit: 2, TimeoutSeconds: 45})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"backend":"yt-dlp"`) {
				t.Fatal("fallback backend missing", string(raw))
			}
			t.Log(call.name, brief(raw, 500))
		})
	}
}

func TestAutoRetriesNetworkFailure(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "lookup unavailable", Name: "www.youtube.com"}
	})
	calls := 0
	s.run = func(context.Context, process.Request) (process.Result, error) {
		calls++
		return process.Result{Stdout: []byte(`{"id":"abcdefghijk","title":"External","formats":[]}`)}, nil
	}
	_, err := s.RunVideo(t.Context(), "video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk"})
	if err != nil || calls != 1 {
		t.Fatalf("network fallback calls=%d: %v", calls, err)
	}
}
