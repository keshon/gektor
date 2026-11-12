package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

func TestStepAppendsTranscript(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "step",
		Fields: map[string]any{"text": "hello", "step": 0},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 1 || mm.transcript.blocks[0].text != "hello" || mm.transcript.blocks[0].role != roleAnswer {
		t.Fatalf("blocks = %+v", mm.transcript.blocks)
	}
	if mm.steps != 1 {
		t.Fatalf("steps = %d", mm.steps)
	}
}

func TestDoneQuits(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(doneMsg{answer: "done"})
	mm := updated.(*Model)
	if mm.state != stDone || mm.answer != "done" {
		t.Fatalf("state = %v answer = %q", mm.state, mm.answer)
	}
	// Empty follow-up quits; the program no longer exits on done.
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	if mm.quit {
		t.Fatal("empty enter in done state must do nothing")
	}
}

func TestFollowUpResumes(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	sess, err := harness.New(harness.Config{
		Task: "task",
		Env: roles.Env{
			Client: stubDone{},
			WS:     ws,
			Procs:  toolbackground.New(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.sess = sess
	doneCh := make(chan doneMsg, 4)
	m.send = func(msg tea.Msg) {
		if d, ok := msg.(doneMsg); ok {
			doneCh <- d
		}
	}
	stateFile := filepath.Join(t.TempDir(), "state.json")
	seedHistory(t, stateFile)
	m.stateFile = stateFile

	updated, _ := m.testUpdate(doneMsg{answer: "done"})
	mm := updated.(*Model)
	mm.input.SetValue("and then?")
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	if mm.state != stRunning {
		t.Fatalf("state = %v, want running", mm.state)
	}
	echoed := false
	for _, b := range mm.transcript.blocks {
		if strings.Contains(b.text, "and then?") {
			echoed = true
			if b.role != roleUser {
				t.Fatalf("echo must be a user block: %+v", b)
			}
		}
	}
	if !echoed {
		t.Fatalf("follow-up not echoed: %v", mm.transcript.blocks)
	}
	select {
	case d := <-doneCh:
		if d.answer != "done" {
			t.Fatalf("answer = %q", d.answer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("resumed run never completed")
	}
}

func TestSeededContinuationKeepsHistoricalHeaderUntilResponse(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m.application = &settings.Application{Temporary: true}
	m.stateFile = filepath.Join(dir, "state.json")
	m.seedHistory = []llm.Message{{Role: llm.RoleSystem, Content: "old system"}, {Role: llm.RoleUser, Content: "previous task"}, {Role: llm.RoleAssistant, Content: "previous answer"}}
	data, err := json.Marshal(usageSnapshot{Tokens: 1000, Model: "/old/model.gguf"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	m.modelName = "/new/model.gguf"
	m.newSession = func(task, stateFile string, _ []string) (*harness.Session, error) {
		return harness.New(harness.Config{Task: task, StateFile: stateFile, Env: roles.Env{Client: stubDone{}, WS: ws}})
	}
	done := make(chan tea.Msg, 1)
	m.send = func(msg tea.Msg) { done <- msg }
	if err := m.startFreshPrompt("continue", "continue", nil); err != nil {
		t.Fatal(err)
	}
	if m.tokens <= 0 || !m.usage.historical || m.usage.liveModel || m.displayedModel() != "/old/model.gguf" {
		t.Fatalf("sending cleared historical values: tokens=%d usage=%+v model=%q", m.tokens, m.usage, m.displayedModel())
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("continuation did not finish")
	}
}

func TestMeter_UsageAndUnknown(t *testing.T) {
	m := sizeModel(t, testModel())
	m.limit = 131072
	if got := m.meter(); got != "ctx 0/131.1k (0%)" {
		t.Fatalf("no measurement yet: %q", got)
	}
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "usage",
		Fields: map[string]any{"prompt": 12400},
	}))
	mm := updated.(*Model)
	if got := mm.meter(); got != "ctx 12.4k/131.1k (9%)" {
		t.Fatalf("meter = %q", got)
	}
}

func TestMeter_Estimated(t *testing.T) {
	m := sizeModel(t, testModel())
	m.limit = 131072
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "usage",
		Fields: map[string]any{"prompt": 12400, "estimated": true},
	}))
	mm := updated.(*Model)
	if got := mm.meter(); got != "~ctx 12.4k/131.1k (9%)" {
		t.Fatalf("estimated meter = %q", got)
	}
}

func TestKTokens(t *testing.T) {
	if got := kTokens(999); got != "999" {
		t.Fatalf("got %q", got)
	}
	if got := kTokens(12400); got != "12.4k" {
		t.Fatalf("got %q", got)
	}
}

// A fresh follow-up with @path reaches the session constructor with
// the text cleaned, and the echo marks the attachment.
func TestFollowUp_AttachesImages(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root(), "shot.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	var gotTask string
	var gotImages []string
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		gotTask, gotImages = task, images
		return harness.New(harness.Config{
			Task: task,
			Env: roles.Env{
				Client: stubDone{},
				WS:     ws,
				Procs:  toolbackground.New(),
			},
		})
	}
	var sent int32
	m.send = func(tea.Msg) { atomic.AddInt32(&sent, 1) }
	m.state = stDone
	m.input.SetValue("what is this @shot.png")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if gotTask != "what is this" || len(gotImages) != 1 {
		t.Fatalf("task=%q images=%v", gotTask, gotImages)
	}
	found := false
	for _, b := range mm.transcript.blocks {
		if strings.Contains(b.text, "[image: shot.png]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("echo missing image marker: %+v", mm.transcript.blocks)
	}
}

func TestResultBlockDiffColors(t *testing.T) {
	out := renderBlock(resultBlock("-old\n+new\n@@ h @@\nctx"), defaultStyles(), tagged(), 80)
	for _, want := range []string{"-old", "+new", "@@ h @@", "ctx"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestViewportSizedWithoutTTY(t *testing.T) {
	m := sizeModel(t, testModel())
	if !m.ready || m.transcript.Viewport.Width() != 78 {
		t.Fatalf("viewport not sized: %+v", m.transcript.Viewport)
	}
}

func TestFollowUnfollowsOnScroll(t *testing.T) {
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock(strings.Repeat("line ", 20) + string(rune('a'+i%26))))
	}
	if !m.transcript.Follow {
		t.Fatal("follow expected while at bottom")
	}
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
	mm := updated.(*Model)
	if mm.transcript.Follow {
		t.Fatal("pgup must unfollow")
	}
	if !strings.Contains(ansi.Strip(mm.testView().Content), "CTRL+END Latest") {
		t.Fatal("unfollowed state must show jump hint")
	}
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	mm = updated.(*Model)
	if !mm.transcript.Follow {
		t.Fatal("end must re-arm follow")
	}
}

func TestDialogQDoesNotClose(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.testControls().Open("help", helpText())
	m.syncControls()
	updated, _ := m.testUpdate(keyRunes('q'))
	if mm := updated.(*Model); mm.chrome.Dialog == nil {
		t.Fatal("q must not close the dialog")
	}
}

// Activity animates in the header badge while a run is live and stops the
// moment it is not. It used to be asserted through a spinner() helper and a
// headerLine() title renderer that nothing calls any more.
func TestActivityBadgeAnimatesOnlyWhileRunning(t *testing.T) {
	m := sizeModel(t, testModel())
	activity := func() string {
		for _, b := range m.headerBadges() {
			if b.ID == "activity" {
				return b.Label
			}
		}
		return ""
	}
	m.state = stRunning
	first := activity()
	if !strings.Contains(first, string(activityFrames[0])) {
		t.Fatalf("no activity frame in badge: %q", first)
	}
	m.testUpdate(tickMsg(time.Now()))
	if activity() == first {
		t.Fatal("activity frame did not advance on tick")
	}
	for _, state := range []runState{stAsk, stPermission, stDone} {
		m.state = state
		label := activity()
		for _, frame := range activityFrames {
			if strings.Contains(label, string(frame)) {
				t.Fatalf("state %v animates: %q", state, label)
			}
		}
	}
	m.state = stRunning
	width := lipgloss.Width(m.identityLine())
	m.state = stDone
	if lipgloss.Width(m.identityLine()) != width {
		t.Fatal("activity moved the header layout")
	}
}

func TestEmptyStart_SubmitBegins(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		return harness.New(harness.Config{
			Task: task,
			Env: roles.Env{
				Client: stubDone{},
				WS:     ws,
				Procs:  toolbackground.New(),
			},
		})
	}
	sent := make(chan doneMsg, 4)
	m.send = func(msg tea.Msg) {
		if d, ok := msg.(doneMsg); ok {
			sent <- d
		}
	}
	// No session, no state: first line starts fresh, like /new.
	m.state = stDone
	m.input.SetValue("hello?")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.state != stRunning {
		t.Fatalf("state = %v, want running", mm.state)
	}
	if mm.sess == nil || mm.stateFile == "" {
		t.Fatal("fresh session and state file expected")
	}
	// The typed task echoes as the opening user block, after the
	// new-task marker (startFresh resets the transcript first).
	if len(mm.transcript.blocks) != 2 || mm.transcript.blocks[1].role != roleUser {
		t.Fatalf("opening task must echo as user block: %+v", mm.transcript.blocks)
	}
	if mm.transcript.blocks[1].text != "hello?" {
		t.Fatalf("echo text = %q, want verbatim task", mm.transcript.blocks[1].text)
	}
	select {
	case d := <-sent:
		if d.answer != "done" {
			t.Fatalf("answer = %q", d.answer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fresh run never completed")
	}
}

func TestInitialTaskEchoedAsUser(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		return harness.New(harness.Config{
			Task: task,
			Env: roles.Env{
				Client: stubDone{},
				WS:     ws,
				Procs:  toolbackground.New(),
			},
		})
	}
	m.send = func(tea.Msg) {}
	if err := m.startInitial("  hi  ", nil); err != nil {
		t.Fatal(err)
	}
	if m.state != stRunning {
		t.Fatalf("state = %v, want running", m.state)
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].role != roleUser {
		t.Fatalf("opening task must echo as user block: %+v", m.transcript.blocks)
	}
	if m.transcript.blocks[0].text != "hi" {
		t.Fatalf("echo must trim the task: %q", m.transcript.blocks[0].text)
	}
}

func TestLongStepFlowsUncut(t *testing.T) {
	// P10-2 fixture (TUI-2/TUI-10): a long verification-list reply
	// plus multibyte reasoning must survive the event funnel intact.
	text := strings.Repeat("verify this. ", 150)
	reason := strings.Repeat("deliberation — ", 100)
	var buf bytes.Buffer
	em := events.New(&buf)
	harness.EmitStep(em, "run", 1, llm.Message{Content: text, Reasoning: reason}, false, 0)
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("not json: %v", err)
	}
	fields := map[string]any{}
	for k, v := range rec {
		if k != "seq" && k != "event" {
			fields[k] = v
		}
	}
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "step", Fields: fields}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 2 {
		t.Fatalf("blocks = %d", len(mm.transcript.blocks))
	}
	if mm.transcript.blocks[0].role != roleThink || mm.transcript.blocks[0].text != reason {
		t.Fatal("reasoning cut or misroled by the funnel")
	}
	if mm.transcript.blocks[1].role != roleAnswer || mm.transcript.blocks[1].text != text {
		t.Fatal("answer cut or misroled by the funnel")
	}
}

func TestStatusPostsToHistory(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.limit = 131072
	m.tokens = 12400
	m.input.SetValue("/status")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.chrome.Dialog != nil {
		t.Fatal("status must post to history, not a modal")
	}
	view := mm.transcript.Viewport.View()
	for _, want := range []string{"backend", "workspace", "context", "ctx 12.4k/131.1k (9%)", "permissions"} {
		if !strings.Contains(view, want) {
			t.Fatalf("status missing %q", want)
		}
	}
}

func TestKvRowsAlignsValues(t *testing.T) {
	rows := kvRows([]string{"a: 1", "longer: 2", "no-colon"})
	if rows[2] != "no-colon" {
		t.Fatalf("colonless row must pass through: %q", rows[2])
	}
	i1, i2 := strings.Index(rows[0], "1"), strings.Index(rows[1], "2")
	if i1 != i2 {
		t.Fatalf("values misaligned: %q vs %q", rows[0], rows[1])
	}
}

func TestStatusLinesPlainLanguage(t *testing.T) {
	m := sizeModel(t, testModel())
	m.backendKind, m.modelName = "llama", "local"
	out := strings.Join(m.statusLines(), "\n")
	for _, banned := range []string{"staged", "always-memory", "pairs", "budget default chars"} {
		if strings.Contains(out, banned) {
			t.Fatalf("status keeps harness jargon %q:\n%s", banned, out)
		}
	}
	for _, want := range []string{"thinking", "Ctrl+G", "permissions", "Y allow once", "session approvals", "none"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q:\n%s", want, out)
		}
	}
}

func TestDialogFitsViewport(t *testing.T) {
	m := sizeModel(t, testModel())
	m.testControls().Open("status", m.statusLines())
	m.syncControls()
	m.transcript.Viewport.SetWidth(26)
	m.transcript.Viewport.SetHeight(8)
	out := m.testControls().DialogView()
	lines := strings.Split(out, "\n")
	if len(lines) > 8 {
		t.Fatalf("%d dialog lines past height 8", len(lines))
	}
	for _, ln := range lines {
		if n := lipgloss.Width(ln); n > 26 {
			t.Fatalf("dialog line %d cols past 26: %q", n, ln)
		}
	}
}

func TestStartRunClosesDialog(t *testing.T) {
	m := sizeModel(t, testModel())
	m.send = func(tea.Msg) {}
	m.testControls().Open("help", helpText())
	m.syncControls()
	if len(m.overlays) != 1 {
		t.Fatal("setup: dialog must push")
	}
	m.startRun(func(ctx context.Context) (string, error) { return "done", nil })
	m.syncControls()
	if m.chrome.Dialog != nil || len(m.overlays) != 0 {
		t.Fatal("run start must close the dialog")
	}
}

func TestBottomZoneHeights(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	updated, _ := m.testUpdate(tea.WindowSizeMsg{Width: 80, Height: 24})
	if mm := updated.(*Model); mm.transcript.Viewport.Height() != 15 {
		t.Fatalf("done viewport height = %d, want 15", mm.transcript.Viewport.Height())
	}
	mm := updated.(*Model)
	mm.state = stRunning
	updated, _ = mm.testUpdate(tea.WindowSizeMsg{Width: 80, Height: 24})
	if mm := updated.(*Model); mm.transcript.Viewport.Height() != 16 {
		t.Fatalf("running viewport height = %d, want 16", mm.transcript.Viewport.Height())
	}
	// Composing keeps the same budget: the hint stays put, so the
	// layout never shifts while typing (user call: always visible).
	mm.input.SetValue("typing")
	mm.state = stDone
	updated, _ = mm.testUpdate(tea.WindowSizeMsg{Width: 80, Height: 24})
	if mm = updated.(*Model); mm.transcript.Viewport.Height() != 15 {
		t.Fatalf("composing viewport height = %d, want 15", mm.transcript.Viewport.Height())
	}
	// The frame uses every row, including the footer at the bottom edge.
	if n := len(strings.Split(mm.testView().Content, "\n")); n != 24 {
		t.Fatalf("view is %d lines, want 24", n)
	}
}

// Keys live only in /help now that the status row carries state
// instead of a manual: every essential binding must be listed there.
func TestHintAlwaysShown(t *testing.T) {
	for _, want := range []string{"Ctrl+Q", "Ctrl+G", "Enter", "Alt+Up / Alt+Down", "Esc"} {
		found := false
		for _, s := range helpSections() {
			for _, r := range s.rows {
				if strings.Contains(r[0], want) {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("help teaches no %q", want)
		}
	}
}

func TestHelpSections(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(markerBlock(strings.Join(helpText(), "\n")), st, tagged(), 80)
	for _, want := range []string{"Input", "Navigate", "Run", "Commands"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q", want)
		}
	}
	if strings.Contains(out, "thread") {
		t.Fatalf("thread strip removed everywhere: %q", out)
	}
	// Two columns: every command description starts at the same column.
	// Asserted on helpRows, which is the renderer the help dialog uses --
	// the padding lives there, in keyWidth.
	cmds := map[string]string{
		"/quit": "Quit", "/help": "Open this help", "/new [task]": "Start a fresh task; omit task for an empty session",
		"/status": "Show current run facts", "/retry": "Retry the last failed turn", "/sessions": "Browse saved sessions",
		"/compact": "Shrink this session's history",
	}
	m := sizeModel(t, testModel())
	var rendered []string
	for _, s := range helpSections() {
		if s.title == "Commands" {
			rendered = m.helpRows(s, 80)
		}
	}
	col, found := -1, 0
	for _, ln := range rendered {
		text := plain(ln)
		for k, d := range cmds {
			if !strings.Contains(text, k+" ") {
				continue
			}
			found++
			if i := strings.Index(text, d); col < 0 {
				col = i
			} else if i != col {
				t.Fatalf("command columns drift: %q", ln)
			}
		}
	}
	if found != len(cmds) {
		t.Fatalf("only %d command rows found", found)
	}
}

func TestFailedRunKeepsErrorBlock(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(doneMsg{err: errors.New("boom")})
	mm := updated.(*Model)
	if mm.runErr == nil {
		t.Fatal("runErr must survive a failed run")
	}
	found := false
	for _, b := range mm.transcript.blocks {
		if b.role == roleError && strings.Contains(b.text, "boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("error missing from transcript: %+v", mm.transcript.blocks)
	}
}

func TestRetryRerunsLastTurn(t *testing.T) {
	m := sizeModel(t, testModel())
	m.send = func(tea.Msg) {}
	m.retryRun = func() { m.startRun(func(context.Context) (string, error) { return "recovered", nil }) }
	m.runErr = errors.New("boom")
	m.state = stDone
	m.input.SetValue("/retry")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.state != stRunning {
		t.Fatalf("state = %v, want running", mm.state)
	}
	updated, _ = mm.testUpdate(doneMsg{answer: "recovered"})
	mm = updated.(*Model)
	if mm.runErr != nil || mm.answer != "recovered" {
		t.Fatalf("runErr = %v answer = %q", mm.runErr, mm.answer)
	}
}

func TestRetryRefusesWhenOk(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("/retry")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.state != stDone {
		t.Fatalf("state = %v, want done", mm.state)
	}
	found := false
	for _, b := range mm.transcript.blocks {
		if strings.Contains(b.text, "nothing to retry") {
			found = true
		}
	}
	if !found {
		t.Fatalf("refusal missing: %+v", mm.transcript.blocks)
	}
}

func TestToggleHoldsPosition(t *testing.T) {
	// Growth below the viewport: offset untouched.
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock("filler"))
	}
	m.appendBlock(thinkBlock("reasoning " + strings.Repeat("y", 300)))
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
	mm := updated.(*Model)
	if mm.transcript.Follow {
		t.Fatal("setup: pgup must unfollow")
	}
	y0 := mm.transcript.Viewport.YOffset()
	first := strings.Split(mm.transcript.Viewport.View(), "\n")[0]
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	mm = updated.(*Model)
	if mm.transcript.Viewport.YOffset() != y0 {
		t.Fatalf("growth below moved offset %d -> %d", y0, mm.transcript.Viewport.YOffset())
	}
	if got := strings.Split(mm.transcript.Viewport.View(), "\n")[0]; got != first {
		t.Fatal("growth below changed the first line")
	}
	// Growth above: first visible line stays first.
	m2 := sizeModel(t, testModel())
	m2.appendBlock(thinkBlock("reasoning " + strings.Repeat("z", 300)))
	for i := 0; i < 40; i++ {
		m2.appendBlock(markerBlock("filler"))
	}
	updated, _ = m2.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	mm2 := updated.(*Model)
	updated, _ = mm2.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	mm2 = updated.(*Model)
	if mm2.transcript.Follow {
		t.Fatal("setup: pgup must unfollow")
	}
	before := mm2.transcript.Viewport.View()
	first2 := strings.Split(before, "\n")[0]
	updated, _ = mm2.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	mm2 = updated.(*Model)
	if got := strings.Split(mm2.transcript.Viewport.View(), "\n")[0]; got != first2 {
		t.Fatalf("growth above moved first line:\nwas  %q\nnow  %q", first2, got)
	}
	// Collapse back: byte-identical view returns.
	updated, _ = mm2.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if got := updated.(*Model).transcript.Viewport.View(); got != before {
		t.Fatal("collapse must restore the exact view")
	}
}

// The step budget and time to first token live in the header details,
// which is what the operator reads. Both used to be asserted through
// runFacts, a status-line renderer nothing called any more.
func TestHeaderDetailsShowStepBudget(t *testing.T) {
	m := sizeModel(t, testModel())
	m.steps = 7
	m.maxSteps = 40
	m.chrome.Header.Menu = "run"
	if joined := headerText(m.testControls().HeaderItems()); !strings.Contains(joined, "7/40") {
		t.Fatalf("header details missing step budget: %q", joined)
	}
}

// Time to first token appears once a token has actually arrived, and not
// before. Fixed clock offsets, no tick flakes.
func TestHeaderDetailsShowFirstToken(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.started = time.Now().Add(-100 * time.Second)
	m.chrome.Header.Menu = "run"
	if got := headerText(m.testControls().HeaderItems()); strings.Contains(got, "First token") {
		t.Fatalf("details show a first token before one arrived: %q", got)
	}
	m.firstToken = m.started.Add(7 * time.Second)
	if got := headerText(m.testControls().HeaderItems()); !strings.Contains(got, "00:07") {
		t.Fatalf("details missing first token: %q", got)
	}
}

func TestEnvMaxSteps_DefaultAndOverride(t *testing.T) {
	if got := envMaxSteps(roles.Env{}); got != agent.DefaultMaxSteps {
		t.Fatalf("envMaxSteps(zero) = %d, want default %d", got, agent.DefaultMaxSteps)
	}
	if got := envMaxSteps(roles.Env{MaxSteps: 60}); got != 60 {
		t.Fatalf("envMaxSteps(60) = %d, want 60", got)
	}
}

func TestStepEventUpdatesStepBudget(t *testing.T) {
	m := sizeModel(t, testModel())
	m.maxSteps = 25
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "step",
		Fields: map[string]any{"max_steps": 37, "text": "hi"},
	}))
	if mm := updated.(*Model); mm.maxSteps != 37 {
		t.Fatalf("maxSteps = %d, want 37", mm.maxSteps)
	}
}

func TestStatusShowsModel(t *testing.T) {
	m := testModel()
	m.modelName = "qwen"
	view := strings.Join(m.statusLines(), "\n")
	// Values align in one column, so the label and name no longer sit
	// adjacent — assert the name on the model row, not exact spacing.
	found := false
	for _, ln := range strings.Split(view, "\n") {
		if strings.HasPrefix(ln, "model") && strings.Contains(ln, "qwen") {
			found = true
		}
	}
	if !found {
		t.Fatalf("status missing model: %q", view)
	}
}

func TestFormatElapsed(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{0, "00:00"},
		{40 * time.Second, "00:40"},
		{70 * time.Second, "01:10"},
		{3723 * time.Second, "1:02:03"},
		{-5 * time.Second, "00:00"},
	} {
		if got := formatElapsed(tc.in); got != tc.want {
			t.Fatalf("formatElapsed(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestElapsedRunsInFlightFreezesWhenDone(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.started = time.Now().Add(-90 * time.Second)
	updated, _ := m.testUpdate(tickMsg(time.Now()))
	mm := updated.(*Model)
	if mm.elapsed != 90*time.Second {
		t.Fatalf("running elapsed = %v, want 1m30s", mm.elapsed)
	}
	// Gates are still the run's wall clock.
	mm.state = stAsk
	mm.started = time.Now().Add(-5 * time.Second)
	updated, _ = mm.testUpdate(tickMsg(time.Now()))
	if mm := updated.(*Model); mm.elapsed != 5*time.Second {
		t.Fatalf("ask elapsed = %v, want 5s", mm.elapsed)
	}
	// Done freezes: idle reading time never accrues.
	mm.state = stDone
	updated, _ = mm.testUpdate(tickMsg(time.Now()))
	if mm := updated.(*Model); mm.elapsed != 5*time.Second {
		t.Fatalf("done elapsed moved to %v", mm.elapsed)
	}
}

func TestStartRunResetsClock(t *testing.T) {
	m := sizeModel(t, testModel())
	m.send = func(tea.Msg) {}
	m.started = time.Now().Add(-time.Hour)
	m.steps = 7
	m.tokens = 42
	before := time.Now()
	m.startRun(func(ctx context.Context) (string, error) { return "done", nil })
	m.syncControls()
	if m.started.Before(before) {
		t.Fatal("startRun must reset the clock")
	}
	if m.steps != 0 || m.tokens != 42 || m.state != stRunning {
		t.Fatalf("run not reset: steps=%d tokens=%d state=%v", m.steps, m.tokens, m.state)
	}
	if !m.input.Focused() {
		t.Fatal("run start must focus input for drafting")
	}
}

func TestOutcomeUsesObservedChanges(t *testing.T) {
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "step", Fields: map[string]any{"tool_calls": []any{map[string]any{"id": "read", "name": "read_file", "args": `{"path":"read-only.go"}`}}}})
	m.handleEvent(events.Event{Name: "outcome", Fields: map[string]any{"outcome": "completed", "files": []string{"changed.go"}}})
	if len(m.filesTouched) != 1 || !m.filesTouched["changed.go"] {
		t.Fatalf("counter must use observed effects: %v", m.filesTouched)
	}
}
