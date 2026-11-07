package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type attachmentKey struct{}

// WithAttachments exposes only images already accepted into this conversation.
// Use the complete archive so IDs survive compaction and resume.
func WithAttachments(ctx context.Context, histories ...[]Message) context.Context {
	paths := []string{}
	seen := map[string]bool{}
	for _, history := range histories {
		for _, m := range history {
			for _, path := range m.Images {
				if !seen[path] {
					paths = append(paths, path)
					seen[path] = true
				}
			}
		}
	}
	return context.WithValue(ctx, attachmentKey{}, paths)
}

type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func Attachments(ctx context.Context) []Attachment {
	paths, _ := ctx.Value(attachmentKey{}).([]string)
	out := []Attachment{}
	for i, path := range paths {
		out = append(out, Attachment{fmt.Sprintf("attachment:%d", i+1), filepath.Base(path)})
	}
	return out
}

func IsAttachment(name string) bool {
	if !strings.HasPrefix(name, "attachment:") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "attachment:"))
	return err == nil && n > 0 && name == fmt.Sprintf("attachment:%d", n)
}

func ResolveAttachment(ctx context.Context, id string) (string, error) {
	paths, _ := ctx.Value(attachmentKey{}).([]string)
	n, err := strconv.Atoi(strings.TrimPrefix(id, "attachment:"))
	if err != nil || !IsAttachment(id) || n < 1 || n > len(paths) || id != fmt.Sprintf("attachment:%d", n) {
		return "", fmt.Errorf("unknown image attachment %q; list image_attachments in this session", id)
	}
	return paths[n-1], nil
}

// OpenImage reads an exact session grant or delegates to the caller's rooted
// workspace reader. It grants no access to arbitrary external paths.
func OpenImage(ctx context.Context, name string, openWorkspace func(string) (*os.File, error)) (*os.File, error) {
	if !IsAttachment(name) {
		return openWorkspace(name)
	}
	path, err := ResolveAttachment(ctx, name)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
