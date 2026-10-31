package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// mcpTimeout bounds a full discovery (initialize + tools/list) per server.
const mcpTimeout = 20 * time.Second

// mcpRestartBackoff caps the wait before a crashed stdio server is
// restarted. A variable (not a const) so tests do not sleep through it.
var mcpRestartBackoff = 10 * time.Second

// mcpMaxToolName caps an exposed tool name. Model tool namespaces are
// small, and a 200-byte server tool name would eat context on every step.
const mcpMaxToolName = 64

// mcpResultMax caps one MCP tool result in context; the wire may carry up
// to mcpWireMax before truncation.
const (
	mcpWireMax   = 10 * 1024 * 1024
	mcpResultMax = 48 * 1024
)

// MCPClient speaks JSON-RPC 2.0 to one MCP server over stdio.
type MCPClient struct {
	server      string
	cmd         *exec.Cmd
	stdin       *bufio.Writer
	input       io.Closer
	output      io.Closer
	writeGate   chan struct{}
	restartGate chan struct{}
	stdout      *bufio.Reader
	mu          sync.Mutex
	seq         int
	pending     map[int]chan json.RawMessage
	scanErr     error

	// close stops the server process, including any that replaced it.
	// Set by StartMCP; nil for the in-process transports tests inject.
	close func()

	// restarter rebuilds a dead transport: new command, pipes and
	// handshake, reusing this client's identity and sequence. Nil means
	// no restart (a dead server stays dead and calls fail). Set by
	// StartMCP; tests inject fakes.
	restarter func(ctx context.Context) (stdin io.Writer, stdout io.Reader, cmd *exec.Cmd, err error)
}

// mcpCaller is what an exposed tool needs: one JSON-RPC call. Both the
// stdio client and the HTTP client satisfy it, so MCPTool never learns
// which transport carries it.
type mcpCaller interface {
	call(ctx context.Context, method string, params any, out any) error
}

// MCPToolDef is one remote tool advertised by tools/list.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// StartMCP launches command with args and runs initialize + tools/list,
// returning one agent.Tool per remote tool named mcp__server__tool.
func StartMCP(ctx context.Context, server, dir, command string, args ...string) ([]agent.Tool, *MCPClient, error) {
	return startMCP(ctx, server, dir, nil, command, args...)
}

func startMCP(ctx context.Context, server, dir string, refs map[string]string, command string, args ...string) (list []agent.Tool, client *MCPClient, runErr error) {
	env, secrets, err := adapterEnv(refs)
	if err != nil {
		return nil, nil, err
	}
	defer func() { runErr = redactCredentialError(runErr, secrets) }()
	if err := validateCapabilityID(server); err != nil {
		return nil, nil, err
	}
	// The server must outlive the handshake, so its process gets a context
	// the handshake timeout cannot cancel. Close cancels it, which also
	// reaps a process a restart replaced.
	procCtx, killProc := context.WithCancel(context.WithoutCancel(ctx))
	var startupLog *syncBuffer
	launch := func() (io.Writer, io.Reader, *exec.Cmd, error) {
		cmd := exec.CommandContext(procCtx, command, args...)
		cmd.Dir = dir
		cmd.Env = env
		group := newProcessGroup()
		group.beforeStart(cmd)
		cmd.Cancel = func() error { return group.kill(cmd) }
		cmd.WaitDelay = stopGrace
		stdin, err := cmd.StdinPipe()
		if err != nil {
			group.release()
			return nil, nil, nil, fmt.Errorf("mcp %s: stdin: %w", server, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			group.release()
			return nil, nil, nil, fmt.Errorf("mcp %s: stdout: %w", server, err)
		}
		startupLog = &syncBuffer{}
		cmd.Stderr = startupLog
		if err := cmd.Start(); err != nil {
			stdin.Close()
			stdout.Close()
			group.release()
			return nil, nil, nil, fmt.Errorf("mcp %s: start %s: %w", server, command, err)
		}
		if err := group.afterStart(cmd); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			group.release()
			return nil, nil, nil, err
		}
		go func() { _ = cmd.Wait(); _ = group.kill(cmd); group.release() }()
		return stdin, stdout, cmd, nil
	}

	stdin, stdout, cmd, err := launch()
	if err != nil {
		killProc()
		return nil, nil, err
	}
	handshake, cancel := context.WithTimeout(ctx, mcpTimeout)
	defer cancel()
	tools, c, err := connectMCP(handshake, server, cmd, stdin, stdout)
	if err != nil {
		killProc()
		if startupLog != nil {
			if text := strings.TrimSpace(startupLog.String()); text != "" {
				err = fmt.Errorf("%w\nserver stderr: %s", err, agent.TruncateMiddle(text, 2048))
			}
		}
		return nil, nil, err
	}
	c.close = killProc
	for _, tool := range tools {
		t := tool.(*MCPTool)
		t.secrets = secrets
		t.def.Description = redactCredentials(t.def.Description, secrets)
		t.def.InputSchema = json.RawMessage(redactCredentials(string(t.def.InputSchema), secrets))
	}
	// The restarter reuses procCtx rather than its argument: a per-call
	// deadline would kill the replacement moments after it started.
	c.restarter = func(context.Context) (io.Writer, io.Reader, *exec.Cmd, error) {
		return launch()
	}
	return tools, c, nil
}

// connectMCP runs the handshake over an already-started transport. Tests
// use it with in-process pipes and a fake server instead of a subprocess.
func connectMCP(ctx context.Context, server string, cmd *exec.Cmd, stdin io.Writer, stdout io.Reader) ([]agent.Tool, *MCPClient, error) {
	c := &MCPClient{
		server:      server,
		cmd:         cmd,
		stdin:       bufio.NewWriter(stdin),
		stdout:      bufio.NewReader(stdout),
		pending:     map[int]chan json.RawMessage{},
		writeGate:   make(chan struct{}, 1),
		restartGate: make(chan struct{}, 1),
	}
	c.input, _ = stdin.(io.Closer)
	c.output, _ = stdout.(io.Closer)
	go c.readLoop(c.stdout)

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
		c.Close()
		return nil, nil, fmt.Errorf("mcp %s: initialize: %w", server, err)
	}
	_ = c.notify(ctx, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})

	defs, err := listMCPTools(ctx, c)
	if err != nil {
		c.Close()
		return nil, nil, fmt.Errorf("mcp %s: tools/list: %w", server, err)
	}
	names := assignToolNames(server, defs)
	tools := make([]agent.Tool, 0, len(defs))
	for i, td := range defs {
		tools = append(tools, &MCPTool{client: c, server: server, def: td, toolName: names[i]})
	}
	return tools, c, nil
}

// assignToolNames maps remote definitions to exposed names of the form
// mcp__server__tool, capped at mcpMaxToolName with _2/_3 dedup. A server
// advertising two "read" tools (or one 200-byte name) must not collide
// with or shadow its siblings — silent shadowing would route a call to
// the wrong remote tool with no visible trace.
func assignToolNames(server string, defs []MCPToolDef) []string {
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

// Close stops the server process, including any that replaced it.
func (c *MCPClient) Close() {
	if c.close != nil {
		c.close()
	}
	c.mu.Lock()
	input, output := c.input, c.output
	c.mu.Unlock()
	if input != nil {
		_ = input.Close()
	}
	if output != nil {
		_ = output.Close()
	}
}

func (c *MCPClient) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Method  string          `json:"method,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

var errMCPTransport = errors.New("MCP transport closed")

func (c *MCPClient) send(ctx context.Context, req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.write(ctx, append(data, '\n'))
}

// A cancelled write invalidates only the current transport. Closing its pipes
// unblocks the writer and reader; the callback finishes before write returns.
func (c *MCPClient) write(ctx context.Context, data []byte) error {
	select {
	case c.writeGate <- struct{}{}:
		defer func() { <-c.writeGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	writer, reader, input, output, dead := c.stdin, c.stdout, c.input, c.output, c.scanErr
	c.mu.Unlock()
	if dead != nil {
		return dead
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		if input != nil {
			_ = input.Close()
		}
		if output != nil {
			_ = output.Close()
		}
		close(finished)
	})
	_, err := writer.Write(data)
	if err == nil {
		err = writer.Flush()
	}
	if !stop() {
		<-finished
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		c.mu.Lock()
		if c.stdout == reader {
			c.scanErr = fmt.Errorf("%w: %v", errMCPTransport, err)
		}
		c.mu.Unlock()
	}
	return err
}

// readLoop dispatches responses for one transport. It tears the client
// down only while its reader is still the live one: after a restart the
// old reader unblocks too, and draining pending then would fail calls
// belonging to the new transport.
func (c *MCPClient) readLoop(r *bufio.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), mcpWireMax)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) > mcpWireMax {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.Method != "" {
			if resp.ID != 0 {
				ctx, cancel := context.WithTimeout(context.Background(), mcpTimeout)
				_ = c.notify(ctx, map[string]any{"jsonrpc": "2.0", "id": resp.ID, "error": map[string]any{"code": -32601, "message": "client method not supported"}})
				cancel()
			}
			continue
		}
		if resp.ID == 0 && resp.Result == nil {
			continue // notification or malformed; nothing waits on it
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if !ok {
			continue
		}
		if resp.Error != nil {
			ch <- json.RawMessage(fmt.Sprintf(`{"error":%q}`, resp.Error.Message))
		} else {
			ch <- resp.Result
		}
	}
	c.mu.Lock()
	if c.stdout != r {
		c.mu.Unlock()
		return
	}
	err := sc.Err()
	if err == nil {
		err = fmt.Errorf("server stdout closed")
	}
	c.scanErr = fmt.Errorf("%w: %v", errMCPTransport, err)
	err = c.scanErr
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	c.mu.Unlock()
}

func (c *MCPClient) call(ctx context.Context, method string, params any, out any) error {
	req := rpcRequest{JSONRPC: "2.0", ID: c.nextID(), Method: method, Params: params}
	if err := c.callOnce(ctx, req, method, out); err != nil {
		if ctx.Err() != nil || c.restarter == nil || !isTransportDead(err) {
			return err
		}
		if method == "tools/call" {
			// A disconnected response may follow a successful mutation.
			// Restart only for the next request; never replay a click/write.
			rerr := c.restartTransport(ctx)
			return fmt.Errorf("MCP connection lost; action outcome unknown, inspect state before retrying: %v (restart: %v)", err, rerr)
		}
		if rerr := c.restartTransport(ctx); rerr != nil {
			return fmt.Errorf("%s: %v (server restart: %v)", method, err, rerr)
		}
		return c.callOnce(ctx, req, method, out)
	}
	return nil
}

// isTransportDead reports transport failures that a restart can fix: the
// server is gone. Anything else (bad args, server-reported errors,
// timeouts) would fail identically against a fresh server.
func isTransportDead(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errMCPTransport) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{
		"broken pipe", "closed pipe", "connection reset", "connection refused",
		"use of closed", "file already closed", "eof", "mcp transport closed",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// restartTransport waits out the backoff, rebuilds a dead stdio server
// and re-runs the initialize handshake (tool definitions are cached, so
// tools/list is skipped). At most one restart per call: if the fresh
// server fails too, the error surfaces.
func (c *MCPClient) restartTransport(ctx context.Context) error {
	select {
	case c.restartGate <- struct{}{}:
		defer func() { <-c.restartGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	c.mu.Lock()
	dead := c.scanErr != nil
	c.mu.Unlock()
	if !dead {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(mcpRestartBackoff):
	}
	stdin, stdout, cmd, err := c.restarter(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	oldInput, oldOutput := c.input, c.output
	c.cmd = cmd
	c.stdin = bufio.NewWriter(stdin)
	reader := bufio.NewReader(stdout)
	c.stdout = reader
	c.input, _ = stdin.(io.Closer)
	c.output, _ = stdout.(io.Closer)
	c.scanErr = nil
	c.mu.Unlock()
	if oldInput != nil {
		_ = oldInput.Close()
	}
	if oldOutput != nil {
		_ = oldOutput.Close()
	}
	go c.readLoop(reader)
	var initResult struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	req := rpcRequest{JSONRPC: "2.0", ID: c.nextID(), Method: "initialize", Params: map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "gektor", "version": "1.0"},
	}}
	if err := c.callOnce(ctx, req, "initialize", &initResult); err != nil {
		c.Close()
		return err
	}
	_ = c.notify(ctx, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})
	return nil
}

func (c *MCPClient) callOnce(ctx context.Context, req rpcRequest, method string, out any) error {
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	if c.scanErr != nil {
		err := c.scanErr
		c.mu.Unlock()
		return err
	}
	c.pending[req.ID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, req); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case raw := <-ch:
		var probe struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Error != "" {
			return fmt.Errorf("%s", probe.Error)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (c *MCPClient) notify(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.write(ctx, append(data, '\n'))
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
	def              MCPToolDef
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
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	timeout := t.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	if err := t.client.call(ctx, "tools/call", map[string]any{
		"name": t.def.Name, "arguments": params,
	}, &result); err != nil {
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
			path, err := saveMCPImage(ctx, c.Data, c.MimeType)
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
		out, pictures = browserArtifacts(ctx, t.browserWorkspace, t.def.Name, out, pictures)
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
		out = LimitOutput(ctx, t.Name(), out, mcpResultMax)
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

func listMCPTools(ctx context.Context, c mcpCaller) ([]MCPToolDef, error) {
	var defs []MCPToolDef
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools      []MCPToolDef `json:"tools"`
			NextCursor string       `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		defs = append(defs, result.Tools...)
		if len(defs) > 4096 {
			return nil, fmt.Errorf("MCP tool catalog exceeds 4096 tools")
		}
		if result.NextCursor == "" {
			return defs, nil
		}
		if seen[result.NextCursor] {
			return nil, fmt.Errorf("MCP tool catalog repeated cursor")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("MCP tool catalog exceeds 100 pages")
}

func allowedMCPMime(m string) bool {
	m = strings.ToLower(m)
	return strings.HasPrefix(m, "text/") || m == "application/json"
}

// ParseMCPFlag parses -mcp "name=cmd args...;name2=cmd2" into server specs.
func ParseMCPFlag(spec string) (names, commands []string, args [][]string, err error) {
	var tokens []string
	var token strings.Builder
	var quote rune
	started := false
	flush := func() {
		if started {
			tokens = append(tokens, token.String())
			token.Reset()
			started = false
		}
	}
	entry := func() error {
		flush()
		if len(tokens) == 0 {
			return nil
		}
		name, command, ok := strings.Cut(tokens[0], "=")
		if !ok {
			command = name
		}
		if name == "" || command == "" {
			return fmt.Errorf("invalid MCP entry: expected name=command")
		}
		if err := validateCapabilityID(name); err != nil {
			return err
		}
		for _, previous := range names {
			if previous == name {
				return fmt.Errorf("duplicate MCP server %q", name)
			}
		}
		names = append(names, name)
		commands = append(commands, command)
		args = append(args, tokens[1:])
		tokens = nil
		return nil
	}
	for _, c := range spec {
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case ';':
			if err = entry(); err != nil {
				return
			}
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			token.WriteRune(c)
			started = true
		}
	}
	if quote != 0 {
		err = fmt.Errorf("unterminated quote in MCP specification")
		return
	}
	err = entry()
	return
}
