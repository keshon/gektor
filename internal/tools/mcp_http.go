package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// httpMCPClient speaks JSON-RPC 2.0 to one MCP server over Streamable
// HTTP: each call is a POST; the answer is a single JSON object or an
// SSE stream of `data:` lines. No subprocess, so no restart: every call
// is already independent, and an expired session is renewed inline.
type httpMCPClient struct {
	server  string
	url     string
	http    *http.Client
	mu      sync.Mutex
	seq     int
	session string
	renew   sync.Mutex
}

func (c *httpMCPClient) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *httpMCPClient) sessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *httpMCPClient) setSession(id string) {
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == "" {
		c.session = id
	}
}

// StartMCPHTTP runs initialize + tools/list against a Streamable HTTP
// endpoint and returns one agent.Tool per remote tool, named exactly like
// the stdio ones (mcp__server__tool, deduped and capped). rawURL must be
// absolute http(s); loopback is permitted (local dev servers), everything
// else follows the public-URL rules.
func StartMCPHTTP(ctx context.Context, server, rawURL string) ([]agent.Tool, *httpMCPClient, error) {
	if err := validateCapabilityID(server); err != nil {
		return nil, nil, err
	}
	clean, err := normalizeLoopbackURL(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp %s: %w", server, err)
	}
	ctx, cancel := context.WithTimeout(ctx, mcpTimeout)
	defer cancel()

	c := &httpMCPClient{
		server: server,
		url:    clean,
		// Discovery, calls, notifications and Close each own their context deadline.
		http: networkClient(true, nil),
	}
	c.http.Timeout = 0 // Each operation supplies its own deadline.
	success := false
	defer func() {
		if !success {
			c.Close()
		}
	}()
	var initResult struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "gektor", "version": "1.0"},
	}, &initResult); err != nil {
		return nil, nil, fmt.Errorf("mcp %s: initialize: %w", server, err)
	}
	_ = c.notify(ctx, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})

	defs, err := listMCPTools(ctx, c)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp %s: tools/list: %w", server, err)
	}
	names := assignToolNames(server, defs)
	tools := make([]agent.Tool, 0, len(defs))
	for i, td := range defs {
		tools = append(tools, &MCPTool{client: c, server: server, def: td, toolName: names[i]})
	}
	success = true
	return tools, c, nil
}

// Close ends the session server-side when the protocol supports it.
// Best-effort: a missing endpoint is normal, not an error.
func (c *httpMCPClient) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session := c.sessionID()
	if session == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (c *httpMCPClient) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID()
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	raw, session, err := c.post(ctx, body, true, id)
	if errors.Is(err, errMCPSessionExpired) && method != "initialize" {
		c.renew.Lock()
		if c.sessionID() == "" {
			var initialized any
			err = c.call(ctx, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "gektor", "version": "1.0"}}, &initialized)
			if err == nil {
				err = c.notify(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
			}
		} else {
			err = nil
		}
		c.renew.Unlock()
		if err != nil {
			return fmt.Errorf("renew MCP session: %w", err)
		}
		if method == "tools/call" {
			return fmt.Errorf("MCP session expired and was reinitialized; original tool call was not replayed. Inspect its effects before explicitly retrying")
		}
		raw, session, err = c.post(ctx, body, false, id)
	}
	if err != nil {
		return err
	}
	c.setSession(session)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

// post sends one message. allowRenew permits a single retry without the
// session id when the server answers 404 (session expired): the retry
// carries no session, the server starts a fresh one, and its id is
// captured from the answer like any other.
func (c *httpMCPClient) post(ctx context.Context, body []byte, allowRenew bool, expectedID int) (json.RawMessage, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session := c.sessionID(); session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && allowRenew {
		c.mu.Lock()
		c.session = ""
		c.mu.Unlock()
		return nil, "", errMCPSessionExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("mcp %s: HTTP %s: %s", c.server, resp.Status, strings.TrimSpace(string(raw)))
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil, session, nil
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		raw, err := scanSSE(resp.Body, expectedID)
		if err != nil {
			return nil, "", err
		}
		var rpc rpcResponse
		if err := json.Unmarshal(raw, &rpc); err != nil {
			return nil, "", fmt.Errorf("decode SSE response: %w", err)
		}
		if rpc.Error != nil {
			return nil, "", fmt.Errorf("%s", rpc.Error.Message)
		}
		return rpc.Result, session, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, mcpWireMax))
	if err != nil {
		return nil, "", err
	}
	var rpc rpcResponse
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}
	if rpc.Error != nil {
		return nil, "", fmt.Errorf("%s", rpc.Error.Message)
	}
	return rpc.Result, session, nil
}

var errMCPSessionExpired = errors.New("MCP session expired")

func (c *httpMCPClient) notify(ctx context.Context, msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _, err = c.post(ctx, body, false, 0)
	return err
}

// scanSSE returns the matching response without waiting for the server to close
// the stream. Notifications and responses for other requests do not complete it.
func scanSSE(r io.Reader, expectedID int) (json.RawMessage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), mcpWireMax)
	var data []string
	dataBytes := 0
	decode := func() (json.RawMessage, error) {
		if len(data) == 0 {
			return nil, nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		dataBytes = 0
		if payload == "[DONE]" {
			return nil, nil
		}
		var response rpcResponse
		if err := json.Unmarshal([]byte(payload), &response); err != nil {
			return nil, fmt.Errorf("decode SSE response: %w", err)
		}
		if response.Method == "" && response.ID == expectedID &&
			(response.Result != nil || response.Error != nil) {
			return json.RawMessage(payload), nil
		}
		return nil, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if raw, err := decode(); raw != nil || err != nil {
				return raw, err
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataBytes += len(value) + 1
			if dataBytes > mcpWireMax {
				return nil, fmt.Errorf("SSE event exceeds %d bytes", mcpWireMax)
			}
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if raw, err := decode(); raw != nil || err != nil {
		return raw, err
	}
	return nil, fmt.Errorf("SSE stream ended without response for request %d", expectedID)
}

// IsMCPURL reports whether s is an http(s) URL (an MCP server address)
// rather than a command line.
func IsMCPURL(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return false
	}
	lower := strings.ToLower(fields[0])
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
