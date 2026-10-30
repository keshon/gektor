package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/tui/components/transcript"
)

func (m *model) Init() tea.Cmd {
	// The terminal owns the native caret's blink and shape.
	return tea.Batch(waitEvents(m), tick(false, m.busy()), m.takeCommands())
}
func waitEvents(m *model) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.events
		if !ok {
			return nil
		}
		if ev.Name == "_done" {
			answer, _ := ev.Fields["answer"].(string)
			err, _ := ev.Fields["error"].(error)
			return doneMsg{answer: answer, err: err}
		}
		return eventMsg(ev)
	}
}

// Frame pacing. Revealing a reply needs frames; a running spinner needs
// a tenth of a second; an idle session needs one tick to keep the clock
// honest. They are separate because the spinner must not spin up when
// the tick drops to a frame.
const (
	spinInterval = 100 * time.Millisecond
	idleTick     = time.Second
)

func tickInterval(revealing, busy bool) time.Duration {
	switch {
	case revealing:
		return revealFrame
	case busy:
		return spinInterval
	default:
		return idleTick
	}
}

func tick(revealing, busy bool) tea.Cmd {
	return tea.Tick(tickInterval(revealing, busy), func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
func (m *model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	defer func() { cmd = tea.Batch(cmd, m.takeCommands()) }()
	defer m.syncSuggestions()
	defer m.syncFocus()
	defer m.queueSettingsDetail()
	switch msg := msg.(type) {
	case tea.FocusMsg:
		// Hosts can restore their own default colors when their window gains
		// focus. Cells remain themed, but terminal padding follows those defaults.
		// The renderer only emits OSC colors when the View's colors change, so
		// reapply them explicitly through its output queue on focus recovery.
		return m, tea.Raw(ansi.SetForegroundColor(themeColor(colorPopupText)) + ansi.SetBackgroundColor(currentPalette().background))
	case tea.BlurMsg:
		return m, nil
	case settingsDetailMsg:
		m.acceptSettingsDetail(msg)
		return m, nil
	case settingsScanMsg:
		m.acceptSettingsScan(msg)
		return m, nil
	case sessionsLoadedMsg:
		m.acceptSessions(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width(msg)
		return m, nil
	case evaluationDoneMsg:
		m.acceptEvaluation(msg)
		return m, nil
	case tickMsg:
		m.refreshEvaluation()
		// The clock runs only while a run is in flight (gates
		// included: a run blocked on input hasn't finished). Once
		// done, elapsed freezes instead of counting idle reading time.
		if m.state != stDone {
			m.elapsed = time.Since(m.started).Round(time.Second)
		}
		if m.busy() {
			// Gated separately from the tick: revealing drops the tick to
			// a frame, and a spinner advancing 60 times a second reads as
			// a glitch rather than activity.
			if m.lastSpin.IsZero() || time.Since(m.lastSpin) >= spinInterval {
				m.spinnerFrame = (m.spinnerFrame + 1) % len(activityFrames)
				m.lastSpin = time.Now()
			}
		} else {
			m.spinnerFrame = 0
		}
		return m, tick(m.revealLive(), m.busy())
	case liveFlushMsg:
		m.liveFlushPending = false
		m.revealLive()
		return m, nil
	case eventMsg:
		if m.state != stStopping || msg.Name == "tool_result" || msg.Name == "tool_output" || msg.Name == "user_input" || msg.Name == "outcome" || msg.Name == "usage" || msg.Name == "finding" {
			m.handleEvent(run.Event(msg))
		}
		return m, waitEvents(m)
	case doneMsg:
		if m.state == stPermission {
			m.popOverlay()
			m.resetNote()
			m.gstage = gsPermit
		}
		m.gate = ""
		m.persistUsage()
		wasInterrupted := m.interrupted
		m.answer = msg.answer
		m.runErr = msg.err
		m.state = stDone
		if err := m.savePreferences(); err != nil {
			m.appendBlock(errorBlock("Cannot save session settings: " + err.Error()))
		}
		m.refreshSessions()
		// Partial live text is not an answer: discard it either way.
		m.resetLive()
		if m.interrupted {
			m.interrupted = false
			m.runErr = nil
			marker := "— interrupted —"
			m.appendBlock(markerBlock(marker))
		} else {
			m.appendBlock(markerBlock("— run finished —"))
			if msg.err != nil {
				// Failures live in the transcript, not just the bottom
				// bar: scrolled-up context keeps the error beside the
				// turn that produced it. /retry re-runs this turn.
				m.appendBlock(errorBlock("failed: " + msg.err.Error() + "\n/retry re-runs this turn"))
			}
		}
		m.alwaysMu.Lock()
		m.always = map[[2]string]bool{}
		m.alwaysMu.Unlock()
		if m.gateDraft != "" {
			m.input.SetValue(m.gateDraft)
			m.gateDraft = ""
		}
		m.fitInput()
		m.fitBottom()
		if m.queued != "" {
			queued := m.queued
			if m.runErr == nil && !wasInterrupted && strings.TrimSpace(m.input.Value()) == "" {
				m.queued = ""
				m.input.SetValue(queued)
				_, cmd := m.followUp()
				return m, tea.Batch(cmd, waitEvents(m))
			}
		}
		return m, waitEvents(m)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	default:
		switch m.activeInputOwner() {
		case focusDialog:
			return m, m.updateFormInput(msg)
		case focusNote:
			var cmd tea.Cmd
			m.note, cmd = m.note.Update(msg)
			return m, cmd
		case focusSearch:
			return m.updateSearchInput(msg)
		case focusInput:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.fitInput()
			m.fitBottom()
			return m, cmd
		}

	}
	return m, nil
}
func (m *model) width(msg tea.WindowSizeMsg) {
	m.termW, m.termH = msg.Width, msg.Height
	if !m.ready {
		m.transcript.Viewport = transcript.NewViewport(msg.Width, msg.Height)
		m.ready = true
	} else {
		m.transcript.Viewport.SetWidth(msg.Width)
	}
	// Cap the answer box like an input field, not a fullscreen editor.
	maxRows := msg.Height / 4
	if maxRows < 2 {
		maxRows = 2
	}
	if maxRows > 6 {
		maxRows = 6
	}
	m.input.MaxHeight = maxRows
	m.fitInput()
	m.fitBottom()
	m.input.SetWidth(max(m.transcript.Viewport.Width()-5, 1))
	m.note.SetWidth(max(m.transcript.Viewport.Width()-10, 1))
	if m.sessions != nil {
		m.sessions.filter.SetWidth(max(msg.Width-14, 1))
	}
	// Height follows the terminal too: without a refit here a resize
	// leaves a stale viewport height, clipping content by the delta.
	m.fitBottom()
	m.resizeFormEditor()
	m.scrollDialog(0)
	// Render (or re-render): blocks appended before the first resize
	// never reached the viewport, and reflow follows later resizes.
	m.refreshContent()
}

// fitInput sizes the answer box to its content: one row for short
// answers, growing to MaxHeight, then scrolling internally.
func (m *model) fitInput() {
	if m.ready {
		// DynamicHeight counts wrapped rows and clamps the scroll offset when
		// the composer grows, keeping earlier lines visible after a newline.
		m.input.SetWidth(max(m.termW-m.promptOffset()-5, 1))
	}
}

// fitBottom resizes the viewport so transcript + status + bottom bar
// always fit the terminal exactly. Called on every state transition
// and every input keystroke, since the answer box grows as you type.
func (m *model) fitBottom() {
	if !m.ready {
		return
	}
	m.input.SetWidth(max(m.termW-m.promptOffset()-5, 1))
	// Done budgets the input box only (the hint moved right into the
	// status row); a run error adds its own line on top.
	lines := 1
	if m.prefillStatus() != "" {
		lines++
	}
	if m.state == stDone && m.runErr != nil {
		lines++
	}
	// The reject stage shows label + input: two bottom lines.
	if m.state == stPermission && m.gstage == gsReject {
		lines = 2
	}
	if m.state == stAsk || m.state == stDone || m.state == stRunning || m.state == stStopping {
		lines += m.input.Height() - 1
	}
	// The fixed rows count the menu, pane header, status and divider,
	// plus the bottom base and breathing blank line above the divider
	// (layout, not content: a content trailing newline breaks scroll
	// math — phantom blank rows mid-scroll).
	// `lines` adds per-state extras (done hint, reject label, input growth).
	// A takeover overlay owns the bottom bar (one slim line), so it
	// budgets nothing extra: the input hides with it.
	if m.queued != "" && m.state != stPermission && m.state != stAsk {
		lines++
	}
	if m.pendingInputCount() > 0 && m.state != stPermission && m.state != stAsk {
		lines++
	}
	if m.dialog != nil {
		lines = 1
	}
	oldWidth := m.transcript.Viewport.Width()
	m.fitColumns()
	m.note.SetWidth(max(m.transcript.Viewport.Width()-10, 1))
	if oldWidth != m.transcript.Viewport.Width() {
		m.refreshContent()
	}
	h := m.termH - 7 - lines
	if m.actionBarVisible() {
		h -= m.footerRows()
	}
	if h < 1 {
		h = 1
	}
	pinned := m.transcript.Follow && m.transcript.Viewport.AtBottom()
	m.transcript.Viewport.SetHeight(h)
	if pinned {
		m.transcript.Viewport.GotoBottom()
	}
	m.gateVP.SetWidth(m.transcript.Viewport.Width())
	m.gateVP.SetHeight(h)
	if m.state == stPermission || m.state == stAsk {
		m.refreshGate()
	}
	if m.sessions != nil {
		width := m.searchGeometry().width - 4
		m.sessions.filter.SetWidth(max(width-lipgloss.Width(m.sessions.filter.Prompt)-1, 1))
		if m.sessions.mode == sessRename {
			m.note.SetWidth(max(width-lipgloss.Width(m.note.Prompt)-1, 1))
		}
	}
}

// scrollViewport forwards a scroll message (mouse or paging key) to
// the transcript and refreshes follow stickiness. Every state routes
// through it, so scroll never depends on which widget is focused.
func (m *model) scrollViewport(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	return m, m.transcript.Update(msg)
}
func (m *model) handleEvent(ev run.Event) {
	m.beginTranscriptUpdate()
	defer m.endTranscriptUpdate()
	wasPrefill := m.prefillStatus() != ""
	defer func() {
		if wasPrefill != (m.prefillStatus() != "") {
			m.fitBottom()
		}
	}()
	switch ev.Name {
	case "progress":
		phase, _ := ev.Fields["phase"].(string)
		number := func(key string) int { n, _ := ev.Fields[key].(float64); return int(n) }
		estimated, _ := ev.Fields["estimated"].(bool)
		m.progress = llm.Progress{Phase: phase, PromptTokens: number("prompt"), CompletionTokens: number("completion"), CachedTokens: number("cached"), ProcessedTokens: number("processed"), Estimated: estimated}
		m.progress.Detail, _ = ev.Fields["detail"].(string)
		if phase == "reasoning" || phase == "generating" || phase == "tool_call" {
			m.usage.liveModel = true
			if m.firstToken.IsZero() {
				m.firstToken = time.Now()
			}
		}
		if m.progress.PromptTokens > 0 {
			m.tokens, m.tokensEst = m.progress.PromptTokens+m.progress.CompletionTokens, estimated
			m.usage.historical = false
			m.usage.source = "Active request"
			m.usage.at = time.Now()
		}
	case "backend":
		if name, _ := ev.Fields["model"].(string); name != "" {
			m.modelName = name
		}
		if limit, _ := ev.Fields["context_limit"].(float64); limit > 0 {
			m.limit = int(limit)
		}
	case "step":
		m.usage.liveModel = true
		m.progress.Phase = "tools"
		m.steps++
		// The enforced step budget rides the event: the meter reads that limit,
		// never a second literal. Absent on old producers — keep ours.
		if ms, ok := ev.Fields["max_steps"].(float64); ok && ms > 0 {
			m.maxSteps = int(ms)
		}
		// The live buffer belongs to this response: clear it first so
		// the authoritative blocks below never duplicate it. Queued text
		// counts as part of it — a step that omits the reply adopts
		// whatever arrived, revealed or not. A step with empty text but
		// a non-empty buffer adopts the buffer (backend streamed text it
		// then omitted); tool steps never adopt, calls are work.
		live := m.live + m.liveQueue
		m.resetLive()
		// Reasoning above its reply: the collapsed
		// think line introduces the answer it produced, causal order.
		// Tool calls render as cards below, whatever prompted them.
		reasoning, _ := ev.Fields["reasoning"].(string)
		text, _ := ev.Fields["text"].(string)
		if text == "" && live != "" {
			if calls, _ := ev.Fields["tool_calls"].([]any); len(calls) == 0 {
				text = live
			}
		}
		for _, b := range assistantBlocks(text, reasoning) {
			m.appendBlock(b)
		}

		if calls, _ := ev.Fields["tool_calls"].([]any); len(calls) > 0 {
			for _, c := range calls {
				call, _ := c.(map[string]any)
				name, _ := call["name"].(string)
				args, _ := call["args"].(string)
				id, _ := call["id"].(string)
				m.toolsUsed++
				m.appendBlock(toolCardBlock(id, name+" "+truncate(args, 120)))
			}
		}
	case "user_input":
		text, _ := ev.Fields["text"].(string)
		var images []string
		imageValues, _ := ev.Fields["images"].([]any)
		for _, raw := range imageValues {
			if path, ok := raw.(string); ok {
				images = append(images, path)
			}
		}
		m.appendBlock(userBlock(displayInputContent(text) + imageSuffix(images)))
		m.fitBottom()
	case "tool_output":
		text, _ := ev.Fields["text"].(string)
		callID, _ := ev.Fields["call_id"].(string)
		for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
			b := &m.transcript.blocks[i]
			if b.role == roleTool && b.open && b.callID == callID {
				b.result += text
				m.refreshContent()
				break
			}
		}
	case "tool_result":
		text, _ := ev.Fields["text"].(string)
		callID, _ := ev.Fields["call_id"].(string)
		var status []bool
		if failed, ok := ev.Fields["failed"].(bool); ok {
			status = []bool{failed}
		}
		if !m.attachResult(callID, text, status...) {
			m.appendBlock(resultBlock(text))
		}
	case "finding":
		rule, _ := ev.Fields["rule"].(string)
		path, _ := ev.Fields["path"].(string)
		line, _ := ev.Fields["line"].(float64)
		summary, _ := ev.Fields["summary"].(string)
		if rule != "" {
			m.appendBlock(findingBlock(rule, path, int(line), summary))
		}
	case "nudge":
		// Harness notices render as dim markers: same text the model
		// saw (provenance mark included), no decisions attached.
		if text, _ := ev.Fields["text"].(string); text != "" {
			m.appendBlock(contextBlock(llm.Message{Role: llm.RoleUser, Content: text}))
		}
	case "local_warning":
		if text, _ := ev.Fields["text"].(string); text != "" {
			m.appendBlock(errorBlock(text))
		}
	case "context":
		text, _ := ev.Fields["text"].(string)
		role, _ := ev.Fields["role"].(string)
		request, _ := ev.Fields["request"].(float64)
		found := false
		for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
			if m.transcript.blocks[i].role == roleMarker && m.transcript.blocks[i].contextText == text {
				at := m.transcript.blocks[i].at
				m.transcript.blocks[i] = contextBlock(llm.Message{Role: llm.Role(role), Content: text, Request: int(request)})
				m.transcript.blocks[i].at = at
				found = true
				break
			}
		}
		if !found {
			m.appendBlock(contextBlock(llm.Message{Role: llm.Role(role), Content: text, Request: int(request)}))
		}
		m.refreshContent()
	case "prompt_budget":
		data, err := json.Marshal(ev.Fields["report"])
		var report llm.ContextReport
		if err == nil && json.Unmarshal(data, &report) == nil {
			m.usage.breakdown = report
		}
	case "usage":
		if p, ok := ev.Fields["prompt"].(float64); ok && int(p) > 0 {
			completion, _ := ev.Fields["completion"].(float64)
			m.tokens = int(p) + int(completion)
			m.tokensEst, _ = ev.Fields["estimated"].(bool)
			m.usage.source = "Last request context"
			if m.tokensEst {
				m.usage.source = "Last request estimate"
			}
			m.usage.at = time.Now()
			m.usage.thisRun = true
			m.usage.historical = false
			m.usage.liveModel = true
			m.usage.last = usageSnapshot{Tokens: m.tokens, Estimated: m.tokensEst, At: m.usage.at, Model: m.modelName}
		}
	case "_permission_preview":
		m.gatePreview, _ = ev.Fields["text"].(string)
	case "awaiting_input":
		m.closeHeader()
		kind, _ := ev.Fields["kind"].(string)
		prompt, _ := ev.Fields["prompt"].(string)
		m.gate = prompt
		m.gateVP = viewport.New(viewport.WithWidth(max(m.termW, 1)), viewport.WithHeight(max(m.transcript.Viewport.Height(), 1)))
		if m.sessions != nil {
			m.closeSessions()
		}
		if m.dialog != nil {
			m.closeDialog()
		}
		// The prompt lives in the transcript, not the bottom bar: the
		// bar has a fixed 1-line budget (2 with the answer box), so the
		// viewport math below always fits the terminal. A long question
		// stays readable by scrolling instead of pushing layout around.
		if prompt != "" {
			m.appendBlock(gateBlock(prompt))
		}
		if kind == string(agent.SuspendPermission) {
			m.state = stPermission
			m.gstage = gsPermit
			m.gateTool, _ = ev.Fields["tool"].(string)
			m.gateResource, _ = ev.Fields["resource"].(string)
			m.note.Blur()
			m.note.SetValue("")
			m.pushOverlay()
			m.openGatePopup()
		} else {
			m.state = stAsk
			m.gatePreview = ""
			m.gateDraft = m.input.Value()
			m.input.SetValue("")
			m.input.Focus()
			m.openGatePopup()
		}
		m.fitInput()
		m.fitBottom()
	case "input_answered":
		m.state = stRunning
		m.gatePreview = ""
		m.gate = ""
		m.input.SetValue(m.gateDraft)
		m.gateDraft = ""
		m.input.Focus()
		m.fitInput()
		m.fitBottom()
	case "result":
		if report, ok := ev.Fields["report"].(string); ok && report != "" {
			m.appendBlock(answerBlock(report))
		}
	case "outcome":
		m.filesTouched = map[string]bool{}
		outcome, _ := ev.Fields["outcome"].(string)
		var files []string
		if values, ok := ev.Fields["files"].([]any); ok {
			for _, value := range values {
				if path, ok := value.(string); ok {
					files = append(files, path)
					m.filesTouched[path] = true
				}
			}
		}
		checks, _ := ev.Fields["verification"].(string)
		if checks == "" {
			checks = "no current command verdict"
		}
		resume, _ := ev.Fields["resume"].(string)
		fileList := strings.Join(files, ", ")
		if fileList == "" {
			fileList = "none observed"
		}
		text := fmt.Sprintf("outcome: %s\nfiles: %s\nchecks: %s", outcome, fileList, checks)
		if resume != "" {
			text += "\nresume: gektor -tui -resume " + resume
		}
		m.appendBlock(block{role: roleReceipt, text: text, at: time.Now()})
	case "reasoning":
		m.usage.liveModel = true
		m.progress.Phase = "reasoning"
		// Streamed deliberation. Queued separately from the answer so the
		// THINK block fills while the model is still thinking, and is still
		// on screen when RESPONSE starts.
		if text, _ := ev.Fields["text"].(string); text != "" {
			m.liveThinkQueue += text
			if m.firstToken.IsZero() {
				m.firstToken = time.Now()
			}
			m.scheduleLiveFlush()
		}
	case "delta":
		m.usage.liveModel = true
		m.progress.Phase = "generating"
		// Coalesce only until the next frame; never simulate character typing.
		if text, _ := ev.Fields["text"].(string); text != "" && !m.liveCut {
			if m.firstToken.IsZero() {
				m.firstToken = time.Now()
			}
			m.liveQueue += text
			m.liveRunes += utf8.RuneCountInString(text)
			if m.liveRunes > liveMaxRunes {
				// Past the cap the buffer stops growing, so show the mark
				// now rather than leaving the reader with text that
				// silently stopped moving.
				m.liveCut = true
				m.live += m.liveQueue
				m.liveQueue = ""
				if r := []rune(m.live); len(r) > liveMaxRunes {
					m.live = string(r[:liveMaxRunes]) + "\n…(live truncated)"
				}
				m.refreshContent()
			}
			m.scheduleLiveFlush()
		}
	}
}
