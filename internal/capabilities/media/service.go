// Package media shares media operations between agent tools and workflow nodes.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/process"
	"github.com/keshon/gektor/internal/programs"
	"github.com/keshon/gektor/internal/workspace"
)

const MaxBytes int64 = 256 << 20
const MaxText = 32 << 10

type Request struct {
	URL             string   `json:"url,omitempty"`
	Backend         string   `json:"backend,omitempty"`
	Path            string   `json:"path,omitempty"`
	Inputs          []string `json:"inputs,omitempty"`
	Format          string   `json:"format,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Language        string   `json:"language,omitempty"`
	Offset          int      `json:"offset,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	StartSeconds    float64  `json:"start_seconds,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	MaxHeight       int      `json:"max_height,omitempty"`
	MaxBytes        int64    `json:"max_bytes,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
	Refresh         bool     `json:"refresh,omitempty"`
	Copy            bool     `json:"copy,omitempty"`
}
type ApplicationResolver interface {
	Probe(context.Context, string, bool) (programs.Status, error)
	Require(context.Context, string) (programs.Status, error)
}

type Service struct {
	WS        *workspace.Workspace
	Apps      ApplicationResolver
	transport http.RoundTripper
	run       func(context.Context, process.Request) (process.Result, error)
}

func New(ws *workspace.Workspace, apps ApplicationResolver) *Service {
	return &Service{WS: ws, Apps: apps}
}
func (s *Service) Validate(name string, in Request) (Request, error) {
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 120
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
	if strings.HasPrefix(name, "video_") {
		var err error
		in.URL, err = httptransport.Normalize(in.URL, false)
		if err != nil {
			return in, err
		}
		u, _ := url.Parse(in.URL)
		u.Fragment = ""
		in.URL = u.String()
		if in.Backend == "native" && !isYouTube(in.URL) {
			return in, fmt.Errorf("native backend supports YouTube URLs only")
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
			return in, fmt.Errorf("merged download path must end in .mp4, .mkv or .webm")
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
func isYouTube(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return u.Port() == ""
	}
	return false
}
func backend(in Request) string {
	if in.Backend == "auto" {
		if isYouTube(in.URL) {
			return "native"
		}
		return "yt-dlp"
	}
	return in.Backend
}
func (s *Service) Run(ctx context.Context, name string, in Request) (any, error) {
	in, err := s.Validate(name, in)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()
	switch name {
	case "media_status":
		return s.Status(ctx, in.Refresh)
	case "video_info", "video_transcript", "video_download":
		if name == "video_download" {
			if err := s.preflightOutput(in.Path); err != nil {
				return nil, err
			}
		}
		if backend(in) == "native" {
			return s.native(ctx, name, in)
		}
		return s.external(ctx, name, in)
	case "media_inspect", "media_convert", "media_clip", "media_frame", "media_merge":
		return s.local(ctx, name, in)
	}
	return nil, fmt.Errorf("unknown media operation %q", name)
}

type OperationStatus struct {
	Name    string   `json:"operation"`
	Backend string   `json:"backend"`
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"`
}

func (s *Service) Status(ctx context.Context, refresh bool) (any, error) {
	apps := []programs.Status{}
	states := map[string]string{}
	for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe", "deno", "node"} {
		v, err := s.Apps.Probe(ctx, name, refresh)
		if err != nil {
			return nil, err
		}
		apps = append(apps, v)
		states[name] = v.State
	}
	operations := []OperationStatus{}
	add := func(name, provider string, dependencies ...string) {
		item := OperationStatus{Name: name, Backend: provider, State: "ready"}
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
	return struct {
		Native       bool              `json:"native_youtube"`
		Preferred    string            `json:"youtube_preferred_backend"`
		Applications []programs.Status `json:"applications"`
		Operations   []OperationStatus `json:"operations"`
		Note         string            `json:"note"`
	}{true, "native", apps, operations, "Local requirements only, not execution permission or a remote/codec compatibility test. yt-dlp's EJS component is bundled or separately installed; its presence is not verified here. No installation or hidden fallback."}, nil
}
func (s *Service) command(ctx context.Context, app string, args []string, dir string, limit int64) (process.Result, error) {
	status, err := s.Apps.Require(ctx, app)
	if err != nil {
		return process.Result{}, err
	}
	run := s.run
	if run == nil {
		run = process.Run
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	observed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				observed <- nil
				return
			case <-child.Done():
				observed <- nil
				return
			case <-ticker.C:
				var total int64
				err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						if os.IsNotExist(err) {
							return nil
						}
						return err
					}
					if d.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("unexpected link in media work directory")
					}
					if !d.IsDir() {
						info, err := d.Info()
						if err != nil {
							if os.IsNotExist(err) {
								return nil
							}
							return err
						}
						total += info.Size()
					}
					return nil
				})
				if err != nil || total > limit {
					cancel()
					if err == nil {
						err = fmt.Errorf("media temporary data exceeded %d bytes", limit)
					}
					observed <- err
					return
				}
			}
		}
	}()
	result, runErr := run(child, process.Request{Program: status.Path, Args: args, Directory: dir, Timeout: 10 * time.Minute})
	close(done)
	if err := <-observed; err != nil {
		return result, err
	}
	if runErr != nil {
		return result, fmt.Errorf("%s: %w; %s", app, runErr, brief(result.Stderr, 2048))
	}
	return result, nil
}
func brief(raw []byte, n int) string {
	if len(raw) > n {
		raw = raw[:n]
	}
	return strings.TrimSpace(string(raw))
}
func (s *Service) work() (string, error) { return os.MkdirTemp("", "gektor-media-") }
func (s *Service) stageInput(ctx context.Context, path, dir string, index int) (string, error) {
	f, err := s.WS.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", fmt.Errorf("media input must be a regular file up to 256 MiB")
	}
	target := filepath.Join(dir, fmt.Sprintf("input-%d%s", index, filepath.Ext(path)))
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = copyBounded(ctx, out, f, MaxBytes)
	return target, err
}
func copyBounded(ctx context.Context, out io.Writer, in io.Reader, limit int64) (int64, error) {
	reader := &contextReader{ctx: ctx, r: in}
	n, err := io.Copy(out, io.LimitReader(reader, limit+1))
	if n > limit {
		return n, fmt.Errorf("media exceeds %d bytes", limit)
	}
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func (s *Service) publishEncoded(ctx context.Context, in Request, file, provider string) (any, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if info.Size() >= in.MaxBytes {
		return nil, fmt.Errorf("output reached max_bytes; refusing a possibly truncated result")
	}
	return s.publish(ctx, in, file, provider)
}
func (s *Service) publish(ctx context.Context, in Request, file, provider string) (result any, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if containerExtension(in.Path) != containerExtension(file) {
		return nil, fmt.Errorf("output container is %s but path ends in %s; choose a matching path or explicitly convert", filepath.Ext(file), filepath.Ext(in.Path))
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > in.MaxBytes {
		return nil, fmt.Errorf("empty, nonregular or oversized media result")
	}
	out, err := s.WS.CreateExclusive(in.Path, 0600)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			if e := s.WS.Remove(in.Path); e != nil && !os.IsNotExist(e) {
				err = errors.Join(err, fmt.Errorf("remove incomplete media output: %w", e))
			}
		}
	}()
	hash := sha256.New()
	size, err := copyBounded(ctx, io.MultiWriter(out, hash), f, in.MaxBytes)
	if err != nil {
		return nil, err
	}
	if err := out.Sync(); err != nil {
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ok = true
	return map[string]any{"path": in.Path, "size": size, "sha256": hex.EncodeToString(hash.Sum(nil)), "backend": provider, "container": strings.TrimPrefix(filepath.Ext(file), ".")}, nil
}
func (s *Service) preflightOutput(path string) error {
	if path == "" {
		return fmt.Errorf("output path is required")
	}
	parent, err := s.WS.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return fmt.Errorf("output parent must be a directory")
	}
	_, err = s.WS.Stat(path)
	if err == nil {
		return fmt.Errorf("destination exists")
	}
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("media data exceeds %d bytes", limit)
	}
	return raw, nil
}

// MP4 and M4A share a container; M4A names audio-only MP4 files.
func containerExtension(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch ext {
	case "m4a":
		return "mp4"
	case "jpeg":
		return "jpg"
	}
	return ext
}
