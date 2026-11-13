package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkdai/youtube/v2"

	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/process"
	"github.com/keshon/gektor/internal/workspace"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testService(t *testing.T) *Service {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	s := New(ws, executables.New(filepath.Join(t.TempDir(), "applications.json")))
	s.Apps.(*executables.Resolver).ConfigPath = filepath.Join(t.TempDir(), "applications.json")
	return s
}
func response(code int, raw string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}
func TestNativePreferredAndYouTubeRoundTrip(t *testing.T) {
	s := testService(t)
	fixture := `{"playabilityStatus":{"status":"OK","playableInEmbed":true},"videoDetails":{"videoId":"abcdefghijk","title":"Fixture talk","author":"Fixture author","lengthSeconds":"5"},"streamingData":{"formats":[{"itag":18,"url":"https://media.example.com/video","mimeType":"video/mp4","height":360,"audioChannels":2,"contentLength":"5"}]},"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"https://www.youtube.com/captions","languageCode":"en","kind":"asr"}]}}}`
	calls := 0
	s.transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		switch {
		case strings.Contains(req.URL.Path, "/embed/"):
			return response(200, "/s/player/fixture/player_ias.vflset/en_US/base.js"), nil
		case strings.HasSuffix(req.URL.Path, "base.js"):
			return response(200, "// fixture player"), nil

		case strings.Contains(req.URL.Path, "player"):
			return response(200, fixture), nil
		case req.URL.Path == "/captions":
			return response(200, `<timedtext><body><p t="0" d="1000">First</p><p t="1000" d="1000">Second</p></body></timedtext>`), nil
		case req.URL.Host == "media.example.com":
			var start, end int
			if _, err := fmt.Sscanf(req.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
				t.Fatal(err)
			}
			r := response(206, "media"[start:end+1])
			r.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/5", start, end))
			return r, nil
		default:
			return response(200, "var ytInitialPlayerResponse = "+fixture+";"), nil
		}
	})
	s.run = func(context.Context, process.Request) (process.Result, error) {
		t.Fatal("native auto invoked an external backend")
		return process.Result{}, nil
	}
	value, err := s.RunVideo(t.Context(), "video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	if !strings.Contains(string(raw), `"backend":"native"`) || !strings.Contains(string(raw), "Fixture talk") {
		t.Fatal(string(raw))
	}
	value, err = s.RunVideo(t.Context(), "video_transcript", VideoRequest{URL: "https://youtu.be/abcdefghijk", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(value)
	if !strings.Contains(string(raw), `"more":true`) || !strings.Contains(string(raw), `"next_offset":1`) {
		t.Fatal(string(raw))
	}
	value, err = s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://youtu.be/abcdefghijk", Path: "saved.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.WS.Open("saved.mp4")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(file)
	file.Close()
	if string(data) != "media" {
		t.Fatal(string(data))
	}
	before := calls
	if _, err = s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://youtu.be/abcdefghijk", Path: "saved.mp4"}); err == nil {
		t.Fatal("overwrote output")
	}
	if calls != before {
		t.Log("existing-output check occurred after metadata retrieval")
	}
	for _, raw := range []string{"https://youtu.be/abcdefghijk", "https://www.youtube.com/watch?v=abcdefghijk"} {
		in, _ := s.ValidateVideo("video_info", VideoRequest{URL: raw})
		if backend(in) != "native" {
			t.Fatal(in)
		}
	}
	in, _ := s.ValidateVideo("video_info", VideoRequest{URL: "https://vimeo.com/123"})
	if backend(in) != "yt-dlp" {
		t.Fatal(in)
	}
}
func TestExplicitNativeFailureDoesNotFallbackAndPathsAreBounded(t *testing.T) {
	s := testService(t)
	s.transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("fixture outage") })
	s.run = func(context.Context, process.Request) (process.Result, error) {
		t.Fatal("hidden backend fallback")
		return process.Result{}, nil
	}
	if _, err := s.RunVideo(t.Context(), "video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk", Backend: "native"}); err == nil {
		t.Fatal("native outage hidden")
	}
	for _, in := range []VideoRequest{{URL: "http://127.0.0.1/video"}, {URL: "https://example.com/video", Backend: "native"}, {URL: "https://youtu.be/abcdefghijk", Path: "../outside"}} {
		if _, err := s.ValidateVideo("video_download", in); err == nil {
			t.Fatal("invalid request accepted", in)
		}
	}
	f := youtube.FormatList{{Height: 1080, AudioChannels: 0, ContentLength: 100}, {Height: 360, AudioChannels: 2, ContentLength: 10}, {Height: 0, AudioChannels: 2, ContentLength: 3}}
	if got, err := nativeFormat(f, VideoRequest{Mode: "combined", MaxBytes: 200}); err != nil || got.Height != 360 {
		t.Fatal(got, err)
	}
	if got, err := nativeFormat(f, VideoRequest{Mode: "audio", MaxBytes: 200}); err != nil || got.Height != 0 {
		t.Fatal(got, err)
	}
	if _, err := nativeFormat(f, VideoRequest{Mode: "combined", MaxBytes: 2}); err == nil {
		t.Fatal("oversized stream selected")
	}
}

type availableApps struct{}

func (availableApps) Probe(_ context.Context, name string, _ bool) (executables.Status, error) {
	state := "ready"
	if name == "node" || name == "deno" {
		state = "disabled"
	}
	return executables.Status{Name: name, State: state, Path: os.Args[0], Version: "fixture"}, nil
}
func (a availableApps) Require(ctx context.Context, name string) (executables.Status, error) {
	return a.Probe(ctx, name, true)
}
func TestExternalTypedArgumentsAndFailedOutputCleanup(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	s.run = func(ctx context.Context, in process.Request) (process.Result, error) {
		joined := strings.Join(in.Args, " ")
		if !strings.Contains(joined, "--ignore-config") || !strings.Contains(joined, "--no-plugin-dirs") || !strings.Contains(joined, "--no-remote-components") {
			t.Fatal(in.Args)
		}
		if in.Args[len(in.Args)-2] != "--" || in.Args[len(in.Args)-1] != "https://vimeo.com/123" {
			t.Fatal(in.Args)
		}
		if err := os.WriteFile(filepath.Join(in.Directory, "download.mp4"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		return process.Result{}, nil
	}
	out, err := s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://vimeo.com/123", Path: "out.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"backend":"yt-dlp"`) {
		t.Fatal(string(raw))
	}
	s.run = func(ctx context.Context, in process.Request) (process.Result, error) {
		if err := os.WriteFile(filepath.Join(in.Directory, "partial.mp4"), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		return process.Result{}, fmt.Errorf("fixture failure")
	}
	if _, err := s.RunVideo(t.Context(), "video_download", VideoRequest{URL: "https://vimeo.com/123", Path: "failed.mp4"}); err == nil {
		t.Fatal("failure ignored")
	}
	if _, err := s.WS.Stat("failed.mp4"); !os.IsNotExist(err) {
		t.Fatal("failed output published", err)
	}
}
func TestLocalProcessingStagesInputsAndReportsBackend(t *testing.T) {
	s := testService(t)
	s.Apps = availableApps{}
	if err := s.WS.WriteFile("in.wav", []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s.run = func(ctx context.Context, in process.Request) (process.Result, error) {
		args := strings.Join(in.Args, " ")
		if !strings.Contains(args, "-protocol_whitelist file,pipe") || !strings.Contains(args, "-format_whitelist") || strings.Contains(args, s.WS.Root()) {
			t.Fatal(in.Args)
		}
		if err := os.WriteFile(in.Args[len(in.Args)-1], []byte("converted"), 0600); err != nil {
			t.Fatal(err)
		}
		return process.Result{}, nil
	}
	out, err := s.RunLocal(t.Context(), "media_convert", LocalRequest{Inputs: []string{"in.wav"}, Path: "out.flac", Format: "flac"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"backend":"ffmpeg"`) {
		t.Fatal(string(raw))
	}
	if _, err := s.RunLocal(t.Context(), "media_convert", LocalRequest{Inputs: []string{"../outside"}, Path: "bad.flac", Format: "flac"}); err == nil {
		t.Fatal("outside input accepted")
	}
}

func TestVideoAndLocalValidationKeepSeparateInputs(t *testing.T) {
	s := testService(t)
	video, err := s.ValidateVideo("video_info", VideoRequest{URL: "https://youtu.be/abcdefghijk#chapter"})
	if err != nil {
		t.Fatal(err)
	}
	if video.URL != "https://youtu.be/abcdefghijk" || video.Backend != "auto" || video.Mode != "combined" || video.Limit != 50 || video.MaxBytes != MaxBytes || video.TimeoutSeconds != 120 {
		t.Fatalf("video defaults changed: %+v", video)
	}

	inputs := []string{filepath.Join(s.WS.Root(), "source.wav")}
	local, err := s.ValidateLocal("media_convert", LocalRequest{Inputs: inputs, Path: "converted.flac", Format: "flac"})
	if err != nil {
		t.Fatal(err)
	}
	if local.Inputs[0] != "source.wav" || local.Path != "converted.flac" || local.MaxBytes != MaxBytes || local.TimeoutSeconds != 120 {
		t.Fatalf("local defaults changed: %+v", local)
	}
	if inputs[0] != filepath.Join(s.WS.Root(), "source.wav") {
		t.Fatal("validation mutated caller inputs")
	}
	local.Inputs[0] = "changed.wav"
	if inputs[0] == "changed.wav" {
		t.Fatal("normalized request shares input storage")
	}
}

func TestMediaFamilyDispatchRejectsWrongOperationsBeforeExecution(t *testing.T) {
	// No workspace, resolver or transport: a wrong family must stop before effects.
	s := &Service{}
	for _, name := range []string{"media_status", "media_convert", "video_unknown"} {
		_, err := s.RunVideo(t.Context(), name, VideoRequest{URL: "https://youtu.be/abcdefghijk"})
		if err == nil || !strings.Contains(err.Error(), "unknown video operation") {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"media_status", "video_download", "media_unknown"} {
		_, err := s.RunLocal(t.Context(), name, LocalRequest{})
		if err == nil || !strings.Contains(err.Error(), "unknown local media operation") {
			t.Fatal(name, err)
		}
	}
}
