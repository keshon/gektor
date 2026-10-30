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

const gitTimeout = 60 * time.Second
const gitMaxItems = 50

// Git inspects repositories. Mutations have separate names so Plan and
// read-only workers can exclude them before execution, independently of JSON.
type Git struct{ WS *workspace.Workspace }

func (Git) Name() string         { return "git" }
func (Git) Mode() agent.ToolMode { return agent.Concurrent }
func (Git) Description() string {
	return "Inspect Git status/log/diff/show/blame/ls-files. Paths are literal, workspace-relative; no globs/shell syntax. diff/show include patches; staged=true selects the index. log caps at 50 commits; output is bounded. Authorized mutations use git_add/git_commit."
}
func (Git) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "subcommand":{"type":"string","enum":["status","log","diff","show","blame","ls-files"]},
            "path":{"type":"string","description":"literal workspace-relative path; required for blame"},
            "ref":{"type":"string","description":"commit revision for log/diff/show; required for show"},
            "max_items":{"type":"integer","minimum":1,"maximum":50,"description":"log only; default 50"},
            "staged":{"type":"boolean","description":"diff only: inspect the index"}
        },
        "required":["subcommand"],"additionalProperties":false
    }`)
}

func (t Git) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Subcommand string `json:"subcommand"`
		Path       string `json:"path"`
		Ref        string `json:"ref"`
		MaxItems   int    `json:"max_items"`
		Staged     bool   `json:"staged"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "subcommand", "path", "ref", "max_items", "staged"); err != nil {
		return "", err
	}
	switch in.Subcommand {
	case "status", "log", "diff", "show", "blame", "ls-files":
	default:
		return "", fmt.Errorf("unknown read-only git subcommand %q", in.Subcommand)
	}
	if in.MaxItems < 0 || (in.MaxItems != 0 && in.Subcommand != "log") {
		return "", fmt.Errorf("max_items is a positive log-only limit")
	}
	if in.Staged && in.Subcommand != "diff" {
		return "", fmt.Errorf("staged is supported only for diff")
	}
	if in.Ref != "" && in.Subcommand != "log" && in.Subcommand != "diff" && in.Subcommand != "show" {
		return "", fmt.Errorf("ref is supported only for log, diff and show")
	}
	if in.Subcommand == "show" && strings.TrimSpace(in.Ref) == "" {
		return "", fmt.Errorf("ref is required for show")
	}
	if in.Subcommand == "blame" && strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required for blame")
	}
	path := "."
	if in.Path != "" {
		if _, err := t.WS.Resolve(in.Path); err != nil {
			return "", err
		}
		path = in.Path
	}
	var parts []string
	switch in.Subcommand {
	case "status":
		parts = []string{"status", "--porcelain"}
	case "log":
		n := in.MaxItems
		if n == 0 {
			n = gitMaxItems
		}
		parts = []string{"log", "--oneline", fmt.Sprintf("--max-count=%d", min(n, gitMaxItems))}
	case "diff":
		parts = []string{"diff", "--no-ext-diff", "--no-textconv", "--stat", "--patch"}
		if in.Staged {
			parts = append(parts, "--cached")
		}
	case "show":
		parts = []string{"show", "--no-ext-diff", "--no-textconv", "--format=fuller", "--stat", "--patch"}
	case "blame":
		parts = []string{"blame", "--no-textconv"}
	case "ls-files":
		parts = []string{"ls-files"}
	}
	if in.Ref != "" {
		// Resolve only commits: option-like strings and ref:path blob selectors
		// must not become options or bypass the path scope of an inspection.
		oid, err := runGit(ctx, t.WS, "rev-parse", "--verify", "--end-of-options", in.Ref+"^{commit}")
		if err != nil {
			return oid, err
		}
		parts = append(parts, strings.TrimSpace(oid))
	}
	parts = append(parts, "--", path)
	return runGit(ctx, t.WS, parts...)
}

func runGit(ctx context.Context, ws *workspace.Workspace, args ...string) (string, error) {
	prefix := []string{"--no-pager", "--literal-pathspecs", "-c", "color.ui=false", "-c", "core.quotePath=false", "-c", "core.fsmonitor=false"}
	out, err := runProgram(ctx, "git", append(prefix, args...), ws.Root(), gitTimeout)
	out = strings.TrimRight(out, "\r\n")
	if err != nil {
		return out, fmt.Errorf("git failed: %w", err)
	}
	return out, nil
}
