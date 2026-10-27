package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// ExecutableTool exchanges one JSON request/response with a native process.
// Arguments are never interpolated into shell source or command-line flags.
type ExecutableTool struct {
	Pack CapabilityPack
	Def  ExecutableToolDef
	Dir  string
}

func (t *ExecutableTool) Name() string {
	return boundedToolName("cap__" + t.Pack.ID + "__" + t.Def.Name)
}
func (t *ExecutableTool) Description() string     { return t.Def.Description }
func (t *ExecutableTool) Schema() json.RawMessage { return t.Def.InputSchema }
func (t *ExecutableTool) Mode() agent.ToolMode    { return agent.Exclusive }
func (t *ExecutableTool) Run(ctx context.Context, args json.RawMessage) (result string, runErr error) {
	env, secrets, err := adapterEnv(t.Pack.EnvRefs)
	if err != nil {
		return "", err
	}
	defer func() {
		result = redactCredentials(result, secrets)
		runErr = redactCredentialError(runErr, secrets)
	}()
	timeout := t.Pack.TimeoutSeconds
	if timeout == 0 {
		timeout = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	request, err := json.Marshal(struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}{t.Def.Name, args})
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, t.Pack.Command[0], t.Pack.Command[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdin = t.Dir, env, bytes.NewReader(append(request, '\n'))
	stdout := &jsonOutput{limit: 1 << 20, cancel: cancel}
	stderr := &jsonOutput{limit: 8192}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = stopGrace
	group := newProcessGroup()
	defer group.release()
	group.beforeStart(cmd)
	cmd.Cancel = func() error { return group.kill(cmd) }
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start executable %s: %w", t.Pack.ID, err)
	}
	if err := group.afterStart(cmd); err != nil {
		_ = group.kill(cmd)
		_ = cmd.Wait()
		return "", err
	}
	err = cmd.Wait()
	_ = group.kill(cmd)
	if stdout.overflow {
		return "", fmt.Errorf("executable %s response exceeds 1 MiB", t.Pack.ID)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("executable %s: %w", t.Pack.ID, ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("executable %s: %w; stderr: %s", t.Pack.ID, err, strings.TrimSpace(stderr.String()))
	}
	var response struct {
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error,omitempty"`
	}
	d := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	d.DisallowUnknownFields()
	if err := d.Decode(&response); err != nil {
		return "", fmt.Errorf("executable %s returned invalid response JSON: %w", t.Pack.ID, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("executable %s must return exactly one JSON response", t.Pack.ID)
	}
	output := string(response.Output)
	var text string
	if bytes.HasPrefix(bytes.TrimSpace(response.Output), []byte(`"`)) && json.Unmarshal(response.Output, &text) == nil {
		output = text
	}
	output = LimitOutput(ctx, t.Name(), output, 48*1024)
	if response.Error != "" {
		return output, fmt.Errorf("executable %s: %s", t.Pack.ID, response.Error)
	}
	if len(response.Output) == 0 {
		return "", fmt.Errorf("executable %s response requires output or error", t.Pack.ID)
	}
	return output, nil
}

type jsonOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *jsonOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, max(0, b.limit-b.Len()))
	_, _ = b.Buffer.Write(p[:keep])
	if keep != n {
		b.overflow = true
		if b.cancel != nil {
			b.cancel()
		}
	}
	return n, nil
}
