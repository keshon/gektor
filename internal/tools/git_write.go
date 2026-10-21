package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// GitAdd and GitCommit are deliberately absent from the read-only registry.
type GitAdd struct{ WS *workspace.Workspace }
type GitCommit struct{ WS *workspace.Workspace }

func (GitAdd) Name() string         { return "git_add" }
func (GitAdd) Mode() agent.ToolMode { return agent.Exclusive }
func (GitAdd) Description() string {
	return "Stage one literal workspace-relative file or directory, including deletions. Inspect status and diff first; stage only authorized changes."
}
func (GitAdd) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1}},"required":["path"],"additionalProperties":false}`)
}
func (t GitAdd) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := rejectUnknownFields(args, "path"); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if _, err := t.WS.Resolve(in.Path); err != nil {
		return "", err
	}
	return runGit(ctx, t.WS, "add", "--", in.Path)
}

const gitAttribution = "Co-authored-by: Gektor <no-reply@keshon.ru>"

func (GitCommit) Name() string         { return "git_commit" }
func (GitCommit) Mode() agent.ToolMode { return agent.Exclusive }
func (GitCommit) Description() string {
	return "Commit the already-staged changes with a message and Gektor co-author credit. Inspect the staged diff first; unrelated staged changes would be included. Requires the user's authorization to commit. Git author identity and hooks come from the repository configuration."
}
func (GitCommit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","minLength":1}},"required":["message"],"additionalProperties":false}`)
}
func (t GitCommit) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := rejectUnknownFields(args, "message"); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Message) == "" {
		return "", fmt.Errorf("message is required")
	}
	if err := guardWriteSize("git_commit", in.Message); err != nil {
		return "", err
	}
	body := strings.TrimRight(in.Message, "\r\n")
	hasCredit := false
	for _, line := range strings.Split(body, "\n") {
		hasCredit = hasCredit || strings.EqualFold(strings.TrimSpace(line), gitAttribution)
	}
	if !hasCredit {
		body += "\n\n" + gitAttribution
	}
	// Outside the workspace so 'git add .' cannot stage it. Its lifetime is
	// the command's lifetime, including hooks, cancellation and errors.
	f, err := os.CreateTemp("", "gektor-commit-*.txt")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body + "\n"); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return runGit(ctx, t.WS, "commit", "--file", f.Name())
}
