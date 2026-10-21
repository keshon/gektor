package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// RunCommand is the common bounded, credential-scrubbed shell path for tools
// and verification. Cancellation kills the process tree and bounds pipe drain.
func RunCommand(ctx context.Context, command, dir string, timeout time.Duration) (string, error) {
	return runCommand(ctx, command, dir, timeout, nil)
}

func runCommand(ctx context.Context, command, dir string, timeout time.Duration, onOutput func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runProcess(ctx, shellCommand(ctx, command), dir, onOutput)
}

// runProgram preserves argument boundaries without passing through a shell.
func runProgram(ctx context.Context, program string, args []string, dir string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runProcess(ctx, exec.CommandContext(ctx, program, args...), dir, nil)
}

func runProcess(ctx context.Context, cmd *exec.Cmd, dir string, onOutput func(string)) (string, error) {
	cmd.Dir = dir
	cmd.Env = scrubEnv(os.Environ())
	cmd.WaitDelay = stopGrace
	group := newProcessGroup()
	defer group.release()
	group.beforeStart(cmd)
	cmd.Cancel = func() error { return group.kill(cmd) }
	out := &commandOutput{onOutput: onOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	if err := group.afterStart(cmd); err != nil {
		_ = group.kill(cmd)
		_ = cmd.Wait()
		return out.String(), err
	}
	err := cmd.Wait()
	out.flush()
	_ = group.kill(cmd)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.String(), err
}

// Keep both the beginning and recent tail without retaining unbounded logs.
func boundedOutput(head, tail string, total int) string {
	if total <= shellMaxBytes {
		return head + tail
	}
	return head + "\n...(output truncated; recent tail follows)\n" + tail
}

func appendBounded(head, tail *strings.Builder, total *int, p []byte) {
	*total += len(p)
	half := shellMaxBytes / 2
	if head.Len() < half {
		n := half - head.Len()
		if n > len(p) {
			n = len(p)
		}
		head.Write(p[:n])
		p = p[n:]
	}
	if len(p) == 0 {
		return
	}
	if len(p) >= half {
		tail.Reset()
		tail.Write(p[len(p)-half:])
		return
	}
	previous := tail.String()
	if len(previous)+len(p) > half {
		previous = previous[len(previous)+len(p)-half:]
	}
	tail.Reset()
	tail.WriteString(previous)
	tail.Write(p)
}

// os/exec serializes writes because stdout and stderr share this writer.
// Preview bytes are bounded independently of the authoritative head/tail log.
type commandOutput struct {
	syncBuffer
	onOutput func(string)
	pending  []byte
	emitted  int
}

func (o *commandOutput) Write(p []byte) (int, error) {
	n, err := o.syncBuffer.Write(p)
	if o.onOutput == nil || o.emitted >= shellMaxBytes {
		return n, err
	}
	remaining := shellMaxBytes - o.emitted - len(o.pending)
	o.pending = append(o.pending, p[:min(len(p), remaining)]...)
	end := 0
	for end < len(o.pending) && utf8.FullRune(o.pending[end:]) {
		_, size := utf8.DecodeRune(o.pending[end:])
		end += size
	}
	if end > 0 {
		o.onOutput(strings.ToValidUTF8(string(o.pending[:end]), "�"))
		o.emitted += end
		o.pending = append(o.pending[:0], o.pending[end:]...)
	}
	if len(p) >= remaining {
		// The preview limit may split a valid rune; omit that partial rune.
		o.pending = nil
		o.onOutput("\n… (live preview limit reached; final output follows when the command finishes)\n")
		o.emitted = shellMaxBytes
	}
	return n, err
}

func (o *commandOutput) flush() {
	if o.onOutput != nil && len(o.pending) > 0 {
		o.onOutput(strings.ToValidUTF8(string(o.pending), "�"))
		o.emitted += len(o.pending)
		o.pending = nil
	}
}
