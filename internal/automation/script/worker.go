package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

func Serve(in io.Reader, out io.Writer) error {
	if err := limitWorker(); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(in, (128<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 128<<10 {
		return fmt.Errorf("script request too large")
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := Evaluate(ctx, req)
	if err != nil {
		return err
	}
	_, err = out.Write(value)
	return err
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("worker output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func Execute(ctx context.Context, r Request) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if len(data) > 128<<10 {
		return "", fmt.Errorf("script request too large")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "workflow", "internal-script-worker")
	configureWorker(cmd)
	cmd.Env = []string{"GOMEMLIMIT=128MiB"}
	cmd.Stdin = bytes.NewReader(data)
	output := &boundedBuffer{limit: 1 << 20}
	diagnostic := &boundedBuffer{limit: 4096}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("script worker: %w: %s", err, diagnostic.String())
	}
	if !json.Valid(output.Bytes()) {
		return "", fmt.Errorf("script worker returned invalid JSON")
	}
	return output.String(), nil
}
