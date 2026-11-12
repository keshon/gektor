package command

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/process"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

// Run is the common bounded, credential-scrubbed shell path for tools
// and verification. Cancellation kills the process tree and bounds pipe drain.
func Run(ctx context.Context, command, dir string, timeout time.Duration) (string, error) {
	return Streaming(ctx, command, dir, timeout, nil)
}

func Streaming(ctx context.Context, command, dir string, timeout time.Duration, onOutput func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return Execute(ctx, Shell(ctx, command), dir, onOutput)
}

// Program preserves argument boundaries without passing through a shell.
func Program(ctx context.Context, program string, args []string, dir string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return Execute(ctx, exec.CommandContext(ctx, program, args...), dir, nil)
}

func Execute(ctx context.Context, cmd *exec.Cmd, dir string, onOutput func(string)) (result string, runErr error) {
	cmd.Dir = dir
	cmd.Env = process.ScrubEnv(os.Environ())
	cmd.WaitDelay = 3 * time.Second
	group := process.NewGroup()
	defer group.Release()
	group.BeforeStart(cmd)
	cmd.Cancel = func() error { return group.Kill(cmd) }
	out := &commandOutput{onOutput: onOutput}
	if store := tooloutput.FromContext(ctx); store != nil {
		file, err := store.Create()
		if err != nil {
			defer func() { result += "\nOutput storage warning: " + err.Error() }()
		} else {
			out.file = file
			defer func() {
				note := file.Finish()
				if out.Size() > tooloutput.PreviewBytes || file.Failed() {
					result = tooloutput.TruncateMiddle(result, max(1, tooloutput.PreviewBytes-len(note)-1)) + "\n" + note
				} else {
					_ = file.Discard()
				}
			}()
		}
	}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	if err := group.AfterStart(cmd); err != nil {
		_ = group.Kill(cmd)
		_ = cmd.Wait()
		return out.String(), err
	}
	err := cmd.Wait()
	out.flush()
	_ = group.Kill(cmd)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.String(), err
}

// os/exec serializes writes because stdout and stderr share this writer.
// Preview bytes are bounded independently of the authoritative head/tail log.
type commandOutput struct {
	tooloutput.Buffer
	file     *tooloutput.Writer
	onOutput func(string)
	pending  []byte
	emitted  int
}

func (o *commandOutput) Write(p []byte) (int, error) {
	if o.file != nil {
		_, _ = o.file.Write(p)
	}
	n, err := o.Buffer.Write(p)
	if o.onOutput == nil || o.emitted >= tooloutput.PreviewBytes {
		return n, err
	}
	remaining := tooloutput.PreviewBytes - o.emitted - len(o.pending)
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
		o.emitted = tooloutput.PreviewBytes
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
