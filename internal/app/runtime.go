package app

import (
	"github.com/keshon/tars/internal/llm"
)

// Runtime owns the accepted services. Its owner calls Close; frontends
// borrow Services.
//
// It deliberately has no reconfigure method. Settings used to be applied in
// place against a live session, which needed a candidate/reuse/persist dance
// here and three parallel implementations in the TUI; all of it went away
// when Apply became a restart, and putting the machinery back would be
// rebuilding a path nothing walks.
type Runtime struct {
	Services *Services
	closed   bool
}

func NewRuntime(services *Services) *Runtime {
	if services == nil {
		services = &Services{}
	}
	return &Runtime{Services: services}
}

func closeClient(client llm.Client) {
	if c, ok := client.(interface{ Close() }); ok {
		c.Close()
	}
}

func (r *Runtime) Close() {
	if r.closed {
		return
	}
	r.closed = true
	closeClient(r.Services.Client)
}