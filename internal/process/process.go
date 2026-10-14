// Package process owns native subprocess lifetime and readiness.
package process

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"
)

type Child struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func Start(ctx context.Context, binary, directory string, args []string, log io.Writer) (*Child, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = directory
	cmd.Stdout = log
	cmd.Stderr = log
	Hide(cmd)
	return startCommand(cmd)
}

// Run executes an already configured command with the same lifetime ownership as servers.
func Run(cmd *exec.Cmd) error {
	Hide(cmd)
	c, err := startCommand(cmd)
	if err != nil {
		return err
	}
	return c.Wait()
}
func startCommand(cmd *exec.Cmd) (*Child, error) {
	job, err := newOwner()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		job.close()
		return nil, err
	}
	if err := job.attach(cmd.Process); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		job.close()
		return nil, err
	}
	c := &Child{command: cmd, done: make(chan struct{})}
	go func() { c.err = cmd.Wait(); job.close(); close(c.done) }()
	return c, nil
}
func (c *Child) Wait() error { <-c.done; return c.err }
func (c *Child) Stop() {
	select {
	case <-c.done:
		return
	default:
		_ = c.command.Process.Kill()
		<-c.done
	}
}
func (c *Child) Ready(ctx context.Context, address string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", address+"/health", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-c.done:
			return fmt.Errorf("native process exited: %v", c.err)
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}
