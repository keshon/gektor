package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// CapabilityConfig is explicitly selected by -capabilities. Workspace files
// never install or authorize executable extensions merely by being present.
type CapabilityConfig struct {
	Version    int              `json:"version"`
	SkillRoots []string         `json:"skill_roots,omitempty"`
	Packs      []CapabilityPack `json:"packs"`
}

type CapabilityPack struct {
	ID             string              `json:"id"`
	Version        string              `json:"version"`
	Description    string              `json:"description"`
	Instructions   string              `json:"instructions,omitempty"`
	Type           string              `json:"type"`
	Command        []string            `json:"command,omitempty"`
	URL            string              `json:"url,omitempty"`
	ReadOnly       bool                `json:"read_only,omitempty"`
	Concurrent     bool                `json:"concurrent,omitempty"`
	ResourceField  string              `json:"resource_field,omitempty"`
	TimeoutSeconds int                 `json:"timeout_seconds,omitempty"`
	Tools          []ExecutableToolDef `json:"tools,omitempty"`
}

type ExecutableToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func LoadCapabilityConfig(path string) (CapabilityConfig, error) {
	var config CapabilityConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if len(data) > 1<<20 {
		return config, fmt.Errorf("capability configuration exceeds 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&config); err != nil {
		return config, fmt.Errorf("capabilities: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return config, fmt.Errorf("capabilities: expected one JSON document")
	}
	if config.Version != 1 {
		return config, fmt.Errorf("unsupported capability config version %d", config.Version)
	}
	seen := map[string]bool{}
	for i := range config.Packs {
		pack := &config.Packs[i]
		if err := validateCapabilityID(pack.ID); err != nil {
			return config, err
		}
		if seen[pack.ID] {
			return config, fmt.Errorf("duplicate capability %q", pack.ID)
		}
		seen[pack.ID] = true
		if pack.Version == "" || pack.Description == "" {
			return config, fmt.Errorf("capability %s requires version and description", pack.ID)
		}
		if pack.TimeoutSeconds < 0 || pack.TimeoutSeconds > 600 {
			return config, fmt.Errorf("capability %s timeout_seconds must be 1–600 or omitted", pack.ID)
		}
		if pack.Concurrent && !pack.ReadOnly {
			return config, fmt.Errorf("capability %s concurrent requires read_only", pack.ID)
		}
		switch pack.Type {
		case "executable":
			if len(pack.Command) == 0 || pack.Command[0] == "" || pack.URL != "" || len(pack.Tools) == 0 {
				return config, fmt.Errorf("executable capability %s requires command and tools, without url", pack.ID)
			}
			toolNames := map[string]bool{}
			for _, tool := range pack.Tools {
				if err := validateCapabilityID(tool.Name); err != nil {
					return config, err
				}
				if toolNames[tool.Name] {
					return config, fmt.Errorf("duplicate tool %s/%s", pack.ID, tool.Name)
				}
				toolNames[tool.Name] = true
				if tool.Description == "" {
					return config, fmt.Errorf("tool %s/%s requires description", pack.ID, tool.Name)
				}
				if err := agent.ValidateToolSchema(tool.InputSchema); err != nil {
					return config, fmt.Errorf("tool %s/%s: %w", pack.ID, tool.Name, err)
				}
			}
		case "mcp":
			if len(pack.Command) > 0 && pack.Command[0] == "" {
				return config, fmt.Errorf("MCP capability %s command must not be empty", pack.ID)
			}
			if (len(pack.Command) == 0) == (pack.URL == "") || len(pack.Tools) != 0 {
				return config, fmt.Errorf("MCP capability %s requires exactly one of command or url, without tools", pack.ID)
			}
			if pack.URL != "" {
				if _, err := normalizeLoopbackURL(pack.URL); err != nil {
					return config, err
				}
			}
		default:
			return config, fmt.Errorf("capability %s: type must be executable or mcp", pack.ID)
		}
	}
	for i, root := range config.SkillRoots {
		if root == "" {
			return config, fmt.Errorf("skill root cannot be empty")
		}
		if !filepath.IsAbs(root) {
			config.SkillRoots[i] = filepath.Join(filepath.Dir(path), root)
		}
	}
	return config, nil
}

// BuildCapabilities constructs adapters without starting them. The caller owns
// cleanup for all connections, including ones first opened by a later session.
func BuildCapabilities(config CapabilityConfig, dir string) ([]*Capability, func()) {
	list := make([]*Capability, 0, len(config.Packs))
	type connection struct {
		source *Capability
		close  func()
	}
	connections := []*connection{}
	for _, pack := range config.Packs {
		encoded, _ := json.Marshal(pack)
		source := &Capability{ID: pack.ID, Version: pack.Version, Description: pack.Description, Instructions: pack.Instructions, Source: string(encoded), ReadOnly: pack.ReadOnly, Concurrent: pack.Concurrent, ResourceField: pack.ResourceField}
		conn := &connection{source: source}
		source.Close = func() {
			if conn.close != nil {
				conn.close()
				conn.close = nil
			}
		}
		connections = append(connections, conn)
		source.Load = func(ctx context.Context, _ func(string)) ([]agent.Tool, error) {
			if pack.Type == "executable" {
				tools := make([]agent.Tool, 0, len(pack.Tools))
				for _, def := range pack.Tools {
					tools = append(tools, &ExecutableTool{Pack: pack, Def: def, Dir: dir})
				}
				return tools, nil
			}
			var tools []agent.Tool
			var err error
			if pack.URL != "" {
				var client *httpMCPClient
				tools, client, err = StartMCPHTTP(ctx, pack.ID, pack.URL)
				if err == nil {
					conn.close = client.Close
				}
			} else {
				var client *MCPClient
				tools, client, err = StartMCP(ctx, pack.ID, dir, pack.Command[0], pack.Command[1:]...)
				if err == nil {
					conn.close = client.Close
				}
			}
			if err != nil {
				return nil, err
			}
			if pack.TimeoutSeconds > 0 {
				for i, tool := range tools {
					tools[i] = &timeoutTool{Tool: tool, timeout: time.Duration(pack.TimeoutSeconds) * time.Second}
				}
			}
			return tools, nil
		}
		list = append(list, source)
	}
	return list, func() {
		for _, conn := range connections {
			conn.source.mu.Lock()
			conn.source.Close()
			conn.source.mu.Unlock()
		}
	}
}

type timeoutTool struct {
	agent.Tool
	timeout time.Duration
}

func (t *timeoutTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	return t.Tool.Run(ctx, args)
}
func (t *timeoutTool) Attach(args json.RawMessage) ([]string, error) {
	if a, ok := t.Tool.(agent.Attacher); ok {
		return a.Attach(args)
	}
	return nil, nil
}
