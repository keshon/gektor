// Package executables resolves operator-configured executables without installing them.
package executables

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/process"
)

type Setting struct {
	Path     string `json:"path,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}
type Config struct {
	Applications map[string]Setting `json:"applications"`
}
type Spec struct {
	Name        string
	VersionArgs []string
}

var specs = map[string]Spec{
	"ffmpeg":  {"ffmpeg", []string{"-version"}},
	"ffprobe": {"ffprobe", []string{"-version"}},
	"yt-dlp":  {"yt-dlp", []string{"--ignore-config", "--version"}},
	"deno":    {"deno", []string{"--version"}},
	"node":    {"node", []string{"--version"}},
}

type Status struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
type cached struct {
	status Status
	until  time.Time
}
type Resolver struct {
	ConfigPath string
	mu         sync.Mutex
	cache      map[string]cached
	run        func(context.Context, process.Request) (process.Result, error)
}

func New(configPath string) *Resolver { return &Resolver{ConfigPath: configPath} }

// Load reads preferences without resolving or running executables.
func (r *Resolver) Load() (Config, error) {
	var c Config
	if r.ConfigPath == "" {
		return c, nil
	}

	f, err := os.Open(r.ConfigPath)
	if os.IsNotExist(err) {
		return c, nil
	}

	if err != nil {
		return c, err
	}

	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil {
		return c, err
	}

	if len(raw) > 32768 {
		return c, fmt.Errorf("applications.json exceeds 32 KiB")
	}

	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}

	if d.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("expected one applications config")
	}

	for name := range c.Applications {
		if _, ok := specs[name]; !ok {
			return c, fmt.Errorf("unknown application %q", name)
		}
	}

	return c, nil
}

// Probe runs only a bounded version command. Refresh bypasses cached observations.
func (r *Resolver) Probe(ctx context.Context, name string, refresh bool) (Status, error) {
	spec, ok := specs[name]
	if !ok {
		return Status{}, fmt.Errorf("unknown application %q", name)
	}

	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	c, err := r.Load()
	if err != nil {
		return Status{}, fmt.Errorf("application settings: %w", err)
	}

	setting := c.Applications[name]
	pathEnv := os.Getenv("PATH")
	key := name + "\x00" + setting.Path + "\x00" + fmt.Sprint(setting.Disabled) + "\x00" + pathEnv
	r.mu.Lock()
	if !refresh {
		if v, ok := r.cache[key]; ok && time.Now().Before(v.until) {
			r.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return Status{}, err
			}

			return v.status, nil
		}
	}

	r.mu.Unlock()
	s := Status{Name: name, State: "missing"}

	switch {
	case setting.Disabled:
		s.State = "disabled"
		s.Reason = "Disabled in application settings"
	default:
		path := setting.Path
		if path != "" && !filepath.IsAbs(path) {
			return s, fmt.Errorf("configured %s path must be absolute", name)
		}

		if path == "" {
			path, err = exec.LookPath(spec.Name)
		}

		if err != nil {
			s.Reason = "Executable not found on PATH"
		} else if !filepath.IsAbs(path) {
			s.State = "unusable"
			s.Reason = "Executable must resolve outside the current directory to an absolute path"
		} else {
			s.Path = filepath.Clean(path)
			ext := strings.ToLower(filepath.Ext(path))
			info, statErr := os.Stat(path)
			if statErr != nil || !info.Mode().IsRegular() {
				s.State = "unusable"
				s.Reason = "Configured executable is not a regular file"
			} else if runtime.GOOS == "windows" && ext != ".exe" {
				s.State = "unusable"
				s.Reason = "A native .exe is required"
			} else {
				run := r.run
				if run == nil {
					run = process.Run
				}

				result, runErr := run(ctx, process.Request{
					Program:   path,
					Args:      spec.VersionArgs,
					Directory: os.TempDir(),
					Timeout:   3 * time.Second,
				})
				if runErr != nil {
					s.State = "unusable"
					s.Reason = "Version probe failed: " + runErr.Error()
				} else {
					line := strings.TrimSpace(string(result.Stdout))
					if line == "" {
						line = strings.TrimSpace(string(result.Stderr))
					}

					line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
					if len(line) > 256 {
						line = line[:256]
					}

					if line == "" {
						s.State = "unusable"
						s.Reason = "Version command returned no version"
					} else {
						s.State = "ready"
						s.Version = line
					}
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cache == nil {
		r.cache = map[string]cached{}
	}

	if len(r.cache) > 32 {
		clear(r.cache)
	}

	r.cache[key] = cached{s, time.Now().Add(time.Minute)}

	return s, nil
}
func (r *Resolver) Require(ctx context.Context, name string) (Status, error) {
	s, err := r.Probe(ctx, name, true)
	if err != nil {
		return s, err
	}

	if s.State != "ready" {
		return s, fmt.Errorf("application %s is %s: %s; configure ~/.gektor/applications.json or PATH", name, s.State, s.Reason)
	}

	return s, nil
}
