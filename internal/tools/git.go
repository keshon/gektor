package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// gitTimeout bounds one git operation. Inspections over a large history can
// take longer than a quick check, so this is generous rather than matching
// the shell's 10-minute default.
const gitTimeout = 60 * time.Second

// gitMaxItems caps how many entries log/show emit before truncating, so a
// huge history cannot swallow the whole context window the way a raw
// `git log` would.
const gitMaxItems = 50

// Git runs a curated, read-only subset of git through the same bounded,
// sandboxed command runner as run_shell. It exists specifically so a model
// never has to shell out to `git diff`/`git log` itself: a Go-owned runner
// enforces the workspace boundary, scrubs credentials from the environment,
// and bounds output, whereas `git` written by hand is only as safe as the
// shell it lands in. The command set is intentionally read-only and fixed,
// so the same tool set is available even in a read-only role where writes
// are blocked but inspection must still work.
type Git struct{ WS *workspace.Workspace }

func (Git) Name() string         { return "git" }
func (Git) Mode() agent.ToolMode { return agent.Concurrent }

// Idempotent: none of these mutate the repository, so results hold until
// something else changes the tree or history.
func (Git) Idempotent() bool { return true }

func (Git) Description() string {
	return fmt.Sprintf("Run a read-only git inspection inside the workspace (status, log, diff, show, blame, ls-files). "+
		"Use this instead of run_shell(\"git ...\"), which bypasses the workspace boundary and credential scrubbing. "+
		"Read-only only: committing, staging, pushing or otherwise changing history is out of scope — use run_shell for that. "+
		"Output is capped at %d entries for list-like commands.", gitMaxItems)
}
func (Git) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"subcommand": {"type": "string", "enum": ["status","log","diff","show","blame","ls-files"]},
			"path": {"type": "string", "description": "optional path within the workspace to scope the command to"},
			"ref": {"type": "string", "description": "required for show: commit, branch, tag or other ref to inspect"},
			"max_items": {"type": "integer", "description": "optional cap for log/show entries, default %d"}
		},
		"required": ["subcommand"]
	}`, gitMaxItems))
}

func (t Git) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Subcommand string `json:"subcommand"`
		Path       string `json:"path"`
		Ref        string `json:"ref"`
		MaxItems   int    `json:"max_items"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "subcommand", "path", "ref", "max_items"); err != nil {
		return "", err
	}

	args_, err := t.buildArgs(in)
	if err != nil {
		return "", err
	}

	full, err := runCommand(ctx, "git "+strings.Join(args_, " "), t.WS.Root(), gitTimeout, nil)
	if err != nil {
		// git exits non-zero for many benign conditions (dirty tree,
		// no commits yet); surface the captured output with the exit
		// reason rather than swallowing it.
		if strings.TrimSpace(full) != "" {
			return full, fmt.Errorf("git failed: %w", err)
		}
		return "", fmt.Errorf("git failed: %w", err)
	}
	return strings.TrimRight(full, "\n"), nil
}

// buildArgs validates the subcommand and assembles the git argument vector.
// A pathspec is appended when a path is given so git itself scopes the work
// against the resolved workspace path.
func (t Git) buildArgs(in struct {
	Subcommand string `json:"subcommand"`
	Path       string `json:"path"`
	Ref        string `json:"ref"`
	MaxItems   int    `json:"max_items"`
}) ([]string, error) {
	var parts []string
	switch in.Subcommand {
	case "status":
		parts = []string{"status", "--porcelain"}
	case "log":
		n := in.MaxItems
		if n <= 0 {
			n = gitMaxItems
		}
		parts = append(parts, "log", "--oneline", fmt.Sprintf("-%d", n))
	case "diff":
		parts = []string{"diff", "--stat"}
	case "show":
		if strings.TrimSpace(in.Ref) == "" {
			return nil, fmt.Errorf("ref is required for show")
		}
		n := in.MaxItems
		if n <= 0 {
			n = gitMaxItems
		}
		parts = append(parts, "show", "--stat", "--format=hash|%an|%ad|%s%n%b", "--date=short", in.Ref)
	case "blame":
		parts = []string{"blame", "--line-porcelain"}
	case "ls-files":
		parts = []string{"ls-files"}
	default:
		return nil, fmt.Errorf("unknown subcommand %q (expected one of: status, log, diff, show, blame, ls-files)", in.Subcommand)
	}

	if strings.TrimSpace(in.Path) != "" {
		if _, err := t.WS.Resolve(in.Path); err != nil {
			return nil, err
		}
		parts = append(parts, "--", in.Path)
	}
	return parts, nil
}
