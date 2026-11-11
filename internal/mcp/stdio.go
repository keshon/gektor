package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Stdio owns request dispatch and its server process. Calls may run concurrently.
// Close stops the process and joins all response readers; it is safe to repeat.
type Stdio struct {
	server                 string
	source                 transportSource
	backoff                time.Duration
	stdin                  *bufio.Writer
	stdout                 *bufio.Reader
	input, output          io.Closer
	writeGate, restartGate chan struct{}
	mu                     sync.Mutex // guards transport, pending calls, sequence, and closed state
	seq                    int
	pending                map[int]chan json.RawMessage
	scanErr                error
	closed                 bool
	readers                sync.WaitGroup
}

type transportSource interface {
	Launch(context.Context) (io.WriteCloser, io.ReadCloser, error)
	Close()
}

var errTransport = errors.New("MCP transport closed")

func connect(ctx context.Context, server string, input io.Writer, output io.Reader) (*Stdio, []ToolDefinition, error) {
	c := &Stdio{
		server:      server,
		stdin:       bufio.NewWriter(input),
		stdout:      bufio.NewReader(output),
		pending:     make(map[int]chan json.RawMessage),
		writeGate:   make(chan struct{}, 1),
		restartGate: make(chan struct{}, 1),
		backoff:     10 * time.Second,
	}
	c.input, _ = input.(io.Closer)
	c.output, _ = output.(io.Closer)
	c.startReader(c.stdout)

	defs, err := initialize(ctx, c)
	if err != nil {
		c.Close()
		return nil, nil, fmt.Errorf("mcp %s: %w", server, err)
	}

	return c, defs, nil
}

func (c *Stdio) startReader(reader *bufio.Reader) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.readers.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.readers.Done()
		c.readLoop(reader)
	}()
}

// Close cancels the owned process and waits for response dispatch to stop.
func (c *Stdio) Close() {
	c.mu.Lock()
	c.closed = true
	c.scanErr = errTransport
	input, output := c.input, c.output
	c.mu.Unlock()

	// Closing streams is best-effort; all pending calls receive transport failure.
	if input != nil {
		_ = input.Close()
	}
	if output != nil {
		_ = output.Close()
	}
	if c.source != nil {
		c.source.Close()
	}
	c.readers.Wait()
}

func (c *Stdio) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

// Call sends one request. Disconnected tool calls are never replayed.
func (c *Stdio) Call(ctx context.Context, method string, params any, out any) error {
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	}
	err := c.callOnce(ctx, request, method, out)
	if err == nil {
		return nil
	}

	canRestart := ctx.Err() == nil && c.source != nil && isTransportDead(err)
	if !canRestart {
		return err
	}
	restartErr := c.restartTransport(ctx)
	if method == "tools/call" {
		// A disconnected response may follow a successful mutation. A replacement
		// is for the next request; the original action must never be replayed.
		return fmt.Errorf("MCP connection lost; action outcome unknown, inspect state before retrying: %v (restart: %v)", err, restartErr)
	}
	if restartErr != nil {
		return fmt.Errorf("%s: %v (server restart: %v)", method, err, restartErr)
	}
	return c.callOnce(ctx, request, method, out)
}
