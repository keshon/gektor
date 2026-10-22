package tui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tui/components/composer"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

// followUp sends a non-empty input line as a resumed turn;
// the saved transcript carries the conversation, so every follow-up is
// a continuation, not a fresh run. A /command runs locally instead.
func (m *model) followUp() (tea.Model, tea.Cmd) {
	original := m.input.Value()
	text := strings.TrimSpace(original)
	if strings.HasPrefix(text, "/") {
		m.input.SetValue("")
		m.fitInput()
		m.fitBottom()
		return m.command(text)
	}
	if text == "" {
		return m, nil
	}
	m.input.Push(text)
	// Resolve explicit file references before starting a turn.
	text, images, err := splitAttachments(m.ws, text)
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return m, nil
	}
	// No session yet (opened without a task): the first line starts
	// a fresh run rather than resuming nothing. The echo lives in
	// startFresh (after its transcript reset) so every fresh-task
	// path — first line, /new — renders the opening task exactly once.
	m.input.SetValue("")
	m.fitInput()
	m.fitBottom()
	if m.sess == nil {
		if err := m.startFreshPrompt(text, strings.TrimSpace(original), images); err != nil {
			m.input.SetValue(original)
			m.fitInput()
			m.fitBottom()
			m.appendBlock(errorBlock("cannot start: " + err.Error()))
		}
		return m, nil
	}
	history, err := session.ReadHistory(m.stateFile)
	if err != nil {
		m.input.SetValue(original)
		m.fitInput()
		m.fitBottom()
		m.appendBlock(errorBlock("cannot resume: " + err.Error()))
		return m, nil
	}
	if err := m.saveMode(); err != nil {
		m.input.SetValue(original)
		m.fitInput()
		m.fitBottom()
		m.appendBlock(errorBlock("cannot save mode: " + err.Error()))
		return m, nil
	}
	m.appendBlock(userBlock(strings.TrimSpace(original) + imageSuffix(images)))
	// Capture the session: startRun runs on a goroutine, and the Update
	// loop can replace m.sess before this body is entered.
	sess := m.sess
	m.startRun(func(runCtx context.Context) (string, error) {
		return sess.Resume(runCtx, history, text, images...)
	})
	return m, nil
}

const referenceStart = session.ReferenceStart
const referenceEnd = session.ReferenceEnd

const maxReferenceBytes = 32 * 1024
const maxTurnReferenceBytes = 64 * 1024

// References retain their names in the prompt, followed by bounded file snapshots.
// Image references use the existing vision attachment path.
func splitAttachments(ws *workspace.Workspace, text string) (string, []string, error) {
	if ws == nil || !strings.Contains(text, "@") {
		return text, nil, nil
	}
	var images, files []string
	var imageSpans []tokenSpan
	seen := map[string]bool{}
	total := 0
	for _, span := range tokenSpans(text) {
		tok := text[span.start:span.end]
		raw := tok
		quoted := strings.HasPrefix(tok, "@\"") && strings.HasSuffix(tok, "\"") && len(tok) > 3
		if quoted {
			raw = "@" + tok[2:len(tok)-1]
		}
		if !strings.HasPrefix(raw, "@") || len(raw) == 1 {
			continue
		}
		full, err := referencePath(ws, raw[1:])
		if err != nil {
			if os.IsNotExist(err) && !quoted && !strings.ContainsAny(raw[1:], "/\\.") {
				continue
			}
			// Preserve legacy literal mentions of missing unquoted images.
			if os.IsNotExist(err) && !quoted && llm.IsImagePath(raw[1:]) {
				continue
			}
			return "", nil, fmt.Errorf("reference %s: %w", tok, err)
		}
		info, err := ws.Stat(full)
		if err != nil {
			return "", nil, err
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("%s is not a regular file", tok)
		}
		if llm.IsImagePath(full) {
			imageSpans = append(imageSpans, span)
			if !seen[full] {
				source, err := ws.Open(full)
				if err != nil {
					return "", nil, err
				}
				snapshot, err := llm.SnapshotImageFile(source, full, gektor.Sub("images"))
				source.Close()
				if err != nil {
					return "", nil, err
				}
				images = append(images, snapshot)
				seen[full] = true
			}
			continue
		}
		if seen[full] {
			continue
		}
		f, err := ws.Open(full)
		if err != nil {
			return "", nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxReferenceBytes+1))
		f.Close()
		if err != nil {
			return "", nil, err
		}
		if len(data) > maxReferenceBytes {
			return "", nil, fmt.Errorf("%s exceeds the 32 KiB reference limit; reference a smaller file", tok)
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return "", nil, fmt.Errorf("%s is binary or not UTF-8 text", tok)
		}
		total += len(data)
		if total > maxTurnReferenceBytes {
			return "", nil, fmt.Errorf("file references exceed 64 KiB for this message")
		}
		// Keep the validated reference spelling. Resolving junctions or
		// symlinks can give full a different root than ws.Root().
		label := filepath.ToSlash(filepath.Clean(raw[1:]))
		files = append(files, "Workspace file "+strconv.Quote(label)+" (attached content):\n"+string(data))
		seen[full] = true
	}
	clean := text
	if len(images) > 0 {
		var b strings.Builder
		pos := 0
		for _, span := range imageSpans {
			b.WriteString(text[pos:span.start])
			pos = span.end
		}
		b.WriteString(text[pos:])
		clean = strings.TrimSpace(b.String())
	}
	if len(files) > 0 {
		clean += referenceStart + strings.Join(files, "\n\n") + referenceEnd
	}
	return clean, images, nil
}

// Keep snapshots in model history without dumping their contents into the UI.
func displayInputContent(text string) string { return session.DisplayInput(text) }

// References and built-in tools use the same rooted filesystem contract.
func referencePath(ws *workspace.Workspace, path string) (string, error) {
	full, err := ws.Resolve(path)
	if err != nil {
		return "", err
	}
	if _, err := ws.Stat(full); err != nil {
		return "", err
	}
	return full, nil
}

// tokenSpans splits on whitespace but keeps @"..." quoted spans whole:
// screenshot filenames love spaces.
type tokenSpan struct{ start, end int }

func tokenSpans(text string) []tokenSpan {
	var spans []tokenSpan
	start, quoted := -1, false
	for i, r := range text {
		if (r == ' ' || r == '\t' || r == '\n') && !quoted {
			if start >= 0 {
				spans = append(spans, tokenSpan{start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		if r == '"' && (quoted || (i > start && text[i-1] == '@')) {
			quoted = !quoted
		}
	}
	if start >= 0 {
		spans = append(spans, tokenSpan{start, len(text)})
	}
	return spans
}

// imageSuffix marks attached pictures on the echoed user block, so the
// transcript shows what the model saw alongside the text.
func imageSuffix(images []string) string {
	if len(images) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range images {
		b.WriteString("\n[image: " + filepath.Base(p) + "]")
	}
	return b.String()
}

// startRun resets run counters and executes run in the background,
// delivering completion as doneMsg like the first run. Each run gets a
// fresh context: an interrupted run's cancelled context must never leak
// into the next turn.
func (m *model) startRun(run func(ctx context.Context) (string, error)) {
	m.cancelEvaluation()
	m.cancel()
	// A run takes over: an open dialog — or sessions screen — would
	// shadow a transcript that is suddenly live again.
	if m.dialog != nil {
		m.closeDialog()
	}
	if m.sessions != nil {
		m.closeSessions()
	}
	// Retry resumes the latest durable checkpoint so completed tool work
	// is never replayed from the original turn closure.
	// runErr clears: a new turn has no result yet, and /retry gates on it.
	// Live state clears too: a new turn starts with no partial text.
	m.alwaysMu.Lock()
	m.always = map[[2]string]bool{}
	m.alwaysMu.Unlock()
	m.retryRun = func() {
		if m.sess == nil || m.stateFile == "" {
			m.startRun(run)
			return
		}
		history, err := session.ReadHistory(m.stateFile)
		if err != nil {
			m.appendBlock(errorBlock("cannot retry saved task: " + err.Error()))
			return
		}
		sess := m.sess
		m.startRun(func(ctx context.Context) (string, error) { return sess.Resume(ctx, history, "") })
	}
	m.runErr = nil
	m.resetLive()
	m.firstToken = time.Time{}
	m.toolsUsed = 0
	m.filesTouched = map[string]bool{}
	// Unit-built models never set base; fall back instead of panicking
	// on a nil parent context.
	base := m.base
	if base == nil {
		base = context.Background()
	}
	runCtx, cancel := context.WithCancel(base)
	m.cancel = cancel
	m.state = stRunning
	m.spinnerFrame = 0
	// No session-list refresh here. Nothing about the session store changes
	// when a turn starts, and the previous turn's doneMsg already
	// rescanned it — two scans per turn, each re-decoding the state.json
	// the agent rewrites on every step, which is a permanent cache miss.
	// Mutating paths (rename, delete, open) and doneMsg still refresh, and
	// opening the Sessions popup rescans regardless.
	m.input.Focus()
	m.fitInput()
	m.fitBottom()
	m.started = time.Now()
	m.steps = 0
	m.progress = llm.Progress{Phase: "waiting"}
	m.usage.thisRun = false
	m.header.focused, m.header.menu = false, ""
	go func() {
		answer, err := run(runCtx)
		m.send(doneMsg{answer: answer, err: err})
	}()
}

// startInitial begins the argv task: it renders as the opening user
// block, then runs like any fresh turn. Without it the opening task
// is invisible — only follow-ups echo.
func (m *model) startInitial(task string, images []string) error {
	sess, err := m.newSession(task, m.stateFile, images)
	if err != nil {
		return err
	}
	m.sess = sess
	m.sessionTitle = workspace.TitleLine(task)
	if err := m.saveMode(); err != nil {
		return err
	}
	m.appendBlock(userBlock(strings.TrimSpace(task) + imageSuffix(images)))
	m.startRun(func(runCtx context.Context) (string, error) {
		return sess.Run(runCtx)
	})
	return nil
}

// newTask resets to the empty state: the session, transcript, and
// last-turn state go, the next submitted line starts a fresh run
// through the sess==nil path in followUp. Bare /new.
func (m *model) newTask() {
	m.seedHistory = nil
	if m.stateFile == "" && m.sess == nil {
		return
	}
	m.saveDraft()
	m.cancel()
	m.sess, m.stateFile, m.transcript.blocks = nil, "", nil
	m.sessionTitle = ""
	m.resetRunFacts()
	m.state = stDone
	m.restoreDraft()
	m.appendBlock(markerBlock("New task · type a task to begin"))
	m.transcript.Follow = true
	m.transcript.Viewport.GotoBottom()
}

// compactNow reduces the model context on demand while preserving the
// full archive and transcript. Resume loads the saved context projection.
func (m *model) compactNow() (tea.Model, tea.Cmd) {
	if m.state != stDone {
		m.appendBlock(markerBlock("Stop the current turn before compacting"))
		return m, nil
	}
	if m.stateFile == "" {
		m.appendBlock(errorBlock("no active session to compact"))
		return m, nil
	}
	before, after, history, err := agent.CompactFile(m.stateFile, agent.DefaultCompactKeepSteps)
	if err != nil {
		m.appendBlock(errorBlock("cannot compact: " + err.Error()))
		return m, nil
	}
	if after >= before {
		m.appendBlock(markerBlock(fmt.Sprintf("nothing to compact: %d messages", before)))
		m.transcript.Follow = true
		m.transcript.Viewport.GotoBottom()
		return m, nil
	}
	mb := markerBlock(fmt.Sprintf("- compacted: %d → %d messages -", before, after))
	// Marker last, not first: the viewport lands at the bottom showing
	// the kept tail, which reads identical to before — the marker is
	// the only visible signal that anything happened.
	m.transcript.blocks = append(m.transcript.blocks, mb)
	if len(history) > 2 && history[2].Harness == "compact" {
		m.transcript.blocks = append(m.transcript.blocks, contextBlock(history[2]))
	}
	m.restoreContext(history)
	m.refreshContent()
	// Show the compaction marker even when the reader had scrolled up.
	m.transcript.Follow = true
	m.transcript.Viewport.GotoBottom()
	return m, nil
}

// taskDirectory decides where an Agent task's files live. A task keeps the
// directory it is resuming, so reopening a session does not relocate it.
// Task state belongs to the user, not the workspace: the same directory
// has to be found whichever directory GEKTOR was pointed at.
func (m *model) taskDirectory(taskID string) string {
	if m.application != nil {
		if m.stateFile != "" && m.sess == nil {
			return filepath.Dir(m.stateFile)
		}
		return filepath.Join(m.sessionRoot(), taskID)
	}
	return filepath.Join(tasksRoot(), taskID)
}

func (m *model) startFreshPrompt(task, display string, images []string) error {
	sum := sha1.Sum([]byte(task + time.Now().String()))
	taskDir := m.taskDirectory(hex.EncodeToString(sum[:])[:8])
	stateFile := filepath.Join(taskDir, "state.json")
	sess, err := m.newSession(task, stateFile, images)
	if err != nil {
		return err
	}
	if err := writeSavedMode(stateFile, m.plan); err != nil {
		return err
	}
	title := workspace.TitleLine(display)
	if len(m.seedHistory) > 0 && m.sessionTitle != "" {
		title = m.sessionTitle
	}
	if err := workspace.WriteSessionTitle(taskDir, title); err != nil {
		return err
	}
	m.saveDraft()
	if m.stateFile == "" {
		delete(m.drafts, "")
	}
	m.sess, m.stateFile = sess, stateFile
	m.sessionTitle = title
	if len(m.seedHistory) > 0 {
		m.restoreContext(m.seedHistory)
	} else {
		m.clearContext()
	}
	m.input.Restore(composer.Snapshot{History: []string{display}, Index: 1})
	m.queued = ""
	m.transcript.blocks = nil
	m.appendBlock(markerBlock("- new task -"))
	m.appendBlock(userBlock(display + imageSuffix(images)))
	seed := append([]llm.Message(nil), m.seedHistory...)
	m.seedHistory = nil
	if err := m.savePreferences(); err != nil {
		return err
	}
	if len(seed) > 0 {
		m.transcript.blocks = append(renderHistory(seed), m.transcript.blocks...)
		seed = agentSeed(seed)
	}
	m.startRun(func(runCtx context.Context) (string, error) {
		if len(seed) > 0 {
			return sess.Resume(runCtx, seed, task, images...)
		}
		return sess.Run(runCtx)
	})
	return nil
}

func (m *model) pendingInputCount() int {
	if m.sess == nil {
		return 0
	}
	return m.sess.PendingInputCount()
}
