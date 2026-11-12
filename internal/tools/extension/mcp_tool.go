package extension

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
	toolimages "github.com/keshon/gektor/internal/tools/images"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

const mcpMaxToolName = 64
const mcpResultMax = 48 * 1024

// mcpCaller is shared by the stdio and HTTP transports.
type mcpCaller interface {
	Call(context.Context, string, any, any) error
}

// assignToolNames maps remote definitions to exposed names of the form
// mcp__server__tool, capped at mcpMaxToolName with _2/_3 dedup. A server
// advertising two "read" tools (or one 200-byte name) must not collide
// with or shadow its siblings — silent shadowing would route a call to
// the wrong remote tool with no visible trace.
func assignToolNames(server string, defs []mcp.ToolDefinition) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(defs))
	for _, td := range defs {
		base := boundedToolName("mcp__" + server + "__" + td.Name)
		name := base
		for n := 2; seen[name]; n++ {
			suffix := fmt.Sprintf("_%d", n)
			name = base[:min(len(base), mcpMaxToolName-len(suffix))] + suffix
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

var capabilityIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func validateCapabilityID(id string) error {
	if !capabilityIdentifier.MatchString(id) {
		return fmt.Errorf("invalid capability/server ID %q: use 1–32 ASCII letters, digits, underscores or hyphens", id)
	}
	return nil
}

func boundedToolName(raw string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, raw)
	if clean != raw || len(clean) > mcpMaxToolName {
		sum := sha256.Sum256([]byte(raw))
		suffix := fmt.Sprintf("_%x", sum[:6])
		clean = clean[:min(len(clean), mcpMaxToolName-len(suffix))] + suffix
	}
	return clean
}

// MCPTool is one remote MCP tool exposed as an agent tool.
type MCPTool struct {
	// Set only by the built-in browser launcher. Other MCP servers do not
	// gain automatic filesystem access from links in their responses.
	browserWorkspace string
	secrets          []string
	timeout          time.Duration
	client           mcpCaller
	server           string
	def              mcp.ToolDefinition
	toolName         string
	imageMu          sync.Mutex
	images           map[string][][]string
}

func (t *MCPTool) Name() string { return t.toolName }

func (t *MCPTool) Mode() agent.ToolMode {
	return agent.Exclusive
}

func (t *MCPTool) Description() string {
	desc := strings.TrimSpace(t.def.Description)
	if desc == "" {
		desc = "MCP tool " + t.def.Name
	}
	return fmt.Sprintf("[MCP %s] %s", t.server, desc)
}

func (t *MCPTool) Schema() json.RawMessage {
	if len(t.def.InputSchema) > 0 {
		return t.def.InputSchema
	}
	return json.RawMessage(`{"type":"object"}`)
}

func (t *MCPTool) Run(ctx context.Context, args json.RawMessage) (output string, runErr error) {
	defer func() {
		output = redactCredentials(output, t.secrets)
		runErr = redactCredentialError(runErr, t.secrets)
	}()
	var params map[string]any
	err := json.Unmarshal(args, &params)
	if err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	timeout := t.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var result struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
			Resource *struct {
				Text     string `json:"text"`
				URI      string `json:"uri"`
				MimeType string `json:"mimeType"`
			} `json:"resource"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	callParams := map[string]any{
		"name": t.def.Name, "arguments": params,
	}
	err = t.client.Call(callCtx, "tools/call", callParams, &result)
	if err != nil {
		return "", fmt.Errorf("mcp %s/%s: %w", t.server, t.def.Name, err)
	}
	var b strings.Builder
	var pictures []string
	if len(result.StructuredContent) > 0 && string(result.StructuredContent) != "null" {
		b.Write(result.StructuredContent)
		b.WriteByte('\n')
	}
	for _, c := range result.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
			b.WriteString("\n")
		} else if c.Type == "image" {
			path, err := toolimages.SaveMCP(callCtx, c.Data, c.MimeType)
			if err != nil {
				fmt.Fprintf(&b, "[image unavailable: %s]\n", err)
			} else {
				pictures = append(pictures, path)
				fmt.Fprintf(&b, "[image attached: %s]\n", path)
			}
		} else if c.Type == "resource" && c.Resource != nil {
			if allowedMCPMime(c.Resource.MimeType) {
				fmt.Fprintf(&b, "Resource %s:\n%s\n", c.Resource.URI, c.Resource.Text)
			} else {
				fmt.Fprintf(&b, "[resource omitted: %s]\n", c.Resource.MimeType)
			}
		} else if c.Type == "resource" {
			if !allowedMCPMime(c.MimeType) {
				fmt.Fprintf(&b, "[omitted %s content: %s]\n", c.Type, c.MimeType)
				continue
			}
			b.WriteString("[binary content omitted: " + c.MimeType + "]\n")
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if result.IsError {
		return out, fmt.Errorf("mcp %s/%s reported an error", t.server, t.def.Name)
	}
	if t.browserWorkspace != "" {
		out, pictures = browserArtifacts(callCtx, t.browserWorkspace, t.def.Name, out, pictures)
	}
	if len(pictures) > 0 {
		t.imageMu.Lock()
		if t.images == nil {
			t.images = map[string][][]string{}
		}
		t.images[string(args)] = append(t.images[string(args)], pictures)
		t.imageMu.Unlock()
	}
	if len(out) > mcpResultMax {
		// Structured output reduces; prose truncates. A blind middle-cut
		// through a JSON array leaves half an element to parse, so try
		// the reducer first and fall back to truncation for non-JSON.
		out = tooloutput.Limit(callCtx, out, mcpResultMax)
	}
	if out == "" {
		return "(empty result)", nil
	}
	return out, nil
}

func (t *MCPTool) Attach(args json.RawMessage) ([]string, error) {
	t.imageMu.Lock()
	defer t.imageMu.Unlock()
	queue := t.images[string(args)]
	if len(queue) == 0 {
		return nil, nil
	}
	paths := queue[0]
	if len(queue) == 1 {
		delete(t.images, string(args))
	} else {
		t.images[string(args)] = queue[1:]
	}
	return paths, nil
}

func allowedMCPMime(m string) bool {
	m = strings.ToLower(m)
	return strings.HasPrefix(m, "text/") || m == "application/json"
}

// RemoteName identifies the original operation before local name qualification.
func (t *MCPTool) RemoteName() string { return t.def.Name }
