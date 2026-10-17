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

	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/tui/components/composer"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/snapshot"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/workspace"
)

// followUp sends a non-empty input line as a resumed turn;
// the saved transcript carries the conversation, so every follow-up is
// a continuation, not a fresh run. A /command runs locally instead.
func (m *model) followUp() (tea.Model, tea.Cmd) {
	if m.chat != nil {
		return m.submitConversation(m.input.Value())
	}
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
	m.startRun(func(runCtx context.Context) (string, error) {
		return m.sess.Resume(runCtx, history, text, images...)
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
		info, err := os.Stat(full)
		if err != nil {
			return "", nil, err
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("%s is not a regular file", tok)
		}
		if llm.IsImagePath(full) {
			imageSpans = append(imageSpans, span)
			if !seen[full] {
				images = append(images, full)
				seen[full] = true
			}
			continue
		}
		if seen[full] {
			continue
		}
		f, err := os.Open(full)
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
		rel, _ := filepath.Rel(ws.Root(), full)
		files = append(files, "Workspace file "+strconv.Quote(filepath.ToSlash(rel))+" (attached content):\n"+string(data))
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

// Resolve symlinks as well as textual paths before exposing file contents.
func referencePath(ws *workspace.Workspace, path string) (string, error) {
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", fmt.Errorf("use a workspace-relative path")
	}
	full, err := ws.Resolve(path)
	if err != nil {
		return "", err
	}
	full, err = filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(ws.Root())
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes the workspace")
	}
	return full, nil
}

// splitTokens splits on whitespace but keeps @"..." quoted spans
// whole: screenshot filenames love spaces.
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

func splitTokens(text string) []string {
	var tokens []string
	for _, span := range tokenSpans(text) {
		tokens = append(tokens, text[span.start:span.end])
	}
	return tokens
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
	m.cancel()
	// A run takes over: an open dialog — or sessions screen — would
	// shadow a transcript that is suddenly live again.
	if m.dialog != nil {
		m.closeDialog()
	}
	if m.sessions != nil {
		m.closeSessions()
	}
	// The turn closure is kept for /retry: re-invoking it re-attempts
	// the exact turn (follow-ups reuse their loaded history, so a
	// retry restarts from last known-good, not partial failure).
	// runErr clears: a new turn has no result yet, and /retry gates on it.
	// Live state clears too: a new turn starts with no partial text.
	m.alwaysMu.Lock()
	m.always = map[[2]string]bool{}
	m.alwaysMu.Unlock()
	m.retryRun = func() { m.startRun(run) }
	m.runErr = nil
	m.live, m.liveQueue, m.liveCut, m.liveRunes = "", "", false, 0
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
	m.ctx = runCtx
	m.state = stRunning
	m.spinnerFrame = 0
	m.navFocused = false
	m.refreshNavigator()
	m.input.Focus()
	m.fitInput()
	m.fitBottom()
	m.started = time.Now()
	m.steps = 0
	m.usage.thisRun = false
	m.header.focused, m.header.menu = false, ""
	var voice speech.Service
	if m.services != nil {
		voice = m.services.Voice
	}
	voiced := m.chat == nil && m.voiceEnabled && voice != nil
	go func() {
		answer, err := run(runCtx)
		if voiced && err == nil && answer != "" && runCtx.Err() == nil {
			audioCtx, cancelAudio := context.WithCancel(runCtx)
			defer m.registerSpeech(cancelAudio)()
			out := speech.NewOutput(audioCtx, voice, func(phase string) { m.emitVoice(runCtx, phase) })
			if e := out.Delta(answer); e != nil {
				out.Cancel()
			}
			if e := out.Finish(); e != nil && runCtx.Err() == nil && audioCtx.Err() == nil {
				m.emitVoice(runCtx, "Speech failed; text remains available")
			}
		}
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

// newChat resets to the empty-chat state: the session, transcript,
// and last-turn state go, the next submitted line starts a fresh run
// through the sess==nil path in followUp. Bare /new.
func (m *model) newChat() {
	m.seedHistory = nil
	if m.chat != nil {
		m.saveDraft()
		m.cancel()
		m.chat.current = nil
		m.stateFile = ""
		m.sessionTitle = ""
		m.transcript.blocks = nil
		m.resetRunFacts()
		m.state = stDone
		m.navFocused = false
		m.restoreDraft()
		m.appendBlock(markerBlock("New conversation · Chat · no tools"))
		m.showGreeting()
		return
	}
	if m.stateFile == "" && m.sess == nil {
		return
	}
	m.saveDraft()
	m.cancel()
	m.sess, m.stateFile, m.transcript.blocks = nil, "", nil
	m.sessionTitle = ""
	m.mission = false
	m.resetRunFacts()
	m.state, m.navFocused = stDone, false
	m.restoreDraft()
	m.appendBlock(markerBlock("New chat · type a task to begin"))
	m.transcript.Follow = true
	m.transcript.Viewport.GotoBottom()
}

// compactNow mechanically compacts the active session's saved history
// in place (/compact): the same compactHistory the loop uses, on
// demand instead of at the usage threshold. The transcript rebuilds
// from the compacted snapshot with a stats marker, so display and
// context agree on what was dropped. Follow-ups continue on the
// compacted history through the normal Resume path.
func (m *model) compactNow() (tea.Model, tea.Cmd) {
	if m.state != stDone {
		m.appendBlock(markerBlock("Stop the current turn before compacting"))
		return m, nil
	}
	if m.chat != nil {
		if m.chat.current == nil || m.stateFile == "" {
			m.appendBlock(errorBlock("no active session to compact"))
			return m, nil
		}
		before, after, err := m.chat.current.CompactAndSave(m.stateFile, agent.DefaultCompactKeepSteps)
		if err != nil {
			m.appendBlock(errorBlock("cannot compact: " + err.Error()))
			return m, nil
		}
		if after == before {
			m.appendBlock(markerBlock(fmt.Sprintf("nothing to compact: %d messages", before)))
			return m, nil
		}
		m.transcript.blocks = renderHistory(m.chat.current.History())
		m.appendBlock(markerBlock(fmt.Sprintf("— compacted: %d → %d messages; original archive backed up —", before, after)))
		m.restoreContext(m.chat.current.History())
		m.refreshContent()
		m.refreshNavigator()
		m.transcript.Follow = true
		m.transcript.Viewport.GotoBottom()
		return m, nil
	}
	if m.stateFile == "" {
		m.appendBlock(errorBlock("no active session to compact"))
		return m, nil
	}
	before, after, err := agent.CompactFile(m.stateFile, agent.DefaultCompactKeepSteps)
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
	history, err := session.ReadHistory(m.stateFile)
	if err != nil {
		m.appendBlock(errorBlock("cannot reload compacted history: " + err.Error()))
		return m, nil
	}
	if m.sess == nil {
		m.seedHistory = append([]llm.Message(nil), history...)
	}
	mb := markerBlock(fmt.Sprintf("- compacted: %d → %d messages -", before, after))
	// Marker last, not first: the viewport lands at the bottom showing
	// the kept tail, which reads identical to before — the marker is
	// the only visible signal that anything happened.
	m.transcript.blocks = append(renderHistory(history), mb)
	m.restoreContext(history)
	m.refreshContent()
	// The point of manual compaction is seeing the shrunken transcript:
	// re-arm follow and jump to bottom even if the reader scrolled up,
	// or the marker lands below the fold and nothing visibly changed.
	m.transcript.Follow = true
	m.transcript.Viewport.GotoBottom()
	return m, nil
}

// startFresh begins a new task in a fresh transcript and state dir,
// mirroring how the CLI namespaces one task per .tars/tasks/<id>.
func (m *model) startFresh(task string, images []string) error {
	return m.startFreshPrompt(task, task, images)
}

// holdsChatArchive reports whether dir is a Chat conversation's own
// directory. Such a directory belongs to the conversation; an Agent task
// that adopts it will overwrite the transcript's identity rather than
// sit beside it.
func holdsChatArchive(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "conversation.json"))
	return err == nil
}

// taskDirectory decides where an Agent task's files live. A task keeps
// the directory it is resuming, but never a Chat conversation's own
// directory: the two keep different state files in the same place, so the
// task's save rewrote the conversation's settings.json and the session
// list stopped offering the transcript at all.
func (m *model) taskDirectory(taskID string) string {
	taskDir := workspace.TaskDir(taskID)
	if m.ws != nil {
		taskDir = filepath.Join(m.ws.Root(), taskDir)
	}
	if m.application != nil {
		taskDir = filepath.Join(m.sessionRoot(), taskID)
		if m.stateFile != "" && m.sess == nil {
			taskDir = filepath.Dir(m.stateFile)
		}
		if holdsChatArchive(taskDir) {
			taskDir = filepath.Join(m.sessionRoot(), taskID)
		}
	}
	return taskDir
}

func (m *model) startFreshPrompt(task, display string, images []string) error {
	if m.chat != nil {
		return m.startConversation(task)
	}
	sum := sha1.Sum([]byte(task + time.Now().String()))
	taskDir := m.taskDirectory(hex.EncodeToString(sum[:])[:8])
	stateFile := filepath.Join(taskDir, "state.json")
	sess, err := m.newSession(task, stateFile, images)
	if err != nil {
		return err
	}
	if err := writeChatMode(stateFile, m.plan); err != nil {
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
	m.clearContext()
	m.input.Restore(composer.Snapshot{History: []string{display}, Index: 1})
	m.queued = ""
	m.transcript.blocks = nil
	m.appendBlock(markerBlock("- new task -"))
	m.appendBlock(userBlock(display + imageSuffix(images)))
	if m.ws != nil {
		if snap := snapshot.Track(m.ws.Root(), filepath.Join(taskDir, "snapshots")); snap.Path == "" {
			m.appendBlock(markerBlock("Pre-run checkpoint unavailable; this run cannot be undone with -revert."))
		}
	}
	seed := append([]llm.Message(nil), m.seedHistory...)
	m.seedHistory = nil
	if err := m.saveSessionSettings(); err != nil {
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
