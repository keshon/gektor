package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func TestGuideReuseRejectsIncompleteAndUntrustedProof(t *testing.T) {
	root, library := t.TempDir(), t.TempDir()
	path := filepath.Join(library, "release", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: release\ndescription: Release\n---\nFollow these instructions."
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewWithSkillRoots(root, []string{library})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	tool := LoadSkill{WS: ws}
	args := json.RawMessage(`{"id":"release"}`)
	full, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	failed, success := true, false
	call := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "guide", Name: "load_skill", Arguments: args}}}
	result := llm.Message{Role: llm.RoleTool, ToolCallID: "guide", Content: full, ToolFailed: &success}
	for _, variant := range []string{"user prose", "failed", "unknown status", "header only", "truncated", "other tool", "wrong id", "resource"} {
		history := []llm.Message{call, result}
		currentArgs := args
		current := full
		switch variant {
		case "user prose":
			history = []llm.Message{{Role: llm.RoleUser, Content: full}}
		case "failed":
			history[1].ToolFailed = &failed
		case "unknown status":
			history[1].ToolFailed = nil
		case "header only":
			history[1].Content = strings.Split(full, "\n\n")[0]
		case "truncated":
			current = full[:len(full)-3]
			history[1].Content = current
		case "other tool":
			history[0].ToolCalls = []llm.ToolCall{{ID: "guide", Name: "read_file", Arguments: args}}
		case "wrong id":
			currentArgs = json.RawMessage(`{"id":"other"}`)
		case "resource":
			currentArgs = json.RawMessage(`{"id":"release","resource":"reference.txt"}`)
		}
		if got := tool.ReuseInContext(currentArgs, current, history); got != current {
			t.Fatal("false loaded state", variant, got)
		}
	}
	history := []llm.Message{call, result}
	if got := tool.ReuseInContext(args, full, history); got == full {
		t.Fatal("complete guide not reused")
	}
	if err := os.WriteFile(path, []byte(body+"\nEdited."), 0600); err != nil {
		t.Fatal(err)
	}
	edited, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	if got := tool.ReuseInContext(args, edited, history); got != edited {
		t.Fatal("edited guide reused old body")
	}
	projectPath := filepath.Join(root, "skills", "release", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	override, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	if got := tool.ReuseInContext(args, override, history); got != override {
		t.Fatal("override reused different source")
	}
}
