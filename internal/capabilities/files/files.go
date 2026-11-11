// Package files implements bounded file operations shared by agent tools and
// workflow nodes. It does not depend on either execution engine.
package files

import (
	"net/http"

	"github.com/keshon/gektor/internal/workspace"
)

const MaxFileBytes int64 = 16 << 20

// Service borrows a workspace for bounded downloads and archive operations.
// Construct it by value with WS; the caller keeps the workspace open during use.
type Service struct {
	WS        *workspace.Workspace
	transport http.RoundTripper // Tests replace the network, not URL validation.
}
