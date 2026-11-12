package agent

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
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
	dialog    *screen.DialogState
}

func (m *Model) queueSummary() string {
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

func (m *Model) openInputQueue() {
	if edit := m.suspendedInputEdit; edit != nil {
		m.suspendedInputEdit = nil
		if edit.session == m.sess && edit.stateFile == m.stateFile {
			m.chrome.Resume(edit.dialog)
			m.fitBottom()
			return
		}
		delete(m.formEdits, edit.dialog)
		delete(m.returnDialogs, edit.dialog)
	}
	q := &inputQueue{session: m.sess, stateFile: m.stateFile, nextRunVersion: m.queuedVersion, view: m.chrome.NewTable(), entries: map[string]queueEntry{}}
	m.showInputQueue(q)
}
func (m *Model) showInputQueue(q *inputQueue) {
	m.chrome.Open("Message queue", nil)
	d := &screen.TableState{
		Model:        q.view,
		List:         true,
		EmptyMessage: "No messages waiting",
		Columns:      []table.Column{{Label: "Message"}},
	}

	if m.queueDialogs == nil {
		m.queueDialogs = make(map[*screen.DialogState]*inputQueue)
	}
	m.chrome.Dialog.Table = d
	m.queueDialogs[m.chrome.Dialog] = q
	m.refreshInputQueue(q)
	m.fitBottom()
}

func (m *Model) changeQueuedInput(q *inputQueue, id string, value *string) error {
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
func (m *Model) returnToInputQueue(q *inputQueue) {
	m.rememberReturn(dialogReturn{queue: q})
}

type editQueuedMessage struct {
	id string
	q  *inputQueue
}

func (edit editQueuedMessage) apply(m *Model, v map[string]string) error {
	text := v["message"]
	return m.changeQueuedInput(edit.q, edit.id, &text)
}

func (m *Model) refreshInputQueue(q *inputQueue) {
	d := m.chrome.Dialog.Table

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
	d.Description = fmt.Sprintf("%d queued · oldest first", queued)
	if queued == 0 && received+missing > 0 {
		d.Description = "No messages waiting. These rows are read only."
	}
	if received > 0 && queued > 0 {
		d.Description += fmt.Sprintf(" · %d received", received)
	}
	m.chrome.RefreshTable(rows)

	d.Actions = m.inputQueueActions(q)
}

func (m *Model) inputQueueActions(q *inputQueue) []table.Action {
	input, ok := q.entries[q.view.Selected()]
	return []table.Action{
		{ID: "details", Label: "Read", Key: "enter", Enabled: ok},
		{ID: "edit", Label: "Edit", Key: "ctrl+e", Enabled: ok && !input.readOnly()},
		{ID: "remove", Label: "Remove", Key: "delete", Enabled: ok && !input.readOnly(), Destructive: true},
		{ID: "close", Label: "Close", Key: "esc", Enabled: true},
	}
}

func (m *Model) inputQueueAction(q *inputQueue, action, id string) tea.Cmd {
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
		m.chrome.Open("Queued message", lines)
		if input.Received {
			m.chrome.Dialog.Title = "Received message"
		} else if input.missing {
			m.chrome.Dialog.Title = "Message no longer queued"
		}
		m.returnToInputQueue(q)
	case "edit":
		if input.readOnly() {
			return nil
		}
		fields := []form.Field{{Key: "message", Label: "Message", Value: input.Message.Content, Multiline: true}}
		if len(input.Message.Images) > 0 {
			names := make([]string, len(input.Message.Images))
			for n, image := range input.Message.Images {
				names[n] = filepath.Base(image)
			}
			fields = append(fields, form.Field{Key: "images", Label: "Images", Value: strings.Join(names, "\n"), Multiline: true, ReadOnly: true})
		}
		m.openForm(screen.Form{
			Title:      "Edit queued message",
			Hint:       "Save keeps its place and images.",
			ApplyLabel: "Save",
			Fields:     fields,
		}, editQueuedMessage{
			id: id,
			q:  q,
		})
		m.returnToInputQueue(q)

	case "remove":
		if err := m.changeQueuedInput(q, id, nil); err != nil {
			m.chrome.Open("Cannot remove message", []string{err.Error()})
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
		m.refreshInputQueue(q)
		m.fitBottom()
	}
	return nil
}
