package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func ffmpegBase() []string { return []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-n"} }
func mediaInput(path string) []string {
	return []string{"-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,mp3,wav,ogg,flac,aac,avi,flv,mpeg,mpegts", "-i", path}
}
func (s *Service) local(ctx context.Context, name string, in LocalRequest) (any, error) {
	if name != "media_inspect" {
		if err := s.preflightOutput(in.Path); err != nil {
			return nil, err
		}
	}
	count := 1
	if name == "media_merge" {
		count = 2
	}
	if len(in.Inputs) != count {
		return nil, fmt.Errorf("%s requires %d input files", name, count)
	}
	app := "ffmpeg"
	if name == "media_inspect" {
		app = "ffprobe"
	}
	if _, err := s.Apps.Require(ctx, app); err != nil {
		return nil, err
	}
	dir, err := s.work()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	files := []string{}
	for i, path := range in.Inputs {
		file, err := s.stageInput(ctx, path, dir, i)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if name == "media_inspect" {
		args := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,mp3,wav,ogg,flac,aac,avi,flv,mpeg,mpegts", "-show_entries", "format=format_name,duration,size,bit_rate:stream=index,codec_name,codec_type,width,height,sample_rate,channels,duration", "-of", "json", files[0]}
		result, err := s.command(ctx, app, args, dir, 3*MaxBytes)
		if err != nil {
			return nil, err
		}
		if len(result.Stdout) > MaxText {
			return nil, fmt.Errorf("media metadata exceeds 32 KiB")
		}
		var data json.RawMessage
		if err := json.Unmarshal(result.Stdout, &data); err != nil {
			return nil, err
		}
		return map[string]any{"backend": "ffprobe", "path": in.Inputs[0], "metadata": data}, nil
	}
	format := in.Format
	if format == "" {
		format = "mkv"
		if name == "media_frame" {
			format = "png"
		}
	}
	if extensionFormat(format) == "" {
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
	if name == "media_merge" && (format != "mkv" && format != "mp4" && format != "webm") {
		return nil, fmt.Errorf("merge format must be mkv, mp4 or webm")
	}
	if name == "media_frame" && format != "png" && format != "jpg" {
		return nil, fmt.Errorf("frame format must be png or jpg")
	}
	if name == "media_clip" && in.DurationSeconds <= 0 {
		return nil, fmt.Errorf("clip requires duration_seconds > 0")
	}
	out := filepath.Join(dir, "result."+format)
	args := ffmpegBase()
	for _, file := range files {
		args = append(args, mediaInput(file)...)
	}
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	if in.StartSeconds > 0 {
		args = append(args, "-ss", number(in.StartSeconds))
	}
	if name == "media_frame" {
		args = append(args, "-map", "0:v:0", "-frames:v", "1", "-update", "1")
	} else if name == "media_convert" && in.Copy {
		args = append(args, "-map", "0:v:0?", "-map", "0:a:0?", "-c", "copy")
	} else if name == "media_merge" {
		args = append(args, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy")
	} else {
		if in.DurationSeconds > 0 {
			args = append(args, "-t", number(in.DurationSeconds))
		}
		switch format {
		case "mp3":
			args = append(args, "-vn", "-c:a", "libmp3lame")
		case "m4a":
			args = append(args, "-vn", "-c:a", "aac")
		case "wav":
			args = append(args, "-vn", "-c:a", "pcm_s16le")
		case "flac":
			args = append(args, "-vn", "-c:a", "flac")
		case "webm":
			args = append(args, "-c:v", "libvpx-vp9", "-c:a", "libopus")
		case "mp4", "mkv":
			args = append(args, "-c:v", "libx264", "-c:a", "aac")
		default:
			return nil, fmt.Errorf("convert/clip requires an audio or video format")
		}
	}
	args = append(args, "-fs", fmt.Sprint(in.MaxBytes), out)
	if _, err := s.command(ctx, "ffmpeg", args, dir, 3*MaxBytes); err != nil {
		return nil, err
	}
	return s.publishEncoded(ctx, outputTarget{Path: in.Path, MaxBytes: in.MaxBytes}, out, "ffmpeg")
}
