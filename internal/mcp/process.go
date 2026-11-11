package mcp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/process"
)

// StdioConfig describes an authorized server command. StartStdio copies Args and
// Env, retains Stderr, and owns the process tree until Stdio.Close completes.
// Stderr must support concurrent writes when an old transport is being replaced.
type StdioConfig struct {
	Server    string
	Directory string
	Command   string
	Args      []string
	Env       []string
	Stderr    io.Writer
}

// StartStdio launches a server and discovers its tools. The discovery context
// bounds startup only; the returned session owns its lifetime. Call Close even
// after the launch context ends. Failed startup stops and joins the process.
func StartStdio(ctx context.Context, cfg StdioConfig) (*Stdio, []ToolDefinition, error) {
	cfg.Args = slices.Clone(cfg.Args)
	cfg.Env = slices.Clone(cfg.Env)
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}

	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	source := &processSource{config: cfg, ctx: lifetime, cancel: cancel}
	input, output, err := source.Launch(ctx)
	if err != nil {
		source.Close()
		return nil, nil, err
	}

	discovery, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	client, defs, err := connect(discovery, cfg.Server, input, output)
	if err != nil {
		source.Close()
		return nil, nil, err
	}

	client.source = source
	return client, defs, nil
}

type processSource struct {
	config  StdioConfig
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex // guards closed and admission to workers
	closed  bool
	workers sync.WaitGroup
}

func (s *processSource) Launch(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
	err := ctx.Err()
	if err != nil {
		return nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errTransport
	}

	cmd := exec.CommandContext(s.ctx, s.config.Command, s.config.Args...)
	cmd.Dir = s.config.Directory
	cmd.Env = slices.Clone(s.config.Env)
	cmd.Stderr = s.config.Stderr
	cmd.WaitDelay = 3 * time.Second
	hideWindow(cmd)

	group := process.NewGroup()
	group.BeforeStart(cmd)
	// exec.Cmd requires this callback to stop the entire tree on cancellation.
	cmd.Cancel = func() error { return group.Kill(cmd) }
	input, err := cmd.StdinPipe()
	if err != nil {
		group.Release()
		return nil, nil, fmt.Errorf("mcp %s: stdin: %w", s.config.Server, err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		// Preserve the pipe setup error if closing the unused input also fails.
		_ = input.Close()
		group.Release()
		return nil, nil, fmt.Errorf("mcp %s: stdout: %w", s.config.Server, err)
	}

	err = cmd.Start()
	if err != nil {
		// Failed startup owns both pipe ends; they must not survive this return.
		_ = input.Close()
		_ = output.Close()
		group.Release()
		return nil, nil, fmt.Errorf("mcp %s: start %s: %w", s.config.Server, s.config.Command, err)
	}
	err = group.AfterStart(cmd)
	if err != nil {
		// Preserve job-adoption failure while reaping the partially started server.
		_ = group.Kill(cmd)
		_ = cmd.Wait()
		group.Release()
		return nil, nil, err
	}

	s.workers.Add(1)
	go s.wait(cmd, group)
	return input, output, nil
}

func (s *processSource) wait(cmd *exec.Cmd, group *process.Group) {
	defer s.workers.Done()
	// RPC readers report transport failures. Wait only reaps the process and
	// stops descendants, including launchers whose direct child has already exited.
	_ = cmd.Wait()
	_ = group.Kill(cmd)
	group.Release()
}

func (s *processSource) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.workers.Wait()
}
