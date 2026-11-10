package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui/components/table"
)

// A queue window belongs to one session. Its rows keep their identity even
// after consumption; those rows remain readable until the window closes.
type inputQueue struct {
	session        *harness.Session
	nextRunVersion uint64
	view           *table.Model
	stateFile      string
	entries        map[string]queueEntry
	order          []string
}

// Missing rows remain inspectable, but disappearance alone does not prove receipt.
type queueEntry struct {
	agent.QueuedInput
	missing bool
}

func (e queueEntry) readOnly() bool { return e.Received || e.missing }

type inputQueueReturn struct{ queue *inputQueue }
type inputQueueEdit struct {
	session   *harness.Session
	stateFile string
	dialog    *infoDialog
}

func (m *model) queueSummary() string {
	var labels []string
	var latest llm.Message
	if m.sess != nil {
		msg, count, received := m.sess.InputPreview()
		if count > received {
			labels = append(labels, fmt.Sprintf("Queued %d", count-received))
		}
		if received > 0 {
			labels = append(labels, fmt.Sprintf("Received %d", received))
		}
		latest = msg
	}
	if m.hasQueued() {
		// The fallback holds a follow-up for the next run (for example a command).
		labels = append(labels, "Next run 1")
		latest = llm.Message{Content: m.queued, Images: m.queuedImages}
	}
	if len(labels) == 0 {
		return ""
	}
	preview := strings.Join(strings.Fields(cleanText(latest.Content)), " ")
	if preview == "" && len(latest.Images) > 0 {
		preview = fmt.Sprintf("%d images", len(latest.Images))
	}
	return strings.Join(labels, " · ") + " · Latest: " + preview
}

func (m *model) openInputQueue() {
	if edit := m.suspendedInputEdit; edit != nil {
		m.suspendedInputEdit = nil
		if edit.session == m.sess && edit.stateFile == m.stateFile {
			m.openDialog("Edit queued message", nil)
			m.dialog = edit.dialog
			m.resizeFormEditor()
			m.fitBottom()
			return
		}
	}
	q := &inputQueue{session: m.sess, stateFile: m.stateFile, nextRunVersion: m.queuedVersion, view: m.newTable(), entries: map[string]queueEntry{}}
	m.showInputQueue(q)
}
func (m *model) showInputQueue(q *inputQueue) {
	m.openDialog("Message queue", nil)
	d := &tableDialog{model: q.view, list: true, emptyMessage: "No messages waiting", columns: []table.Column{{Label: "Message"}}}
	d.refresh = func() {
		active := map[string]queueEntry{}
		ids := []string{}
		if m.sess == q.session && m.stateFile == q.stateFile && q.session != nil {
			for _, input := range q.session.PendingInputs() {
				id := strconv.FormatUint(input.ID, 10)
				active[id] = queueEntry{QueuedInput: input}
				ids = append(ids, id)
			}
		}
		if m.sess == q.session && m.stateFile == q.stateFile && m.queuedVersion == q.nextRunVersion && m.hasQueued() {
			active["next"] = queueEntry{QueuedInput: agent.QueuedInput{Message: llm.Message{Content: m.queued, Images: append([]string(nil), m.queuedImages...)}}}
		}
		for _, id := range q.order {
			input := q.entries[id]
			input.missing = true
			q.entries[id] = input
		}
		// Snapshot order follows send order, never map iteration order.
		if _, ok := active["next"]; ok {
			ids = append(ids, "next")
		}
		for _, id := range ids {
			if _, exists := q.entries[id]; !exists {
				q.order = append(q.order, id)
			}
			q.entries[id] = active[id]
		}
		queued, received, missing := 0, 0, 0
		rows := []table.Row{}
		for n, id := range q.order {
			input := q.entries[id]
			status := "Queued"
			if id == "next" {
				status = "Next run"
			}
			if input.Received {
				status = "Received by agent · read only"
				received++
			} else if input.missing {
				status = "No longer queued · read only"
				missing++
			} else {
				queued++
			}
			if len(input.Message.Images) > 0 {
				status += fmt.Sprintf(" · %d image", len(input.Message.Images))
				if len(input.Message.Images) > 1 {
					status += "s"
				}
			}
			text := cleanText(input.Message.Content)
			if strings.TrimSpace(text) == "" {
				text = "Image message"
			}
			rows = append(rows, table.Row{ID: id, Cells: []string{fmt.Sprintf("%d  %s", n+1, text)}, Detail: status})
		}
		d.description = fmt.Sprintf("%d queued · oldest first", queued)
		if queued == 0 && received+missing > 0 {
			d.description = "No messages waiting. These rows are read only."
		}
		if received > 0 && queued > 0 {
			d.description += fmt.Sprintf(" · %d received", received)
		}
		q.view.SetRows(rows)
	}
	d.actions = func() []table.Action {
		input, ok := q.entries[q.view.Selected()]
		return []table.Action{{ID: "details", Label: "Read", Key: "enter", Enabled: ok}, {ID: "edit", Label: "Edit", Key: "ctrl+e", Enabled: ok && !input.readOnly()}, {ID: "remove", Label: "Remove", Key: "delete", Enabled: ok && !input.readOnly(), Destructive: true}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
	}
	d.activate = func(action, id string) tea.Cmd {
		input, ok := q.entries[id]
		if !ok {
			return nil
		}
		switch action {
		case "details":
			lines := []string{input.Message.Content}
			if len(input.Message.Images) > 0 {
				lines = append(lines, "", "Images")
				for _, image := range input.Message.Images {
					lines = append(lines, filepath.Base(image))
				}
			}
			m.openDialog("Queued message", lines)
			if input.Received {
				m.dialog.title = "Received message"
			} else if input.missing {
				m.dialog.title = "Message no longer queued"
			}
			m.returnToInputQueue(q)
		case "edit":
			if input.readOnly() {
				return nil
			}
			fields := []formField{{Key: "message", Label: "Message", Value: input.Message.Content, Multiline: true}}
			if len(input.Message.Images) > 0 {
				names := make([]string, len(input.Message.Images))
				for n, image := range input.Message.Images {
					names[n] = filepath.Base(image)
				}
				fields = append(fields, formField{Key: "images", Label: "Images", Value: strings.Join(names, "\n"), Multiline: true, ReadOnly: true})
			}
			m.openForm("Edit queued message", "Save keeps its place and images.", "Save", fields, func(v map[string]string) error {
				text := v["message"]
				return m.changeQueuedInput(q, id, &text)
			})
			m.returnToInputQueue(q)
			editor := m.dialog
			editor.onSuspend = func() {
				editor.form.Sync()
				m.suspendedInputEdit = &inputQueueEdit{session: q.session, stateFile: m.stateFile, dialog: editor}
			}
		case "remove":
			if err := m.changeQueuedInput(q, id, nil); err != nil {
				m.openDialog("Cannot remove message", []string{err.Error()})
				m.returnToInputQueue(q)
				return nil
			}
			delete(q.entries, id)
			for n, key := range q.order {
				if key == id {
					q.order = append(q.order[:n], q.order[n+1:]...)
					break
				}
			}
			d.refresh()
			m.fitBottom()
		}
		return nil
	}
	m.dialog.table = d
	d.refresh()
	m.fitBottom()
}

func (m *model) changeQueuedInput(q *inputQueue, id string, value *string) error {
	if m.sess != q.session || m.stateFile != q.stateFile {
		return fmt.Errorf("this session has changed; copy your edit before closing")
	}
	if id == "next" {
		if m.queuedVersion != q.nextRunVersion || !m.hasQueued() {
			return agent.ErrInputMissing
		}
		if value == nil {
			m.queued = ""
			m.queuedImages = nil
			m.queuedVersion++
		} else {
			if strings.TrimSpace(*value) == "" && len(m.queuedImages) == 0 {
				return fmt.Errorf("message is empty")
			}
			m.queued = *value
		}
		return nil
	}
	inputID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || q.session == nil {
		return agent.ErrInputMissing
	}
	if value == nil {
		return q.session.RemoveInput(inputID)
	}
	if err := q.session.EditInput(inputID, *value); err != nil {
		return fmt.Errorf("%w. Your edit is still here; copy it before closing", err)
	}
	return nil
}
func (m *model) returnToInputQueue(q *inputQueue) {
	m.dialog.onClose = func() { m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return inputQueueReturn{q} }) }
}
