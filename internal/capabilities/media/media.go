// Package media shares media operations between agent tools and workflow nodes.
package media

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/process"
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
	Probe(context.Context, string, bool) (executables.Status, error)
	Require(context.Context, string) (executables.Status, error)
}

// Service borrows its workspace and executable resolver for media operations.
// Configure its dependencies before use; callers own their lifetimes.
type Service struct {
	WS        *workspace.Workspace
	Apps      ApplicationResolver
	transport http.RoundTripper
	run       func(context.Context, process.Request) (process.Result, error)
}

func New(ws *workspace.Workspace, apps ApplicationResolver) *Service {
	return &Service{WS: ws, Apps: apps}
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
