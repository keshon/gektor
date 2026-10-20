package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/gektor"
)

// Spill writes full tool output to disk and returns its path. Best-effort:
// any failure returns "" and the caller keeps the truncated inline result.
// Files land in the user's GEKTOR directory so a large fetch never lands in
// the workspace the agent is working on.
func Spill(tool string, content string) string {
	if content == "" {
		return ""
	}
	dir := gektor.Sub("output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	name := fmt.Sprintf("%s-%d.log", tool, time.Now().UnixNano())
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return ""
	}
	return p
}
