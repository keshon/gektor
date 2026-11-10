package script

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/keshon/gektor/internal/worker"
)

func Serve(in io.Reader, out io.Writer) error {
	if err := worker.Limit(); err != nil {
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

func Execute(ctx context.Context, r Request) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if len(data) > 128<<10 {
		return "", fmt.Errorf("script request too large")
	}
	return worker.Run(ctx, "internal-script-worker", data, 5*time.Second, 1<<20)
}
