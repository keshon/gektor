package tui

import "github.com/keshon/tars/internal/app"

// Command launches supply their runtime. Run creates an owned fallback before
// validation; unit-built models create one lazily.
func (m *model) runtime() *app.Runtime {
	if m.services == nil {
		m.services = app.NewRuntime(m.audio())
	}
	return m.services
}
