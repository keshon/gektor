package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
)

func (c *Stdio) send(ctx context.Context, req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.write(ctx, append(data, '\n'))
}

// A cancelled write invalidates only the current transport. Closing its pipes
// unblocks the writer and reader; the callback finishes before write returns.
func (c *Stdio) write(ctx context.Context, data []byte) error {
	select {
	case c.writeGate <- struct{}{}:
		defer func() { <-c.writeGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		return contextErr
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
			c.scanErr = fmt.Errorf("%w: %v", errTransport, err)
		}
		c.mu.Unlock()
	}
	return err
}

// readLoop dispatches responses for one transport. It tears the client
// down only while its reader is still the live one: after a restart the
// old reader unblocks too, and draining pending then would fail calls
// belonging to the new transport.
func (c *Stdio) readLoop(r *bufio.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), wireMax)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) > wireMax {
			continue
		}
		var resp rpcResponse
		decodeErr := json.Unmarshal(line, &resp)
		if decodeErr != nil {
			continue
		}
		if resp.Method != "" {
			if resp.ID != 0 {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	c.scanErr = fmt.Errorf("%w: %v", errTransport, err)
	err = c.scanErr
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	c.mu.Unlock()
}

func (c *Stdio) callOnce(ctx context.Context, req rpcRequest, method string, out any) error {
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
	sendErr := c.send(ctx, req)
	if sendErr != nil {
		return sendErr
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
			decodeErr := json.Unmarshal(raw, out)
			if decodeErr != nil {
				return fmt.Errorf("decode %s result: %w", method, decodeErr)
			}
		}
		return nil
	}
}

func (c *Stdio) notify(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.write(ctx, append(data, '\n'))
}
