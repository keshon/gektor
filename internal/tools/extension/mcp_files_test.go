package extension

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
)

func TestMCPFileArgumentSendsBytesWithoutReturningThem(t *testing.T) {
	directory := t.TempDir()
	contents := []byte("private image bytes")
	err := os.WriteFile(filepath.Join(directory, "cover image.webp"), contents, 0600)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	client := contractMCP(func(_ context.Context, method string, params any, out any) error {
		called++
		request := params.(map[string]any)
		arguments := request["arguments"].(map[string]any)
		if method != "tools/call" || request["name"] != "add_media" || arguments["base64"] != base64.StdEncoding.EncodeToString(contents) {
			t.Fatalf("wrong upload: %v", params)
		}
		if _, found := arguments["base64_file"]; found {
			t.Fatal("local path was sent to remote server")
		}
		return json.Unmarshal([]byte(`{"structuredContent":{"url":"/media/test/cover.webp"},"content":[{"type":"text","text":"{ \"url\": \"/media/test/cover.webp\" }"}]}`), out)
	})
	tool := &MCPTool{workspace: directory, client: client, server: "test", toolName: "mcp__test__add_media", def: mcp.ToolDefinition{Name: "add_media", InputSchema: json.RawMessage(`{"type":"object","properties":{"slug":{"type":"string"},"base64":{"type":"string"}},"required":["slug","base64"],"additionalProperties":false}`)}}
	args := json.RawMessage(`{"slug":"test","base64_file":"cover image.webp"}`)
	registry := agent.NewRegistry(tool)
	err = registry.ValidateCall(tool.Name(), args)
	if err != nil {
		t.Fatalf("file alternative schema: %s %v", tool.Schema(), err)
	}
	for _, invalid := range []string{`{"slug":"test"}`, `{"slug":"test","base64":"x","base64_file":"cover image.webp"}`} {
		if registry.ValidateCall(tool.Name(), json.RawMessage(invalid)) == nil {
			t.Fatalf("accepted invalid alternative %s", invalid)
		}
	}
	resources, err := tool.Resources(args)
	if err != nil || len(resources) != 1 || resources[0] != filepath.Join(directory, "cover image.webp") {
		t.Fatal(resources, err)
	}
	output, err := tool.Run(t.Context(), args)
	if err != nil || called != 1 || strings.Count(output, "cover.webp") != 1 || strings.Contains(output, "private") || strings.Contains(output, "base64") {
		t.Fatal(output, err, called)
	}
	for _, invalid := range []string{`{"base64_file":"../secret"}`, `{"base64_file":5}`, `{"base64_file":""}`, `{"base64_file":"missing"}`, `{"base64":"x","base64_file":"cover image.webp"}`} {
		_, err = tool.Run(t.Context(), json.RawMessage(invalid))
		if err == nil || called != 1 {
			t.Fatalf("invalid file reached transport: %s %v", invalid, err)
		}
	}
	large, err := os.Create(filepath.Join(directory, "large.webp"))
	if err != nil {
		t.Fatal(err)
	}
	err = large.Truncate(mcpFileMax + 1)
	closeErr := large.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	_, err = tool.Run(t.Context(), json.RawMessage(`{"base64_file":"large.webp"}`))
	if err == nil || called != 1 {
		t.Fatal("oversized file reached transport", err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	err = os.WriteFile(outside, contents, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Symlink(outside, filepath.Join(directory, "outside.webp"))
	if err == nil {
		_, err = tool.Run(t.Context(), json.RawMessage(`{"base64_file":"outside.webp"}`))
		if err == nil || called != 1 {
			t.Fatal("outside symlink reached transport", err)
		}
	}
}

func TestMCPFileResourcesSurviveCapabilityWrapper(t *testing.T) {
	directory := t.TempDir()
	remote := &MCPTool{workspace: directory, toolName: "mcp__test__upload", def: mcp.ToolDefinition{InputSchema: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string","contentEncoding":"base64"}}}`)}}
	err := agent.ValidateToolSchema(remote.Schema())
	if err != nil {
		t.Fatal(err)
	}
	source := &Capability{workspace: directory, ResourceField: "slug"}
	tool := &capabilityTool{source: source}
	tool.def.Name = remote.Name()
	tool.def.Parameters = remote.Schema()
	resources, err := tool.Resources(json.RawMessage(`{"slug":"article","payload_file":"cover.webp"}`))
	if err != nil || len(resources) != 2 || resources[0] != filepath.Join(directory, "cover.webp") || resources[1] != "article" {
		t.Fatal(resources, err)
	}
}
