package workflow

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

// Model owns one workflow screen and its background operations.
// All mutable screen state is confined to the Bubble Tea update loop.
type Model struct {
	permissionDialog, runsDialog, proposalDialog *screen.DialogState
	permissionRequests                           map[*screen.DialogState]chan permission.Effect
	proposalBefore                               string
	proposalDraft                                workflow.Definition

	form pendingForm

	dialogs                  *screen.Controls
	surface                  screen.Surface
	config                   Config
	ctx                      context.Context
	shutdown                 context.CancelFunc
	commands                 []tea.Cmd
	thumb                    scrollDrag
	active, busy, inspect    bool
	overview                 bool
	rawDetails               bool
	inspectorTab             int
	portFocus                string
	portOutput, portSelected bool
	positionsLocked          bool
	inspectorOpen, panning   bool
	connectFrom              string
	connectPort              string
	editor                   *Editor
	graph                    graph.Model
	registry                 *workflow.Registry
	notice                   string
	noticeLevel              pane.NoticeLevel
	offset                   int
	dragging                 bool
	last                     graph.Point
	cancel                   context.CancelFunc
	events                   chan tea.Msg
	run                      *workflow.Run
	runID                    string
	schedules                []store.Schedule
	scheduleStatus           []service.ScheduleInspection
	workerLeaseHeld          bool
}

// setNotice replaces both text and severity; a later update must not inherit
// the previous failure's presentation.
func (m *Model) setNotice(level pane.NoticeLevel, text string) {
	m.notice, m.noticeLevel = text, level
}
