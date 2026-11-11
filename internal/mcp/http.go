package mcp

import (
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
)

// HTTP speaks JSON-RPC 2.0 to one MCP server over Streamable
// HTTP: each call is a POST; the answer is a single JSON object or an
// SSE stream of `data:` lines. No subprocess, so no restart: every call
// is already independent, and an expired session is renewed inline.
// Calls may run concurrently; finish them before closing the session.
type HTTP struct {
	server  string
	url     string
	http    *http.Client
	mu      sync.Mutex // guards sequence and session
	seq     int
	session string
	headers map[string]string
	renew   sync.Mutex // serializes session renewal
}

func (c *HTTP) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seq++

	return c.seq
}

func (c *HTTP) sessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.session
}

func (c *HTTP) setSession(id string) {
	if id == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session == "" {
		c.session = id
	}
}

// Close ends the session server-side when the protocol supports it.
// Best-effort: a missing endpoint is normal, not an error.
func (c *HTTP) Close() {
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

	for name, value := range c.headers {
		req.Header.Set(name, value)
	}

	req.Header.Set("Mcp-Session-Id", session)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}

	// Close is best-effort; DELETE is unsupported by some MCP servers.
	resp.Body.Close()
}

// Call sends one request with the supplied deadline. Expired sessions renew
// once; tool mutations are never replayed after expiration. Safe for concurrent calls.
func (c *HTTP) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID()
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}

	raw, session, err := c.post(ctx, body, true, id)
	if errors.Is(err, errSessionExpired) && method != "initialize" {
		c.renew.Lock()
		if c.sessionID() == "" {
			var initialized any
			err = c.Call(ctx, "initialize", initialization(), &initialized)
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
		decodeErr := json.Unmarshal(raw, out)
		if decodeErr != nil {
			return fmt.Errorf("decode %s result: %w", method, decodeErr)
		}
	}

	return nil
}

// post sends one message. allowRenew permits a single retry without the
// session id when the server answers 404 (session expired): the retry
// carries no session, the server starts a fresh one, and its id is
// captured from the answer like any other.
func (c *HTTP) post(ctx context.Context, body []byte, allowRenew bool, expectedID int) (json.RawMessage, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}

	for name, value := range c.headers {
		req.Header.Set(name, value)
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

	// The response body must be released even when protocol decoding fails.
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound && allowRenew {
		c.mu.Lock()
		c.session = ""
		c.mu.Unlock()

		return nil, "", errSessionExpired
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
		decodeErr := json.Unmarshal(raw, &rpc)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("decode SSE response: %w", decodeErr)
		}

		if rpc.Error != nil {
			return nil, "", fmt.Errorf("%s", rpc.Error.Message)
		}

		return rpc.Result, session, nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, wireMax))
	if err != nil {
		return nil, "", err
	}

	var rpc rpcResponse
	decodeErr := json.Unmarshal(raw, &rpc)
	if decodeErr != nil {
		return nil, "", fmt.Errorf("decode response: %w", decodeErr)
	}

	if rpc.Error != nil {
		return nil, "", fmt.Errorf("%s", rpc.Error.Message)
	}

	return rpc.Result, session, nil
}

var errSessionExpired = errors.New("MCP session expired")

func (c *HTTP) notify(ctx context.Context, msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	notificationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, _, err = c.post(notificationCtx, body, false, 0)

	return err
}
