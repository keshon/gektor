package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

type Check struct {
	Type     string `json:"type"`               // shell, go_acceptance, file_exists, file_absent, content_contains, http, none
	Cmd      string `json:"cmd,omitempty"`      // shell: the command line
	Path     string `json:"path,omitempty"`     // file_exists / file_absent / content_contains
	Contains string `json:"contains,omitempty"` // content_contains: substring that must appear
	URL      string `json:"url,omitempty"`      // http: expect a 200 response
}

const checkShellTimeout = 120 * time.Second

// ValidateCheck catches invalid frozen criteria before model calls.
func ValidateCheck(c Check) error {
	switch c.Type {
	case "", "none":
		return nil
	case "file_exists", "file_absent":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("type %q needs path", c.Type)
		}
		return nil
	case "content_contains":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("type %q needs path", c.Type)
		}
		if strings.TrimSpace(c.Contains) == "" {
			return fmt.Errorf("type %q needs non-blank contains (blank needles fail at runtime)", c.Type)
		}
		return nil
	case "shell":
		if strings.TrimSpace(c.Cmd) == "" {
			return fmt.Errorf("type %q needs cmd", c.Type)
		}
		return nil
	case "go_acceptance":
		if !strings.HasPrefix(filepath.ToSlash(filepath.Clean(c.Path)), "eval/verifiers/") || !strings.HasSuffix(c.Path, ".gotest") {
			return fmt.Errorf("go_acceptance needs an eval/verifiers/*.gotest source")
		}
		if _, err := os.Stat(c.Path); err != nil {
			return err
		}
		return nil
	case "http":
		if strings.TrimSpace(c.URL) == "" {
			return fmt.Errorf("type %q needs url", c.Type)
		}
		return nil
	default:
		return fmt.Errorf("unknown check type %q", c.Type)
	}
}

func RunCheck(ctx context.Context, c Check, ws *workspace.Workspace) (output string, ok bool) {
	switch c.Type {
	case "", "none":
		return "(no check declared)", true

	case "file_exists":
		full, err := ws.Resolve(c.Path)
		if err != nil {
			return fmt.Sprintf("check path rejected: %v", err), false
		}
		info, err := os.Stat(full)
		if err != nil {
			return fmt.Sprintf("file %s does not exist", c.Path), false
		}
		if info.IsDir() {
			return fmt.Sprintf("%s exists but is a directory, not a file", c.Path), false
		}
		if info.Size() == 0 {
			// A weak model "completing" a subtask by writing an empty file
			// is a known failure shape — existence alone proves nothing.
			return fmt.Sprintf("file %s exists but is empty (0 bytes)", c.Path), false
		}
		return fmt.Sprintf("file %s exists (%d bytes)", c.Path, info.Size()), true

	case "file_absent":
		// The mirror of file_exists for deletions and renames: proves the
		// old path is gone rather than shelling out to `if exist` (cmd)
		// or `[ ! -f ]` (sh), neither of which is portable.
		full, err := ws.Resolve(c.Path)
		if err != nil {
			return fmt.Sprintf("check path rejected: %v", err), false
		}
		if _, err := os.Stat(full); err == nil {
			return fmt.Sprintf("file %s still exists", c.Path), false
		} else if !os.IsNotExist(err) {
			return fmt.Sprintf("cannot check absence of %s: %v", c.Path, err), false
		}
		return fmt.Sprintf("file %s is absent", c.Path), true

	case "content_contains":
		// Stronger than file_exists: proves a distinctive symbol from the
		// acceptance criteria actually landed. Caps the read so a huge
		// file can't blow a fix-worker prompt.
		needle := strings.TrimSpace(strings.Trim(c.Contains, `"'`))
		if needle == "" {
			return "content_contains check has empty contains", false
		}
		full, err := ws.Resolve(c.Path)
		if err != nil {
			return fmt.Sprintf("check path rejected: %v", err), false
		}
		const maxRead = 256 * 1024
		f, err := os.Open(full)
		if err != nil {
			return fmt.Sprintf("cannot read %s: %v", c.Path, err), false
		}
		defer f.Close()
		body, err := io.ReadAll(io.LimitReader(f, maxRead))
		if err != nil {
			return fmt.Sprintf("cannot read %s: %v", c.Path, err), false
		}
		if !strings.Contains(string(body), needle) {
			return fmt.Sprintf("file %s (%d bytes) does not contain %q", c.Path, len(body), needle), false
		}
		return fmt.Sprintf("file %s contains %q", c.Path, needle), true

	case "shell":
		out, err := RunShellCommand(ctx, c.Cmd, ws.Root())
		if err != nil {
			return fmt.Sprintf("command failed: %v\n%s", err, out), false
		}
		if out == "" {
			out = "(no output)"
		}
		return out, true
	case "go_acceptance":
		body, err := os.ReadFile(c.Path)
		if err != nil {
			return err.Error(), false
		}
		target := "gektor_acceptance_" + rand.Text() + "_test.go"
		if err := ws.WriteFile(target, body, 0600); err != nil {
			return err.Error(), false
		}
		defer ws.Remove(target)
		out, err := RunShellCommand(ctx, "go test ./...", ws.Root())
		if err != nil {
			return fmt.Sprintf("acceptance failed: %v\n%s", err, out), false
		}
		return out, true

	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		if err != nil {
			return fmt.Sprintf("bad check URL: %v", err), false
		}
		client := tools.NetworkClient(true)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Sprintf("GET %s failed: %v", c.URL, err), false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Sprintf("GET %s returned %s, want 200", c.URL, resp.Status), false
		}
		return fmt.Sprintf("GET %s returned 200", c.URL), true

	default:
		return fmt.Sprintf("unknown check type %q", c.Type), false
	}
}

// RunShellCommand uses the same bounded shell runner as the agent tools.
func RunShellCommand(ctx context.Context, command, dir string) (string, error) {
	return tools.RunCommand(ctx, command, dir, checkShellTimeout)
}
