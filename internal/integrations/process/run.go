package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const OutputLimit = 1 << 20 // per stream

type Request struct {
	Program   string
	Args      []string
	Directory string
	Stdin     io.Reader
	Timeout   time.Duration
}
type Result struct {
	ExitCode       int
	Stdout, Stderr []byte
	Truncated      bool
}

// Run starts a finite native process. The caller owns authorization and directory
// selection. It is not an OS sandbox; explicit interpreters remain executable.
func Run(ctx context.Context, in Request) (Result, error) {
	result := Result{ExitCode: -1}
	if strings.TrimSpace(in.Program) == "" || in.Timeout <= 0 || in.Timeout > 10*time.Minute {
		return result, fmt.Errorf("program and timeout in (0, 10m] are required")
	}
	ctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, in.Program, in.Args...)
	if ext := strings.ToLower(filepath.Ext(cmd.Path)); runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat" || ext == ".ps1") {
		return result, fmt.Errorf("program must be a native executable; specify an interpreter explicitly for scripts")
	}
	cmd.Dir, cmd.Env, cmd.Stdin = in.Directory, ScrubEnv(os.Environ()), in.Stdin
	cmd.WaitDelay = 3 * time.Second
	group := NewGroup()
	defer group.Release()
	group.BeforeStart(cmd)
	hideWindow(cmd)
	cmd.Cancel = func() error { return group.Kill(cmd) }
	stdout, stderr := &limitedOutput{cancel: cancel}, &limitedOutput{cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return result, err
	}
	if err := group.AfterStart(cmd); err != nil {
		_ = group.Kill(cmd)
		_ = cmd.Wait()
		return result, err
	}
	err := cmd.Wait()
	_ = group.Kill(cmd)
	result.ExitCode = cmd.ProcessState.ExitCode()
	result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
	result.Truncated = stdout.overflow || stderr.overflow
	if result.Truncated {
		return result, fmt.Errorf("process output exceeded 1 MiB per stream; process stopped")
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return result, fmt.Errorf("process output pipes remained open after exit: %w", err)
	}
	return result, err
}

type limitedOutput struct {
	buffer   bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (b *limitedOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, OutputLimit-b.buffer.Len())
	_, _ = b.buffer.Write(p[:keep])
	if keep < n {
		b.overflow = true
		b.cancel()
	}
	return n, nil
}
