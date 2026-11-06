package automation

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Artifact identifies immutable bytes owned by one run, never a workspace path.
type Artifact struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Created   time.Time `json:"created"`
}

// ArtifactAccess is scoped to the executing run and step by the service.
// Nodes cannot select another run or obtain a store filesystem path.
type ArtifactAccess interface {
	Put(context.Context, string, io.Reader) (Artifact, error)
	Read(context.Context, string) (Artifact, []byte, error)
}

type artifactKey struct{}

func WithArtifacts(ctx context.Context, access ArtifactAccess) context.Context {
	return context.WithValue(ctx, artifactKey{}, access)
}

func Artifacts(ctx context.Context) (ArtifactAccess, error) {
	a, ok := ctx.Value(artifactKey{}).(ArtifactAccess)
	if !ok {
		return nil, fmt.Errorf("artifacts require an active workflow run")
	}
	return a, nil
}
