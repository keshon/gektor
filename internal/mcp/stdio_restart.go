package mcp

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"time"
)

// isTransportDead reports transport failures that a restart can fix: the
// server is gone. Anything else (bad args, server-reported errors,
// timeouts) would fail identically against a fresh server.
func isTransportDead(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errTransport) {
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
func (c *Stdio) restartTransport(ctx context.Context) error {
	select {
	case c.restartGate <- struct{}{}:
		defer func() { <-c.restartGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	c.mu.Lock()
	dead := c.scanErr != nil
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errTransport
	}
	if !dead {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.backoff):
	}
	stdin, stdout, err := c.source.Launch(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		// Close won the race while a replacement was starting.
		_ = stdin.Close()
		_ = stdout.Close()
		return errTransport
	}
	oldInput, oldOutput := c.input, c.output
	c.stdin = bufio.NewWriter(stdin)
	reader := bufio.NewReader(stdout)
	c.stdout = reader
	c.input = stdin
	c.output = stdout
	c.scanErr = nil
	c.mu.Unlock()
	// Old streams can already be closed by the failing reader or writer.
	if oldInput != nil {
		_ = oldInput.Close()
	}
	if oldOutput != nil {
		_ = oldOutput.Close()
	}
	c.startReader(reader)
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
	err = c.callOnce(ctx, req, "initialize", &initResult)
	if err != nil {
		c.Close()
		return err
	}
	_ = c.notify(ctx, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})
	return nil
}
