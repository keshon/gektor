package command

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func Git(ctx context.Context, dir string, args ...string) (string, error) {
	prefix := []string{"--no-pager", "--literal-pathspecs", "-c", "color.ui=false", "-c", "core.quotePath=false", "-c", "core.fsmonitor=false"}
	out, err := Program(ctx, "git", append(prefix, args...), dir, 60*time.Second)
	out = strings.TrimRight(out, "\r\n")
	if err != nil {
		return out, fmt.Errorf("git failed: %w", err)
	}
	return out, nil
}
