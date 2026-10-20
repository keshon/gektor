package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tui/components/gate"
	"github.com/keshon/gektor/internal/workspace"
)

// gateStage is the permission overlay stage: permit, always-confirm,
// or reject-with-note. Ask gates keep
// their own path and never touch stages.
type gateStage int

const (
	// gsPermit answers inline: allow once, escalate to always-confirm,
	// or drop to reject-with-note.
	gsPermit gateStage = iota
	// gsAlways confirms the run-local always scope before answering.
	gsAlways
	// gsReject takes a redirect note; empty means plain deny.
	gsReject
)

// newNote builds the reject-note input: single line, capped, no
// history. Esc is handled by the stager (back), never the widget.
func newNote() textinput.Model {
	ti := textinput.New()
	ti.SetStyles(textInputStyles(ti.Styles()))
	ti.SetVirtualCursor(false)
	style := ti.Styles()
	style.Cursor.Shape = tea.CursorUnderline
	ti.SetStyles(style)
	ti.Prompt = "note> "
	ti.CharLimit = 240
	return ti
}

// gateKey handles keys while a permission gate is open, one stage at
// a time. It answers through resolveGate only: every exit path tears
// the overlay down, so a answered gate can never strand a stage.
func (m *model) gateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	defer m.fitBottom()
	if msg.String() == "ctrl+q" {
		// Quit works from every gate stage: letters are answers here.
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	switch m.gstage {
	case gsAlways:
		switch msg.String() {
		case "enter":
			m.resolveGate("a")
			return m, nil
		case "esc":
			m.gstage = gsPermit
			return m, nil
		}
		return m, nil
	case gsReject:
		switch msg.String() {
		case "enter":
			note := strings.TrimSpace(m.note.Value())
			ans := "n"
			if note != "" {
				ans = "n: " + note
				rb := userBlock("reject: " + note)
				m.appendBlock(rb)
			}
			m.resolveGate(ans)
			return m, nil
		case "esc":
			m.note.Blur()
			m.note.SetValue("")
			m.gstage = gsPermit
			return m, nil
		case "ctrl+c":
			m.quit = true
			m.cancel()
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.note, cmd = m.note.Update(msg)
		return m, cmd
	default: // gsPermit
		// The popup owns up/down; Enter confirms the highlighted
		// option. Letter shortcuts still answer directly.
		if m.gatePopup.Keypress(msg) {
			return m, nil
		}
		switch msg.String() {
		case "enter":
			if m.gatePopup.Pending() {
				m.confirmGateOption()
				return m, nil
			}
			return m, nil
		case "y", "Y":
			m.resolveGate("y")
		case "a", "A":
			m.gstage = gsAlways
		case "n", "N":
			m.gstage = gsReject
			m.note.SetValue("")
			return m, m.note.Focus()
		case "esc":
			// Esc denies, as before: a gate left open would hang the
			// run, and there is no "later" to defer to.
			m.resolveGate("n")
		case "ctrl+c":
			m.quit = true
			m.cancel()
			return m, tea.Quit
		}
		return m, nil
	}
}

// confirmGateOption answers with the highlighted popup option.
func (m *model) confirmGateOption() {
	if !m.gatePopup.Pending() {
		return
	}
	opt := m.gatePopup.Current()
	switch opt.Value {
	case "y":
		m.resolveGate("y")
	case "a":
		m.gstage = gsAlways
	case "n":
		m.gstage = gsReject
		m.note.SetValue("")
		_ = m.note.Focus()
	default:
		m.resolveGate(opt.Value)
	}
}

// resolveGate answers the pending gate and tears the overlay down:
// stage reset, note blurred, focus restored, run resumed.
func (m *model) resolveGate(answer string) {
	_ = m.hub.Respond(answer)
	m.gstage = gsPermit
	m.note.Blur()
	m.gatePopup.Close()
	m.popOverlay()
	m.state = stRunning
	m.fitBottom()
	m.refreshContent()
	m.input.Focus()
}

// answerAsk sends an stAsk reply and resumes. One place so the Enter
// path and the plan Y/N shortcuts cannot diverge.
func (m *model) answerAsk(answer string) {
	m.input.Push(strings.TrimSpace(answer))
	_ = m.hub.Respond(answer)
	m.gatePopup.Close()
	m.state = stRunning
}

// openGatePopup builds the bottom popup for the pending gate. The
// transcript stays mounted behind it: history remains readable while
// the question is open.
func (m *model) openGatePopup() {
	switch m.state {
	case stPermission:
		m.gatePopup.Open("Permission required", []gate.Option{
			{Value: "y", Label: "Allow once", Hint: "Y"},
			{Value: "a", Label: "Allow this run", Hint: "A"},
			{Value: "n", Label: "Deny with note", Hint: "N"},
		})
	case stAsk:
		// A free-text question has no fixed answers: the popup shows
		// the question and the composer takes the reply.
		m.gatePopup.Open("Answer required", nil)
	}
}

// gateHint is the single footer row naming the popup keys.
func (m *model) gateHint() string {
	switch m.state {
	case stPermission:
		switch m.gstage {
		case gsAlways:
			return "Enter confirm  Esc back"
		case gsReject:
			return "Enter deny with note  Esc back"
		default:
			return "↑↓ select  Y/A/N answer  Enter confirm  Esc deny"
		}
	case stAsk:
		return "Type the answer and press Enter  Esc stop"
	default:
		return ""
	}
}

// gateView draws the gate popup over the bottom of the transcript; the
// composer and its caret stay put.
//
// The popup is sized to leave a band of transcript showing. A gate that
// filled the pane would answer "what do I do" by hiding "what happened",
// and the prompt being asked about lives in that transcript.
func (m *model) gateView(body string) string {
	if !m.gatePopup.Pending() {
		return body
	}
	width := min(m.transcript.Viewport.Width(), 78)
	lines := strings.Split(body, "\n")
	height := max(len(lines)-gateTranscriptRows, 6)
	question := gate.Wrap(m.gate, max(width-4, 1))
	rows := m.gatePopup.View(width, height, question, m.gateHint(),
		m.gstage == gsReject, popupSurface())
	if len(rows) == 0 {
		return body
	}
	if len(lines) < len(rows) {
		return body
	}
	return strings.Join(append(lines[:len(lines)-len(rows)], rows...), "\n")
}

// gateTranscriptRows is how much conversation stays visible above a gate.
const gateTranscriptRows = 4

// gateBar renders the bottom bar for the permission overlay by stage.
// The reject stage costs two lines (label + input); fitBottom budgets
// it, the transcript keeps scrolling above.
func (m *model) gateBar() string {
	switch m.gstage {
	case gsAlways:
		return m.styles.gate.Render(fmt.Sprintf(
			"Enter confirm  Esc back  allow %s on %q for this run",
			m.gateTool, truncate(m.gateResource, 80)))
	case gsReject:
		return m.styles.dim.Render("reject with note (empty = plain deny)  [Esc] back") +
			"\n" + m.note.View()
	default:
		return m.styles.gate.Render("Y allow once  N deny with note  A allow this run  Esc deny")
	}
}

func (m *model) contextGate(hub *run.GateHub, ws *workspace.Workspace) func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
	return func(runCtx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		key := [2]string{tool, resource}
		m.alwaysMu.Lock()
		allowed := m.always[key]
		m.alwaysMu.Unlock()
		if allowed {
			return permission.Allow, nil
		}
		prompt := fmt.Sprintf("[permission] %s on %q", tool, resource)
		if preview := tools.PreviewArgs(ws, tool, args); preview != "" {
			prompt += "\n" + preview
		}
		rep, err := hub.Suspender()(runCtx, agent.SuspendRequest{
			Kind:     agent.SuspendPermission,
			Tool:     tool,
			Resource: resource,
			Prompt:   prompt,
		})
		if err != nil {
			return permission.Deny, err
		}
		eff, derr := permission.Decide(rep.Answer)
		if derr != nil {
			return permission.Deny, derr
		}
		if isAlwaysAnswer(rep.Answer) {
			m.alwaysMu.Lock()
			m.always[key] = true
			m.alwaysMu.Unlock()
		}
		return eff, nil
	}
}

// isAlwaysAnswer reports whether the reply asks for run-long allow.
// Mirrors permission.Decide's allow list; kept beside the memory that
// depends on it.
func isAlwaysAnswer(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "a", "always":
		return true
	}
	return false
}
func (m *model) refreshGate() {
	title := "Permission required"
	switch {
	case m.state == stAsk:
		title = "Answer required"
	}
	rows := []string{m.styles.gate.Render(title), ""}
	for _, line := range strings.Split(m.gate, "\n") {
		rows = append(rows, reflow(line, max(m.gateVP.Width()-1, 1))...)
	}
	m.gateVP.SetContent(strings.Join(rows, "\n"))
}
