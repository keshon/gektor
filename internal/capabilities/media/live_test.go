package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/process"
	"github.com/keshon/gektor/internal/userdirs"
)

// Opt-in smoke checks use installed applications and public YouTube requests.
// Unit tests stay offline and do not require external programs.
func TestLiveMedia(t *testing.T) {
	if os.Getenv("GEKTOR_MEDIA_LIVE") != "1" {
		t.Skip("set GEKTOR_MEDIA_LIVE=1 for live compatibility checks")
	}
	s := testService(t)
	s.Apps = executables.New(userdirs.Sub("applications.json"))
	t.Run("applications", func(t *testing.T) {
		value, err := s.Status(t.Context(), true)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(value)
		t.Log(string(raw))
	})
	t.Run("local", func(t *testing.T) {
		if _, err := exec.LookPath("ffprobe"); err != nil {
			t.Skip("ffprobe unavailable")
		}
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("ffmpeg unavailable")
		}
		var wav bytes.Buffer
		wav.WriteString("RIFF")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(36+3200))
		wav.WriteString("WAVEfmt ")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16000))
		_ = binary.Write(&wav, binary.LittleEndian, uint32(32000))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(2))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(16))
		wav.WriteString("data")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(3200))
		wav.Write(make([]byte, 3200))
		if err := s.WS.WriteFile("tone.wav", wav.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		for _, call := range []struct {
			name string
			in   Request
		}{
			{"media_inspect", Request{Inputs: []string{"tone.wav"}}},
			{"media_convert", Request{Inputs: []string{"tone.wav"}, Path: "tone.flac", Format: "flac"}},
			{"media_clip", Request{Inputs: []string{"tone.wav"}, Path: "clip.wav", Format: "wav", DurationSeconds: 0.05}},
		} {
			out, err := s.Run(t.Context(), call.name, call.in)
			if err != nil {
				t.Fatal(call.name, err)
			}
			raw, _ := json.Marshal(out)
			t.Log(call.name, string(raw))
		}
		ff, err := s.Apps.Require(t.Context(), "ffmpeg")
		if err != nil {
			t.Fatal(err)
		}
		result, err := process.Run(t.Context(), process.Request{Program: ff.Path, Directory: s.WS.Root(), Timeout: 10 * time.Second, Args: []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-n", "-f", "lavfi", "-i", "color=size=16x16:rate=10", "-t", "0.1", "-c:v", "libx264", "-pix_fmt", "yuv420p", filepath.Join(s.WS.Root(), "video.mp4")}})
		if err != nil {
			t.Fatal(err, string(result.Stderr))
		}
		for _, call := range []struct {
			name string
			in   Request
		}{
			{"media_convert", Request{Inputs: []string{"video.mp4"}, Path: "remux.mp4", Format: "mp4", Copy: true}},
			{"media_inspect", Request{Inputs: []string{"remux.mp4"}}},
			{"media_frame", Request{Inputs: []string{"video.mp4"}, Path: "frame.png", Format: "png"}},
			{"media_merge", Request{Inputs: []string{"video.mp4", "tone.wav"}, Path: "merged.mkv", Format: "mkv"}},
		} {
			out, err := s.Run(t.Context(), call.name, call.in)
			if err != nil {
				t.Fatal(call.name, err)
			}
			raw, _ := json.Marshal(out)
			t.Log(call.name, string(raw))
		}

	})
	if os.Getenv("GEKTOR_MEDIA_LIVE_NETWORK") != "1" {
		return
	}
	for _, provider := range []string{"native", "yt-dlp"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			for _, name := range []string{"video_info", "video_transcript"} {
				out, err := s.Run(ctx, name, Request{URL: "https://www.youtube.com/watch?v=rFejpH_tAHM", Backend: provider, Limit: 2, TimeoutSeconds: 30})
				if err != nil {
					t.Errorf("compatibility failure (%s/%s): %v", provider, name, err)
					continue
				}
				raw, _ := json.Marshal(out)
				text := string(raw)
				if len(text) > 1200 {
					text = text[:1200]
				}
				t.Log(name, strings.TrimSpace(text))
			}
		})
	}
}

func TestLiveDownload(t *testing.T) {
	if os.Getenv("GEKTOR_MEDIA_LIVE_DOWNLOAD") != "1" {
		t.Skip("set GEKTOR_MEDIA_LIVE_DOWNLOAD=1 for short video download checks")
	}
	s := testService(t)
	s.Apps = executables.New(userdirs.Sub("applications.json"))
	for _, backend := range []string{"native", "yt-dlp"} {
		t.Run(backend, func(t *testing.T) {
			out, err := s.Run(t.Context(), "video_download", Request{URL: "https://www.youtube.com/watch?v=rFejpH_tAHM", Backend: backend, Mode: "audio", Path: backend + ".webm", MaxBytes: 16 << 20, TimeoutSeconds: 60})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(out)
			t.Log(string(raw))
		})
	}
}
