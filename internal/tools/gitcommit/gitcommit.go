package gitcommit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct{ WS *workspace.Workspace }

const gitAttribution = "Co-authored-by: GEKTOR <no-reply@keshon.ru>"

func (Tool) Name() string         { return "git_commit" }
func (Tool) Mode() agent.ToolMode { return agent.Exclusive }
func (Tool) Description() string {
	return "Commit the already-staged changes with a message and GEKTOR co-author credit. Inspect the staged diff first; unrelated staged changes would be included. Requires the user's authorization to commit. Git author identity and hooks come from the repository configuration."
}
func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","minLength":1}},"required":["message"],"additionalProperties":false}`)
}
func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := agent.RejectUnknownFields(args, "message"); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Message) == "" {
		return "", fmt.Errorf("message is required")
	}
	if err := guardMessageSize(in.Message); err != nil {
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
	return commandrun.Git(ctx, t.WS.Root(), "commit", "--file", f.Name())
}

func guardMessageSize(message string) error {
	const limit = 1024 * 1024
	if len(message) > limit {
		return fmt.Errorf("git_commit: content %d bytes exceeds write cap %d — split the write", len(message), limit)
	}
	return nil
}
