//go:build !windows

package agent

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

func consoleInput(context.Context) ([]tea.ProgramOption, func(), error) {
	return nil, func() {}, nil
}
